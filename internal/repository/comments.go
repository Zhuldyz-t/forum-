package repository

import (
	"database/sql"
	"forum/internal/models"
)

type CommentRepository struct{ DB *sql.DB }

func (r *CommentRepository) Create(postID, userID int, content string) error {
	_, err := r.DB.Exec(`INSERT INTO comments (post_id, user_id, content) VALUES (?, ?, ?)`, postID, userID, content)
	return err
}

func (r *CommentRepository) ListByPost(postID int) ([]models.CommentView, error) {
	query := `
SELECT c.id, c.post_id, c.user_id, u.username, c.content, c.created_at,
       COALESCE(SUM(CASE WHEN cr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
       COALESCE(SUM(CASE WHEN cr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes
FROM comments c
JOIN users u ON u.id = c.user_id
LEFT JOIN comment_reactions cr ON cr.comment_id = c.id
WHERE c.post_id = ?
GROUP BY c.id, c.post_id, c.user_id, u.username, c.content, c.created_at
ORDER BY c.created_at ASC`
	rows, err := r.DB.Query(query, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var comments []models.CommentView
	for rows.Next() {
		var c models.CommentView
		if err := rows.Scan(&c.ID, &c.PostID, &c.UserID, &c.AuthorName, &c.Content, &c.CreatedAt, &c.Likes, &c.Dislikes); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}
