package repository

import (
	"database/sql"
	"forum/internal/models"
	"strings"
)

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

func (r *PostRepository) List(filterType string, category string, currentUserID int) ([]models.PostSummary, error) {
	base := `
SELECT p.id, p.title, p.content, p.user_id, u.username, p.created_at,
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
		base += ` JOIN post_categories pc ON pc.post_id = p.id JOIN categories c ON c.id = pc.category_id `
		conditions = append(conditions, "c.name = ?")
		args = append(args, category)
	}

	query := base
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += ` GROUP BY p.id, p.title, p.content, p.user_id, u.username, p.created_at ORDER BY p.created_at DESC`

	rows, err := r.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []models.PostSummary
	for rows.Next() {
		var p models.PostSummary
		if err := rows.Scan(&p.ID, &p.Title, &p.Content, &p.AuthorID, &p.AuthorName, &p.CreatedAt, &p.Likes, &p.Dislikes, &p.CommentCount); err != nil {
			return nil, err
		}
		cats, err := r.getCategories(p.ID)
		if err != nil {
			return nil, err
		}
		p.Categories = cats
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

func (r *PostRepository) Get(postID int) (models.PostSummary, error) {
	var p models.PostSummary
	query := `
SELECT p.id, p.title, p.content, p.user_id, u.username, p.created_at,
       COALESCE(SUM(CASE WHEN pr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
       COALESCE(SUM(CASE WHEN pr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes,
       (SELECT COUNT(*) FROM comments c WHERE c.post_id = p.id) AS comment_count
FROM posts p
JOIN users u ON u.id = p.user_id
LEFT JOIN post_reactions pr ON pr.post_id = p.id
WHERE p.id = ?
GROUP BY p.id, p.title, p.content, p.user_id, u.username, p.created_at`
	if err := r.DB.QueryRow(query, postID).Scan(&p.ID, &p.Title, &p.Content, &p.AuthorID, &p.AuthorName, &p.CreatedAt, &p.Likes, &p.Dislikes, &p.CommentCount); err != nil {
		return p, err
	}
	cats, err := r.getCategories(p.ID)
	if err != nil {
		return p, err
	}
	p.Categories = cats
	return p, nil
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
