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

func (r *CommentRepository) DeleteByAuthor(commentID, userID int) (bool, error) {
	res, err := r.DB.Exec(`DELETE FROM comments WHERE id = ? AND user_id = ?`, commentID, userID)
	if err != nil {
		return false, err
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

func (r *CommentRepository) ListByPost(postID int, page int) ([]models.CommentView, bool, error) {
	query := `
SELECT c.id, c.post_id, c.user_id, u.username, c.content, c.created_at,
       COALESCE(SUM(CASE WHEN cr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
       COALESCE(SUM(CASE WHEN cr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes
FROM comments c
JOIN users u ON u.id = c.user_id
LEFT JOIN comment_reactions cr ON cr.comment_id = c.id
WHERE c.post_id = ?
GROUP BY c.id, c.post_id, c.user_id, u.username, c.content, c.created_at
ORDER BY c.created_at ASC
LIMIT ? OFFSET ?`
	offset := (page - 1) * pageSize
	rows, err := r.DB.Query(query, postID, pageSize+1, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var comments []models.CommentView
	for rows.Next() {
		var c models.CommentView
		if err := rows.Scan(&c.ID, &c.PostID, &c.UserID, &c.AuthorName, &c.Content, &c.CreatedAt, &c.Likes, &c.Dislikes); err != nil {
			return nil, false, err
		}
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasNext := len(comments) > pageSize
	if hasNext {
		comments = comments[:pageSize]
	}
	return comments, hasNext, nil
}
