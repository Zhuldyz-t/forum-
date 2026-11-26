package repository

import (
	"database/sql"
	"errors"
	"forum/internal/models"
)

type CommentRepository struct{ DB *sql.DB }

// MaxCommentDepth is how deep replies nest; deeper replies attach to the
// last level.
const MaxCommentDepth = 3

// CommentRef is the minimal data needed to reply to or change a comment.
type CommentRef struct {
	ID        int
	PostID    int
	UserID    int
	ParentID  int
	IsDeleted bool
	Depth     int // 1 for top-level comments
}

// Ref loads a comment and computes its depth in the thread.
func (r *CommentRepository) Ref(id int) (CommentRef, error) {
	var c CommentRef
	var parent sql.NullInt64
	err := r.DB.QueryRow(`SELECT id, post_id, user_id, parent_id, is_deleted FROM comments WHERE id = ?`, id).
		Scan(&c.ID, &c.PostID, &c.UserID, &parent, &c.IsDeleted)
	if err != nil {
		return c, err
	}
	c.ParentID = int(parent.Int64)
	c.Depth = 1
	for p := c.ParentID; p != 0 && c.Depth <= MaxCommentDepth; c.Depth++ {
		var next sql.NullInt64
		if err := r.DB.QueryRow(`SELECT parent_id FROM comments WHERE id = ?`, p).Scan(&next); err != nil {
			return c, err
		}
		p = int(next.Int64)
	}
	return c, nil
}

// Create adds a comment (parentID 0 for top level) and returns its id.
func (r *CommentRepository) Create(postID, userID, parentID int, content string) (int, error) {
	var parent any
	if parentID > 0 {
		parent = parentID
	}
	res, err := r.DB.Exec(`INSERT INTO comments (post_id, user_id, parent_id, content) VALUES (?, ?, ?, ?)`,
		postID, userID, parent, content)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

// Update edits the author's own comment. It reports whether a row changed.
func (r *CommentRepository) Update(id, userID int, content string) (bool, error) {
	res, err := r.DB.Exec(`UPDATE comments SET content = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND user_id = ? AND is_deleted = 0`, content, id, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeleteByAuthor removes the author's comment. A comment that has replies
// becomes a «Комментарий удалён» placeholder so the thread stays intact; a
// leaf is deleted, together with placeholders it leaves without replies.
func (r *CommentRepository) DeleteByAuthor(commentID, userID int) (bool, error) {
	tx, err := r.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck

	var parent sql.NullInt64
	err = tx.QueryRow(`SELECT parent_id FROM comments WHERE id = ? AND user_id = ? AND is_deleted = 0`, commentID, userID).Scan(&parent)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	var replies int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM comments WHERE parent_id = ?`, commentID).Scan(&replies); err != nil {
		return false, err
	}
	if replies > 0 {
		if _, err := tx.Exec(`UPDATE comments SET is_deleted = 1, content = '' WHERE id = ?`, commentID); err != nil {
			return false, err
		}
		if _, err := tx.Exec(`DELETE FROM comment_reactions WHERE comment_id = ?`, commentID); err != nil {
			return false, err
		}
		if _, err := tx.Exec(`DELETE FROM comment_emoji_reactions WHERE comment_id = ?`, commentID); err != nil {
			return false, err
		}
		// A deleted comment can't stay the best answer.
		if _, err := tx.Exec(`UPDATE posts SET best_comment_id = NULL WHERE best_comment_id = ?`, commentID); err != nil {
			return false, err
		}
		return true, tx.Commit()
	}

	if _, err := tx.Exec(`DELETE FROM comments WHERE id = ?`, commentID); err != nil {
		return false, err
	}
	// Remove placeholders up the thread that no longer have replies.
	for p := parent; p.Valid; {
		var deleted bool
		var next sql.NullInt64
		if err := tx.QueryRow(`SELECT is_deleted, parent_id FROM comments WHERE id = ?`, p.Int64).Scan(&deleted, &next); err != nil {
			break
		}
		var left int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM comments WHERE parent_id = ?`, p.Int64).Scan(&left); err != nil {
			return false, err
		}
		if !deleted || left > 0 {
			break
		}
		if _, err := tx.Exec(`DELETE FROM comments WHERE id = ?`, p.Int64); err != nil {
			return false, err
		}
		p = next
	}
	return true, tx.Commit()
}

// ListByPost returns every comment of the post in posting order, flat; the
// handler builds the reply tree.
func (r *CommentRepository) ListByPost(postID int) ([]models.CommentView, error) {
	rows, err := r.DB.Query(`
SELECT c.id, c.post_id, c.user_id, COALESCE(c.parent_id, 0), u.username, u.avatar, u.reputation, c.content, c.created_at,
       c.updated_at, c.is_deleted, c.is_hidden,
       COALESCE(SUM(CASE WHEN cr.value = 1 THEN 1 ELSE 0 END), 0) AS likes,
       COALESCE(SUM(CASE WHEN cr.value = -1 THEN 1 ELSE 0 END), 0) AS dislikes
FROM comments c
JOIN users u ON u.id = c.user_id
LEFT JOIN comment_reactions cr ON cr.comment_id = c.id
WHERE c.post_id = ?
GROUP BY c.id
ORDER BY c.created_at ASC, c.id ASC`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var comments []models.CommentView
	for rows.Next() {
		var c models.CommentView
		var updated sql.NullTime
		if err := rows.Scan(&c.ID, &c.PostID, &c.UserID, &c.ParentID, &c.AuthorName, &c.AuthorAvatar, &c.AuthorRep, &c.Content,
			&c.CreatedAt, &updated, &c.IsDeleted, &c.IsHidden, &c.Likes, &c.Dislikes); err != nil {
			return nil, err
		}
		if updated.Valid {
			c.UpdatedAt = &updated.Time
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

// RootPosition returns the 1-based position of a top-level comment among the
// post's top-level comments, to find its page.
func (r *CommentRepository) RootPosition(postID, rootID int) (int, error) {
	var n int
	err := r.DB.QueryRow(`SELECT COUNT(*) FROM comments WHERE post_id = ? AND parent_id IS NULL AND id <= ?`, postID, rootID).Scan(&n)
	return n, err
}

// ListByUser returns the user's latest comments with the title of each post.
func (r *CommentRepository) ListByUser(userID, limit int) ([]models.UserComment, error) {
	rows, err := r.DB.Query(`
SELECT c.id, c.post_id, p.title, c.content, c.created_at
FROM comments c
JOIN posts p ON p.id = c.post_id
WHERE c.user_id = ? AND c.is_deleted = 0
ORDER BY c.created_at DESC, c.id DESC
LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []models.UserComment
	for rows.Next() {
		var c models.UserComment
		if err := rows.Scan(&c.ID, &c.PostID, &c.PostTitle, &c.Content, &c.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}
