package repository

import (
	"database/sql"
	"forum/internal/models"
	"strings"
	"time"
)

const pageSize = 20

type PostRepository struct{ DB *sql.DB }

type CategoryRepository struct{ DB *sql.DB }

func (r *CategoryRepository) List() ([]models.Category, error) {
	rows, err := r.DB.Query(`SELECT id, name FROM categories ORDER BY name = 'Другое', name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Category
	for rows.Next() {
		var c models.Category
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListWithCounts returns categories with the number of posts in each.
func (r *CategoryRepository) ListWithCounts() ([]models.Category, error) {
	rows, err := r.DB.Query(`
SELECT c.id, c.name, COUNT(pc.post_id)
FROM categories c
LEFT JOIN post_categories pc ON pc.category_id = c.id
	AND pc.post_id IN (SELECT id FROM posts WHERE is_hidden = 0)
GROUP BY c.id ORDER BY c.name = 'Другое', c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Category
	for rows.Next() {
		var c models.Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *PostRepository) Create(userID int, title, content, imagePath string, categoryIDs []int) (int, error) {
	tx, err := r.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO posts (user_id, title, content, image_path) VALUES (?, ?, ?, ?)`, userID, title, content, imagePath)
	if err != nil {
		return 0, err
	}
	id64, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	postID := int(id64)

	for _, cid := range categoryIDs {
		if _, err := tx.Exec(`INSERT INTO post_categories (post_id, category_id) VALUES (?, ?)`, postID, cid); err != nil {
			return 0, err
		}
	}

	return postID, tx.Commit()
}

func (r *PostRepository) DeleteByAuthor(postID, userID int) (bool, error) {
	res, err := r.DB.Exec(`DELETE FROM posts WHERE id = ? AND user_id = ?`, postID, userID)
	if err != nil {
		return false, err
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

func (r *PostRepository) Update(postID, userID int, title, content, imagePath string, categoryIDs []int) (bool, error) {
	tx, err := r.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE posts SET title = ?, content = ?, image_path = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?`,
		title, content, imagePath, postID, userID,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}

	if _, err := tx.Exec(`DELETE FROM post_categories WHERE post_id = ?`, postID); err != nil {
		return false, err
	}
	for _, cid := range categoryIDs {
		if _, err := tx.Exec(`INSERT INTO post_categories (post_id, category_id) VALUES (?, ?)`, postID, cid); err != nil {
			return false, err
		}
	}

	return true, tx.Commit()
}

func (r *PostRepository) GetCategoryIDs(postID int) ([]int, error) {
	rows, err := r.DB.Query(`SELECT category_id FROM post_categories WHERE post_id = ?`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// FeedOptions selects and orders posts for the feed and profile lists.
type FeedOptions struct {
	Filter   string // "", "my_posts" (posts by UserID) or "liked" (liked by UserID)
	UserID   int
	Category string
	Query    string // matched case-insensitively against title and text
	Sort     string // "new" (default), "top" or "discussed"
	ViewerID int    // fills PostSummary.MyReaction; 0 for guests
	// Following limits the feed to authors that ViewerID follows.
	Following bool
	Limit     int
	Offset    int
}

const postColumns = `
SELECT p.id, p.title, p.content, p.user_id, u.username, u.avatar, u.reputation, p.created_at, p.updated_at, p.image_path,
       COALESCE(SUM(CASE WHEN pr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
       COALESCE(SUM(CASE WHEN pr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes,
       (SELECT COUNT(*) FROM comments c WHERE c.post_id = p.id AND c.is_deleted = 0) AS comment_count,
       COALESCE((SELECT mr.value FROM post_reactions mr WHERE mr.post_id = p.id AND mr.user_id = ?), 0) AS my_reaction,
       EXISTS (SELECT 1 FROM bookmarks bm WHERE bm.post_id = p.id AND bm.user_id = ?) AS bookmarked,
       EXISTS (SELECT 1 FROM polls pl WHERE pl.post_id = p.id) AS has_poll,
       p.best_comment_id IS NOT NULL AS solved,
       p.is_hidden
FROM posts p
JOIN users u ON u.id = p.user_id
LEFT JOIN post_reactions pr ON pr.post_id = p.id
`

const postGroupBy = ` GROUP BY p.id `

func scanPost(sc interface{ Scan(...any) error }) (models.PostSummary, error) {
	var p models.PostSummary
	var updatedAt sql.NullTime
	err := sc.Scan(&p.ID, &p.Title, &p.Content, &p.AuthorID, &p.AuthorName, &p.AuthorAvatar, &p.AuthorRep, &p.CreatedAt, &updatedAt,
		&p.ImagePath, &p.Likes, &p.Dislikes, &p.CommentCount, &p.MyReaction, &p.Bookmarked, &p.HasPoll, &p.Solved, &p.IsHidden)
	if updatedAt.Valid {
		p.UpdatedAt = &updatedAt.Time
	}
	return p, err
}

// likePattern escapes LIKE wildcards in q and wraps it for a substring match.
func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(strings.ToLower(q)) + "%"
}

// Feed returns one page of posts and whether there are more.
func (r *PostRepository) Feed(o FeedOptions) ([]models.PostSummary, bool, error) {
	query := postColumns
	args := []any{o.ViewerID, o.ViewerID}
	// Posts hidden by a moderator never appear in lists.
	conds := []string{"p.is_hidden = 0"}

	switch o.Filter {
	case "my_posts":
		conds = append(conds, "p.user_id = ?")
		args = append(args, o.UserID)
	case "liked":
		conds = append(conds, "EXISTS (SELECT 1 FROM post_reactions l WHERE l.post_id = p.id AND l.user_id = ? AND l.value = 1)")
		args = append(args, o.UserID)
	case "bookmarks":
		conds = append(conds, "EXISTS (SELECT 1 FROM bookmarks sb WHERE sb.post_id = p.id AND sb.user_id = ?)")
		args = append(args, o.UserID)
	}
	if o.Following {
		conds = append(conds, "p.user_id IN (SELECT following_id FROM follows WHERE follower_id = ?)")
		args = append(args, o.ViewerID)
	}
	if o.Category != "" {
		conds = append(conds, `p.id IN (SELECT pc.post_id FROM post_categories pc
			JOIN categories cat ON cat.id = pc.category_id WHERE cat.name = ?)`)
		args = append(args, o.Category)
	}
	if q := strings.TrimSpace(o.Query); q != "" {
		like := likePattern(q)
		conds = append(conds, `(ulower(p.title) LIKE ? ESCAPE '\' OR ulower(p.content) LIKE ? ESCAPE '\')`)
		args = append(args, like, like)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += postGroupBy
	switch o.Sort {
	case "top":
		query += ` ORDER BY likes DESC, p.created_at DESC, p.id DESC`
	case "discussed":
		query += ` ORDER BY comment_count DESC, p.created_at DESC, p.id DESC`
	default:
		query += ` ORDER BY p.created_at DESC, p.id DESC`
	}
	if o.Limit <= 0 {
		o.Limit = pageSize
	}
	query += ` LIMIT ? OFFSET ?`
	args = append(args, o.Limit+1, o.Offset)

	posts, err := r.queryPosts(query, args...)
	if err != nil {
		return nil, false, err
	}
	hasNext := len(posts) > o.Limit
	if hasNext {
		posts = posts[:o.Limit]
	}
	return posts, hasNext, r.attachCategories(posts)
}

// List is used by the profile tabs: the user's posts or the posts they liked.
func (r *PostRepository) List(filterType string, category string, currentUserID int, page int) ([]models.PostSummary, bool, error) {
	return r.Feed(FeedOptions{
		Filter: filterType, UserID: currentUserID, Category: category,
		ViewerID: currentUserID, Limit: pageSize, Offset: (page - 1) * pageSize,
	})
}

func (r *PostRepository) queryPosts(query string, args ...any) ([]models.PostSummary, error) {
	rows, err := r.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []models.PostSummary
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

func (r *PostRepository) attachCategories(posts []models.PostSummary) error {
	if len(posts) == 0 {
		return nil
	}
	ids := make([]int, len(posts))
	for i, p := range posts {
		ids[i] = p.ID
	}
	catMap, err := r.fetchCategoriesForPosts(ids)
	if err != nil {
		return err
	}
	for i := range posts {
		posts[i].Categories = catMap[posts[i].ID]
	}
	return nil
}

// Get returns one post; viewerID fills MyReaction (0 for guests).
func (r *PostRepository) Get(postID, viewerID int) (models.PostSummary, error) {
	p, err := scanPost(r.DB.QueryRow(postColumns+` WHERE p.id = ?`+postGroupBy, viewerID, viewerID, postID))
	if err != nil {
		return p, err
	}
	p.Categories, err = r.getCategories(p.ID)
	return p, err
}

// Reactions returns a post's like/dislike counts and the user's own reaction.
func (r *PostRepository) Reactions(postID, userID int) (likes, dislikes, mine int, err error) {
	err = r.DB.QueryRow(`
SELECT COALESCE(SUM(CASE WHEN value = 1 THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN value = -1 THEN 1 ELSE 0 END), 0),
       COALESCE(MAX(CASE WHEN user_id = ? THEN value END), 0)
FROM post_reactions WHERE post_id = ?`, userID, postID).Scan(&likes, &dislikes, &mine)
	return
}

// PopularSince returns the most liked posts created after since.
func (r *PostRepository) PopularSince(since time.Time, limit int) ([]models.PostSummary, error) {
	posts, err := r.queryPosts(postColumns+` WHERE p.created_at >= ? AND p.is_hidden = 0`+postGroupBy+
		` HAVING likes > 0 ORDER BY likes DESC, p.created_at DESC LIMIT ?`, 0, 0, since.UTC().Format("2006-01-02 15:04:05"), limit)
	return posts, err
}

// TopAuthors returns the users with the most posts.
func (r *PostRepository) TopAuthors(limit int) ([]models.AuthorStat, error) {
	rows, err := r.DB.Query(`
SELECT u.id, u.username, u.avatar, u.reputation, COUNT(p.id) AS n
FROM users u JOIN posts p ON p.user_id = u.id
GROUP BY u.id ORDER BY n DESC, u.username LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AuthorStat
	for rows.Next() {
		var a models.AuthorStat
		if err := rows.Scan(&a.ID, &a.Username, &a.Avatar, &a.Reputation, &a.Posts); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Stats returns forum-wide totals.
func (r *PostRepository) Stats() (models.ForumStats, error) {
	var s models.ForumStats
	err := r.DB.QueryRow(`SELECT (SELECT COUNT(*) FROM users), (SELECT COUNT(*) FROM posts WHERE is_hidden = 0), (SELECT COUNT(*) FROM comments)`).
		Scan(&s.Users, &s.Posts, &s.Comments)
	return s, err
}

func (r *PostRepository) fetchCategoriesForPosts(postIDs []int) (map[int][]string, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(postIDs)), ",")
	query := `SELECT pc.post_id, c.name FROM post_categories pc JOIN categories c ON c.id = pc.category_id WHERE pc.post_id IN (` + placeholders + `) ORDER BY c.name`
	args := make([]any, len(postIDs))
	for i, id := range postIDs {
		args[i] = id
	}
	rows, err := r.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int][]string)
	for rows.Next() {
		var pid int
		var name string
		if err := rows.Scan(&pid, &name); err != nil {
			return nil, err
		}
		result[pid] = append(result[pid], name)
	}
	return result, rows.Err()
}

func (r *PostRepository) getCategories(postID int) ([]string, error) {
	rows, err := r.DB.Query(`SELECT c.name FROM categories c JOIN post_categories pc ON pc.category_id = c.id WHERE pc.post_id = ? ORDER BY c.name`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cats []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		cats = append(cats, name)
	}
	return cats, rows.Err()
}

// TopCategories returns the categories the user posts in most often.
func (r *PostRepository) TopCategories(userID, limit int) ([]models.CategoryCount, error) {
	rows, err := r.DB.Query(`
SELECT c.name, COUNT(*) AS n
FROM posts p
JOIN post_categories pc ON pc.post_id = p.id
JOIN categories c ON c.id = pc.category_id
WHERE p.user_id = ?
GROUP BY c.id, c.name
ORDER BY n DESC, c.name
LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cats []models.CategoryCount
	for rows.Next() {
		var c models.CategoryCount
		if err := rows.Scan(&c.Name, &c.Count); err != nil {
			return nil, err
		}
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

// BestComment returns the post's best answer id (0 if none), its author and
// whether the post is in «Вопросы».
func (r *PostRepository) BestAnswerInfo(postID int) (bestID, authorID int, isQuestion bool, err error) {
	var best sql.NullInt64
	err = r.DB.QueryRow(`SELECT best_comment_id, user_id,
		EXISTS (SELECT 1 FROM post_categories pc JOIN categories c ON c.id = pc.category_id
		        WHERE pc.post_id = posts.id AND c.name = 'Вопросы')
		FROM posts WHERE id = ?`, postID).Scan(&best, &authorID, &isQuestion)
	return int(best.Int64), authorID, isQuestion, err
}

// SetBestComment sets (or with 0 clears) the post's best answer.
func (r *PostRepository) SetBestComment(postID, commentID int) error {
	var v any
	if commentID > 0 {
		v = commentID
	}
	_, err := r.DB.Exec(`UPDATE posts SET best_comment_id = ? WHERE id = ?`, v, postID)
	return err
}
