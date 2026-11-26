package repository

import (
	"database/sql"
	"errors"
	"forum/internal/models"
	"time"
)

type MessageRepository struct{ DB *sql.DB }

const MessagesPageSize = 30

// pair orders two user IDs the way conversations store them.
func pair(a, b int) (int, int) {
	if a < b {
		return a, b
	}
	return b, a
}

// FindConversation returns the conversation between two users, or 0.
func (r *MessageRepository) FindConversation(a, b int) (int, error) {
	u1, u2 := pair(a, b)
	var id int
	err := r.DB.QueryRow(`SELECT id FROM conversations WHERE user1_id = ? AND user2_id = ?`, u1, u2).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// GetOrCreateConversation returns the pair's conversation, creating it once.
func (r *MessageRepository) GetOrCreateConversation(a, b int) (int, error) {
	u1, u2 := pair(a, b)
	if _, err := r.DB.Exec(`INSERT OR IGNORE INTO conversations (user1_id, user2_id) VALUES (?, ?)`, u1, u2); err != nil {
		return 0, err
	}
	return r.FindConversation(a, b)
}

// Partner returns the other participant if userID is in the conversation.
func (r *MessageRepository) Partner(convID, userID int) (int, bool, error) {
	var u1, u2 int
	err := r.DB.QueryRow(`SELECT user1_id, user2_id FROM conversations WHERE id = ?`, convID).Scan(&u1, &u2)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	switch userID {
	case u1:
		return u2, true, nil
	case u2:
		return u1, true, nil
	}
	return 0, false, nil
}

// Partners returns everyone the user has a conversation with.
func (r *MessageRepository) Partners(userID int) ([]int, error) {
	rows, err := r.DB.Query(`SELECT CASE WHEN user1_id = ? THEN user2_id ELSE user1_id END
		FROM conversations WHERE user1_id = ? OR user2_id = ?`, userID, userID, userID)
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

// List returns the user's conversations, most recent first, with the last
// message and the number of unread ones.
func (r *MessageRepository) List(userID int) ([]models.Conversation, error) {
	rows, err := r.DB.Query(`
SELECT c.id, u.id, u.username, u.avatar, u.last_seen_at,
       COALESCE(m.text, ''), COALESCE(m.sender_id, 0), COALESCE(m.created_at, c.created_at),
       (SELECT COUNT(*) FROM messages x WHERE x.conversation_id = c.id AND x.sender_id != ? AND x.is_read = 0)
FROM conversations c
JOIN users u ON u.id = CASE WHEN c.user1_id = ? THEN c.user2_id ELSE c.user1_id END
LEFT JOIN messages m ON m.id = (SELECT MAX(id) FROM messages WHERE conversation_id = c.id)
WHERE (c.user1_id = ? OR c.user2_id = ?) AND m.id IS NOT NULL
ORDER BY c.last_message_at DESC, c.id DESC`, userID, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Conversation
	for rows.Next() {
		var c models.Conversation
		var seen sql.NullTime
		var last string
		if err := rows.Scan(&c.ID, &c.PartnerID, &c.PartnerName, &c.PartnerAvatar, &seen,
			&c.LastText, &c.LastSenderID, &last, &c.Unread); err != nil {
			return nil, err
		}
		c.LastAt = parseSQLiteTime(last)
		if seen.Valid {
			c.PartnerLastSeen = &seen.Time
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// parseSQLiteTime parses a DATETIME that came back as text (COALESCE loses
// the column type, so the driver doesn't convert it).
func parseSQLiteTime(s string) time.Time {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339Nano, "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// Send stores a message and bumps the conversation.
func (r *MessageRepository) Send(convID, senderID int, text string) (models.Message, error) {
	tx, err := r.DB.Begin()
	if err != nil {
		return models.Message{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	res, err := tx.Exec(`INSERT INTO messages (conversation_id, sender_id, text) VALUES (?, ?, ?)`, convID, senderID, text)
	if err != nil {
		return models.Message{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Message{}, err
	}
	if _, err := tx.Exec(`UPDATE conversations SET last_message_at = CURRENT_TIMESTAMP WHERE id = ?`, convID); err != nil {
		return models.Message{}, err
	}
	var m models.Message
	err = tx.QueryRow(`SELECT id, conversation_id, sender_id, text, created_at, is_read, is_delivered FROM messages WHERE id = ?`, id).
		Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Text, &m.CreatedAt, &m.IsRead, &m.IsDelivered)
	if err != nil {
		return models.Message{}, err
	}
	return m, tx.Commit()
}

// History returns up to MessagesPageSize messages before beforeID (0 = the
// newest), oldest first, and whether older ones exist.
func (r *MessageRepository) History(convID, beforeID int) ([]models.Message, bool, error) {
	query := `SELECT id, conversation_id, sender_id, text, created_at, is_read, is_delivered
		FROM messages WHERE conversation_id = ?`
	args := []any{convID}
	if beforeID > 0 {
		query += ` AND id < ?`
		args = append(args, beforeID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, MessagesPageSize+1)
	rows, err := r.DB.Query(query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var msgs []models.Message
	for rows.Next() {
		var m models.Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Text, &m.CreatedAt, &m.IsRead, &m.IsDelivered); err != nil {
			return nil, false, err
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(msgs) > MessagesPageSize
	if more {
		msgs = msgs[:MessagesPageSize]
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, more, nil
}

// MarkRead marks the partner's messages in the conversation as read and
// returns the newest id marked (0 if none).
func (r *MessageRepository) MarkRead(convID, readerID int) (int, error) {
	var maxID sql.NullInt64
	if err := r.DB.QueryRow(`SELECT MAX(id) FROM messages WHERE conversation_id = ? AND sender_id != ? AND is_read = 0`,
		convID, readerID).Scan(&maxID); err != nil || !maxID.Valid {
		return 0, err
	}
	_, err := r.DB.Exec(`UPDATE messages SET is_read = 1, is_delivered = 1
		WHERE conversation_id = ? AND sender_id != ? AND id <= ? AND is_read = 0`, convID, readerID, maxID.Int64)
	return int(maxID.Int64), err
}

// MarkDelivered marks one message as delivered.
func (r *MessageRepository) MarkDelivered(id int) error {
	_, err := r.DB.Exec(`UPDATE messages SET is_delivered = 1 WHERE id = ?`, id)
	return err
}

// DeliverPending marks every undelivered message addressed to userID as
// delivered and returns their ids grouped by sender, so senders can be told.
func (r *MessageRepository) DeliverPending(userID int) (map[int][]int, error) {
	rows, err := r.DB.Query(`
SELECT m.id, m.sender_id FROM messages m JOIN conversations c ON c.id = m.conversation_id
WHERE (c.user1_id = ? OR c.user2_id = ?) AND m.sender_id != ? AND m.is_delivered = 0`, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	bySender := map[int][]int{}
	var ids []any
	for rows.Next() {
		var id, sender int
		if err := rows.Scan(&id, &sender); err != nil {
			rows.Close()
			return nil, err
		}
		bySender[sender] = append(bySender[sender], id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if _, err := r.DB.Exec(`UPDATE messages SET is_delivered = 1 WHERE id = ?`, id); err != nil {
			return nil, err
		}
	}
	return bySender, nil
}

// UnreadCount is the number of unread messages addressed to the user.
func (r *MessageRepository) UnreadCount(userID int) (int, error) {
	var n int
	err := r.DB.QueryRow(`
SELECT COUNT(*) FROM messages m JOIN conversations c ON c.id = m.conversation_id
WHERE (c.user1_id = ? OR c.user2_id = ?) AND m.sender_id != ? AND m.is_read = 0`, userID, userID, userID).Scan(&n)
	return n, err
}

// TouchLastSeen records when the user was last online.
func (r *MessageRepository) TouchLastSeen(userID int) (time.Time, error) {
	now := time.Now().UTC()
	_, err := r.DB.Exec(`UPDATE users SET last_seen_at = ? WHERE id = ?`, now.Format("2006-01-02 15:04:05"), userID)
	return now, err
}
