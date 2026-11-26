package repository

import (
	"database/sql"
	"forum/internal/models"
)

type NotificationRepository struct{ DB *sql.DB }

// Notification types.
const (
	NotifyFollow  = "follow"
	NotifyPost    = "post"
	NotifyComment = "comment"
	NotifyLike    = "like"
	NotifyReply   = "reply"
	NotifyMention = "mention"
	NotifyBest    = "best"
)

// Notify creates a notification for userID about actorID's action. Actions on
// your own content are skipped, and an identical unread notification isn't
// repeated (e.g. like → unlike → like).
func (r *NotificationRepository) Notify(userID int, kind string, actorID, postID int) error {
	return r.NotifyComment(userID, kind, actorID, postID, 0)
}

// NotifyComment is Notify with a link to a specific comment.
func (r *NotificationRepository) NotifyComment(userID int, kind string, actorID, postID, commentID int) error {
	if userID == actorID || userID == 0 {
		return nil
	}
	var post, comment any
	if postID > 0 {
		post = postID
	}
	if commentID > 0 {
		comment = commentID
	}
	_, err := r.DB.Exec(`
INSERT INTO notifications (user_id, type, actor_id, post_id, comment_id)
SELECT ?, ?, ?, ?, ?
WHERE NOT EXISTS (SELECT 1 FROM notifications
	WHERE user_id = ? AND type = ? AND actor_id = ? AND post_id IS ? AND comment_id IS ? AND is_read = 0)`,
		userID, kind, actorID, post, comment, userID, kind, actorID, post, comment)
	return err
}

// NotifyFollowers tells everyone who follows authorID about a new post.
func (r *NotificationRepository) NotifyFollowers(authorID, postID int) error {
	_, err := r.DB.Exec(`
INSERT INTO notifications (user_id, type, actor_id, post_id)
SELECT follower_id, ?, ?, ? FROM follows WHERE following_id = ?`, NotifyPost, authorID, postID, authorID)
	return err
}

func (r *NotificationRepository) UnreadCount(userID int) (int, error) {
	var n int
	err := r.DB.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND is_read = 0`, userID).Scan(&n)
	return n, err
}

// List returns the latest notifications with actor and post details.
func (r *NotificationRepository) List(userID, limit int) ([]models.Notification, error) {
	rows, err := r.DB.Query(`
SELECT n.id, n.type, n.actor_id, u.username, u.avatar, COALESCE(n.post_id, 0), COALESCE(p.title, ''),
       COALESCE(n.comment_id, 0), COALESCE(n.achievement, ''), n.is_read, n.created_at
FROM notifications n
JOIN users u ON u.id = n.actor_id
LEFT JOIN posts p ON p.id = n.post_id
WHERE n.user_id = ?
ORDER BY n.created_at DESC, n.id DESC
LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Notification
	for rows.Next() {
		var n models.Notification
		if err := rows.Scan(&n.ID, &n.Type, &n.ActorID, &n.ActorName, &n.ActorAvatar, &n.PostID, &n.PostTitle,
			&n.CommentID, &n.Achievement, &n.IsRead, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *NotificationRepository) MarkAllRead(userID int) error {
	_, err := r.DB.Exec(`UPDATE notifications SET is_read = 1 WHERE user_id = ? AND is_read = 0`, userID)
	return err
}
