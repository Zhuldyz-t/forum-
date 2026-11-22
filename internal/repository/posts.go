package repository

import (
	"database/sql"
	"forum/internal/models"
	"strings"
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

func (r *PostRepository) Create(userID int, title, content string, categoryIDs []int) (int, error) {
	tx, err := r.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO posts (user_id, title, content) VALUES (?, ?, ?)`, userID, title, content)
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

func (r *PostRepository) Update(postID, userID int, title, content string, categoryIDs []int) (bool, error) {
	tx, err := r.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE posts SET title = ?, content = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?`,
		title, content, postID, userID,
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

func (r *PostRepository) List(filterType string, category string, currentUserID int, page int) ([]models.PostSummary, bool, error) {
	base := `
SELECT p.id, p.title, p.content, p.user_id, u.username, u.avatar, p.created_at, p.updated_at,
       COALESCE(SUM(CASE WHEN pr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
       COALESCE(SUM(CASE WHEN pr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes,
       (SELECT COUNT(*) FROM comments c WHERE c.post_id = p.id) AS comment_count
FROM posts p
JOIN users u ON u.id = p.user_id
LEFT JOIN post_reactions pr ON pr.post_id = p.id
`
	var conditions []string
	var args []any

	switch filterType {
	case "my_posts":
		conditions = append(conditions, "p.user_id = ?")
		args = append(args, currentUserID)
	case "liked":
		base += ` JOIN post_reactions liked_pr ON liked_pr.post_id = p.id `
		conditions = append(conditions, "liked_pr.user_id = ? AND liked_pr.value = 1")
		args = append(args, currentUserID)
	}

	if category != "" {
		conditions = append(conditions, `p.id IN (
			SELECT pc2.post_id FROM post_categories pc2
			JOIN categories cat ON cat.id = pc2.category_id
			WHERE cat.name = ?
		)`)
		args = append(args, category)
	}

	query := base
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += ` GROUP BY p.id, p.title, p.content, p.user_id, u.username, u.avatar, p.created_at, p.updated_at ORDER BY p.created_at DESC`

	offset := (page - 1) * pageSize
	query += ` LIMIT ? OFFSET ?`
	args = append(args, pageSize+1, offset)

	rows, err := r.DB.Query(query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var posts []models.PostSummary
	for rows.Next() {
		var p models.PostSummary
		var updatedAt sql.NullTime
		if err := rows.Scan(&p.ID, &p.Title, &p.Content, &p.AuthorID, &p.AuthorName, &p.AuthorAvatar, &p.CreatedAt, &updatedAt, &p.Likes, &p.Dislikes, &p.CommentCount); err != nil {
			return nil, false, err
		}
		if updatedAt.Valid {
			p.UpdatedAt = &updatedAt.Time
		}
		posts = append(posts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	hasNext := len(posts) > pageSize
	if hasNext {
		posts = posts[:pageSize]
	}

	if len(posts) > 0 {
		ids := make([]int, len(posts))
		for i, p := range posts {
			ids[i] = p.ID
		}
		catMap, err := r.fetchCategoriesForPosts(ids)
		if err != nil {
			return nil, false, err
		}
		for i := range posts {
			posts[i].Categories = catMap[posts[i].ID]
		}
	}

	return posts, hasNext, nil
}

func (r *PostRepository) Search(q string, page int) ([]models.PostSummary, bool, error) {
	like := "%" + q + "%"
	query := `
SELECT p.id, p.title, p.content, p.user_id, u.username, u.avatar, p.created_at, p.updated_at,
       COALESCE(SUM(CASE WHEN pr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
       COALESCE(SUM(CASE WHEN pr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes,
       (SELECT COUNT(*) FROM comments c WHERE c.post_id = p.id) AS comment_count
FROM posts p
JOIN users u ON u.id = p.user_id
LEFT JOIN post_reactions pr ON pr.post_id = p.id
WHERE p.title LIKE ? OR p.content LIKE ?
GROUP BY p.id, p.title, p.content, p.user_id, u.username, u.avatar, p.created_at, p.updated_at
ORDER BY p.created_at DESC
LIMIT ? OFFSET ?`

	offset := (page - 1) * pageSize
	rows, err := r.DB.Query(query, like, like, pageSize+1, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var posts []models.PostSummary
	for rows.Next() {
		var p models.PostSummary
		var updatedAt sql.NullTime
		if err := rows.Scan(&p.ID, &p.Title, &p.Content, &p.AuthorID, &p.AuthorName, &p.AuthorAvatar, &p.CreatedAt, &updatedAt, &p.Likes, &p.Dislikes, &p.CommentCount); err != nil {
			return nil, false, err
		}
		if updatedAt.Valid {
			p.UpdatedAt = &updatedAt.Time
		}
		posts = append(posts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	hasNext := len(posts) > pageSize
	if hasNext {
		posts = posts[:pageSize]
	}

	if len(posts) > 0 {
		ids := make([]int, len(posts))
		for i, p := range posts {
			ids[i] = p.ID
		}
		catMap, err := r.fetchCategoriesForPosts(ids)
		if err != nil {
			return nil, false, err
		}
		for i := range posts {
			posts[i].Categories = catMap[posts[i].ID]
		}
	}

	return posts, hasNext, nil
}

func (r *PostRepository) Get(postID int) (models.PostSummary, error) {
	var p models.PostSummary
	var updatedAt sql.NullTime
	query := `
SELECT p.id, p.title, p.content, p.user_id, u.username, u.avatar, p.created_at, p.updated_at,
       COALESCE(SUM(CASE WHEN pr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
       COALESCE(SUM(CASE WHEN pr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes,
       (SELECT COUNT(*) FROM comments c WHERE c.post_id = p.id) AS comment_count
FROM posts p
JOIN users u ON u.id = p.user_id
LEFT JOIN post_reactions pr ON pr.post_id = p.id
WHERE p.id = ?
GROUP BY p.id, p.title, p.content, p.user_id, u.username, u.avatar, p.created_at, p.updated_at`
	if err := r.DB.QueryRow(query, postID).Scan(&p.ID, &p.Title, &p.Content, &p.AuthorID, &p.AuthorName, &p.AuthorAvatar, &p.CreatedAt, &updatedAt, &p.Likes, &p.Dislikes, &p.CommentCount); err != nil {
		return p, err
	}
	if updatedAt.Valid {
		p.UpdatedAt = &updatedAt.Time
	}
	cats, err := r.getCategories(p.ID)
	if err != nil {
		return p, err
	}
	p.Categories = cats
	return p, nil
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
