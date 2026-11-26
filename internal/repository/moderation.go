package repository

import (
	"database/sql"
	"errors"
	"forum/internal/models"
	"strings"
)

type ModerationRepository struct{ DB *sql.DB }

var ErrDuplicateReport = errors.New("already reported")

// ─── Reports ────────────────────────────────────────────────

// Report files a complaint about a post or comment (once per user and target).
func (r *ModerationRepository) Report(reporterID int, targetType string, targetID int, reason, details string) error {
	res, err := r.DB.Exec(`INSERT OR IGNORE INTO reports (reporter_id, target_type, target_id, reason, details)
		VALUES (?, ?, ?, ?, ?)`, reporterID, targetType, targetID, reason, details)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrDuplicateReport
	}
	return nil
}

// Reports lists complaints for the admin panel, filtered by status and type.
func (r *ModerationRepository) Reports(status, targetType string, limit int) ([]models.Report, error) {
	query := `
SELECT r.id, r.reporter_id, u.username, r.target_type, r.target_id, r.reason, r.details, r.status, r.created_at,
       CASE r.target_type
         WHEN 'post' THEN COALESCE((SELECT title FROM posts WHERE id = r.target_id), '')
         ELSE COALESCE((SELECT content FROM comments WHERE id = r.target_id AND is_deleted = 0), '') END,
       CASE r.target_type
         WHEN 'post' THEN COALESCE((SELECT id FROM posts WHERE id = r.target_id), 0)
         ELSE COALESCE((SELECT post_id FROM comments WHERE id = r.target_id), 0) END,
       CASE r.target_type
         WHEN 'post' THEN COALESCE((SELECT is_hidden FROM posts WHERE id = r.target_id), 0)
         ELSE COALESCE((SELECT is_hidden FROM comments WHERE id = r.target_id), 0) END,
       CASE r.target_type
         WHEN 'post' THEN COALESCE((SELECT a.username FROM posts p JOIN users a ON a.id = p.user_id WHERE p.id = r.target_id), '')
         ELSE COALESCE((SELECT a.username FROM comments c JOIN users a ON a.id = c.user_id WHERE c.id = r.target_id), '') END
FROM reports r JOIN users u ON u.id = r.reporter_id
WHERE 1 = 1`
	var args []any
	if status != "" {
		query += ` AND r.status = ?`
		args = append(args, status)
	}
	if targetType != "" {
		query += ` AND r.target_type = ?`
		args = append(args, targetType)
	}
	query += ` ORDER BY r.status = 'open' DESC, r.created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Report
	for rows.Next() {
		var rp models.Report
		if err := rows.Scan(&rp.ID, &rp.ReporterID, &rp.ReporterName, &rp.TargetType, &rp.TargetID, &rp.Reason, &rp.Details,
			&rp.Status, &rp.CreatedAt, &rp.Excerpt, &rp.PostID, &rp.Hidden, &rp.AuthorName); err != nil {
			return nil, err
		}
		out = append(out, rp)
	}
	return out, rows.Err()
}

// CloseReports marks every open report about a target as resolved or dismissed.
func (r *ModerationRepository) CloseReports(targetType string, targetID int, status string, by int) error {
	_, err := r.DB.Exec(`UPDATE reports SET status = ?, resolved_by = ?, resolved_at = CURRENT_TIMESTAMP
		WHERE target_type = ? AND target_id = ? AND status = 'open'`, status, by, targetType, targetID)
	return err
}

// ReportTarget returns what a report is about.
func (r *ModerationRepository) ReportTarget(id int) (string, int, error) {
	var t string
	var target int
	err := r.DB.QueryRow(`SELECT target_type, target_id FROM reports WHERE id = ?`, id).Scan(&t, &target)
	return t, target, err
}

func (r *ModerationRepository) OpenReportCount() (int, error) {
	var n int
	err := r.DB.QueryRow(`SELECT COUNT(*) FROM reports WHERE status = 'open'`).Scan(&n)
	return n, err
}

// ─── Hiding and removing content ────────────────────────────

func (r *ModerationRepository) SetPostHidden(id int, hidden bool) (bool, error) {
	return affected(r.DB.Exec(`UPDATE posts SET is_hidden = ? WHERE id = ?`, hidden, id))
}

func (r *ModerationRepository) SetCommentHidden(id int, hidden bool) (bool, error) {
	return affected(r.DB.Exec(`UPDATE comments SET is_hidden = ? WHERE id = ? AND is_deleted = 0`, hidden, id))
}

func affected(res sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ─── Requests: moderator applications, user deletion ────────

// Apply files a request; a user may have only one pending request of a
// type (per target for deletions).
func (r *ModerationRepository) Apply(kind string, userID, targetID int, text string) (bool, error) {
	var target any
	if targetID > 0 {
		target = targetID
	}
	var exists int
	err := r.DB.QueryRow(`SELECT COUNT(*) FROM requests WHERE type = ? AND user_id = ? AND target_user_id IS ? AND status = 'pending'`,
		kind, userID, target).Scan(&exists)
	if err != nil || exists > 0 {
		return false, err
	}
	_, err = r.DB.Exec(`INSERT INTO requests (type, user_id, target_user_id, text) VALUES (?, ?, ?, ?)`, kind, userID, target, text)
	return err == nil, err
}

// PendingApplication reports whether the user has a pending moderator application.
func (r *ModerationRepository) PendingApplication(userID int) (bool, error) {
	var n int
	err := r.DB.QueryRow(`SELECT COUNT(*) FROM requests WHERE type = 'moderator' AND user_id = ? AND status = 'pending'`, userID).Scan(&n)
	return n > 0, err
}

func (r *ModerationRepository) Requests(status string, limit int) ([]models.Request, error) {
	query := `
SELECT q.id, q.type, q.user_id, u.username, COALESCE(q.target_user_id, 0), COALESCE(t.username, ''), q.text, q.status, q.created_at
FROM requests q JOIN users u ON u.id = q.user_id LEFT JOIN users t ON t.id = q.target_user_id
WHERE 1 = 1`
	var args []any
	if status != "" {
		query += ` AND q.status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY q.status = 'pending' DESC, q.created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Request
	for rows.Next() {
		var q models.Request
		if err := rows.Scan(&q.ID, &q.Type, &q.UserID, &q.UserName, &q.TargetID, &q.TargetName, &q.Text, &q.Status, &q.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (r *ModerationRepository) Request(id int) (models.Request, error) {
	var q models.Request
	err := r.DB.QueryRow(`SELECT id, type, user_id, COALESCE(target_user_id, 0), status FROM requests WHERE id = ?`, id).
		Scan(&q.ID, &q.Type, &q.UserID, &q.TargetID, &q.Status)
	return q, err
}

func (r *ModerationRepository) DecideRequest(id int, approved bool, by int) error {
	status := "rejected"
	if approved {
		status = "approved"
	}
	_, err := r.DB.Exec(`UPDATE requests SET status = ?, decided_by = ?, decided_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'pending'`, status, by, id)
	return err
}

func (r *ModerationRepository) PendingRequestCount() (int, error) {
	var n int
	err := r.DB.QueryRow(`SELECT COUNT(*) FROM requests WHERE status = 'pending'`).Scan(&n)
	return n, err
}

// ─── Audit log ──────────────────────────────────────────────

func (r *ModerationRepository) Log(actor *models.User, action, targetType string, targetID int, details string) error {
	_, err := r.DB.Exec(`INSERT INTO mod_log (actor_id, actor_name, action, target_type, target_id, details)
		VALUES (?, ?, ?, ?, ?, ?)`, actor.ID, actor.Username, action, targetType, targetID, details)
	return err
}

// LogEntries lists the log, newest first, filtered by action and by a
// search in the actor's name or the details.
func (r *ModerationRepository) LogEntries(action, q string, limit int) ([]models.LogEntry, error) {
	query := `SELECT id, actor_id, actor_name, action, target_type, target_id, details, created_at FROM mod_log WHERE 1 = 1`
	var args []any
	if action != "" {
		query += ` AND action = ?`
		args = append(args, action)
	}
	if q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(q)) + "%"
		query += ` AND (ulower(actor_name) LIKE ? ESCAPE '\' OR ulower(details) LIKE ? ESCAPE '\')`
		args = append(args, like, like)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.LogEntry
	for rows.Next() {
		var e models.LogEntry
		if err := rows.Scan(&e.ID, &e.ActorID, &e.ActorName, &e.Action, &e.TargetType, &e.TargetID, &e.Details, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ─── Categories ─────────────────────────────────────────────

func (r *ModerationRepository) CreateCategory(name string) error {
	_, err := r.DB.Exec(`INSERT INTO categories (name) VALUES (?)`, name)
	return err
}

func (r *ModerationRepository) RenameCategory(id int, name string) (string, error) {
	var old string
	if err := r.DB.QueryRow(`SELECT name FROM categories WHERE id = ?`, id).Scan(&old); err != nil {
		return "", err
	}
	_, err := r.DB.Exec(`UPDATE categories SET name = ? WHERE id = ?`, name, id)
	return old, err
}

// DeleteCategory removes a category; posts keep their other categories.
func (r *ModerationRepository) DeleteCategory(id int) (string, error) {
	var name string
	if err := r.DB.QueryRow(`SELECT name FROM categories WHERE id = ?`, id).Scan(&name); err != nil {
		return "", err
	}
	_, err := r.DB.Exec(`DELETE FROM categories WHERE id = ?`, id)
	return name, err
}
