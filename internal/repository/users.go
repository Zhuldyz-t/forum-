package repository

import (
	"database/sql"
	"errors"
	"forum/internal/models"
	"sort"
	"strings"
	"time"
)

type UserRepository struct{ DB *sql.DB }

func (r *UserRepository) Create(email, username, passwordHash string) error {
	_, err := r.DB.Exec(`INSERT INTO users (email, username, password_hash) VALUES (?, ?, ?)`, email, username, passwordHash)
	return err
}

const userColumns = `SELECT id, email, username, password_hash, avatar, bio, cover_image, cover_preset,
	reputation, role, banned_until, ban_reason, created_at FROM users `

func scanUser(row *sql.Row) (*models.User, error) {
	u := &models.User{}
	var banned sql.NullTime
	err := row.Scan(&u.ID, &u.Email, &u.Username, &u.PasswordHash, &u.Avatar, &u.Bio, &u.CoverImage, &u.CoverPreset,
		&u.Reputation, &u.Role, &banned, &u.BanReason, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	if banned.Valid {
		u.BannedUntil = &banned.Time
	}
	return u, nil
}

func (r *UserRepository) GetByEmail(email string) (*models.User, error) {
	return scanUser(r.DB.QueryRow(userColumns+`WHERE email = ? COLLATE NOCASE`, email))
}

func (r *UserRepository) GetByUsername(username string) (*models.User, error) {
	return scanUser(r.DB.QueryRow(userColumns+`WHERE username = ?`, username))
}

func (r *UserRepository) GetByID(id int) (*models.User, error) {
	return scanUser(r.DB.QueryRow(userColumns+`WHERE id = ?`, id))
}

func (r *UserRepository) EmailExists(email string) (bool, error) {
	var exists int
	err := r.DB.QueryRow(`SELECT 1 FROM users WHERE email = ? LIMIT 1`, email).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *UserRepository) UsernameExists(username string) (bool, error) {
	var exists int
	err := r.DB.QueryRow(`SELECT 1 FROM users WHERE username = ? LIMIT 1`, username).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *UserRepository) UpdateUsername(id int, username string) error {
	_, err := r.DB.Exec(`UPDATE users SET username = ? WHERE id = ?`, username, id)
	return err
}

func (r *UserRepository) UpdateAvatar(id int, avatar string) error {
	_, err := r.DB.Exec(`UPDATE users SET avatar = ? WHERE id = ?`, avatar, id)
	return err
}

func (r *UserRepository) UpdateBio(id int, bio string) error {
	_, err := r.DB.Exec(`UPDATE users SET bio = ? WHERE id = ?`, bio, id)
	return err
}

// LikesReceived counts likes on all of the user's posts and comments.
func (r *UserRepository) LikesReceived(id int) (int, error) {
	var n int
	err := r.DB.QueryRow(`
SELECT (SELECT COUNT(*) FROM post_reactions pr JOIN posts p ON p.id = pr.post_id
        WHERE p.user_id = ? AND pr.value = 1)
     + (SELECT COUNT(*) FROM comment_emoji_reactions cr JOIN comments c ON c.id = cr.comment_id
        WHERE c.user_id = ? AND cr.kind = 'like')`, id, id).Scan(&n)
	return n, err
}

func (r *UserRepository) UpdateCover(id int, image, preset string) error {
	_, err := r.DB.Exec(`UPDATE users SET cover_image = ?, cover_preset = ? WHERE id = ?`, image, preset, id)
	return err
}

// RecentActivity returns the user's latest posts, comments and likes, newest
// first. Each source is queried separately and merged, which keeps the
// DATETIME columns typed (a UNION would hand them back as plain strings).
// Likes are private, so they're included only when includeLikes is set.
func (r *UserRepository) RecentActivity(id, limit int, includeLikes bool) ([]models.Activity, error) {
	queries := []struct{ kind, sql string }{
		{"post", `SELECT p.id, p.title, p.created_at FROM posts p
			WHERE p.user_id = ? ORDER BY p.created_at DESC LIMIT ?`},
		{"comment", `SELECT c.post_id, p.title, c.created_at FROM comments c JOIN posts p ON p.id = c.post_id
			WHERE c.user_id = ? AND c.is_deleted = 0 ORDER BY c.created_at DESC LIMIT ?`},
		{"like", `SELECT pr.post_id, p.title, pr.created_at FROM post_reactions pr JOIN posts p ON p.id = pr.post_id
			WHERE pr.user_id = ? AND pr.value = 1 AND pr.created_at IS NOT NULL ORDER BY pr.created_at DESC LIMIT ?`},
	}
	if !includeLikes {
		queries = queries[:2]
	}
	var all []models.Activity
	for _, q := range queries {
		rows, err := r.DB.Query(q.sql, id, limit)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			a := models.Activity{Kind: q.kind}
			if err := rows.Scan(&a.PostID, &a.PostTitle, &a.At); err != nil {
				rows.Close()
				return nil, err
			}
			all = append(all, a)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].At.After(all[j].At) })
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// MentionTarget is a user that can be @mentioned.
type MentionTarget struct {
	ID       int
	Username string
}

// ByUsernames resolves lowercased usernames to existing users.
func (r *UserRepository) ByUsernames(names []string) (map[string]MentionTarget, error) {
	out := map[string]MentionTarget{}
	for _, n := range names {
		var t MentionTarget
		err := r.DB.QueryRow(`SELECT id, username FROM users WHERE username = ? COLLATE NOCASE`, n).Scan(&t.ID, &t.Username)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[n] = t
	}
	return out, nil
}

// SearchByPrefix finds users whose name starts with prefix, for @mentions.
func (r *UserRepository) SearchByPrefix(prefix string, limit int) ([]models.AuthorStat, error) {
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix)
	rows, err := r.DB.Query(`SELECT id, username, avatar FROM users
		WHERE username LIKE ? ESCAPE '\' ORDER BY length(username), username LIMIT ?`, esc+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AuthorStat
	for rows.Next() {
		var a models.AuthorStat
		if err := rows.Scan(&a.ID, &a.Username, &a.Avatar); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RecalcReputation recomputes users.reputation from the current data.
func (r *UserRepository) RecalcReputation(ids ...int) error {
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, err := r.DB.Exec(`UPDATE users SET reputation = `+ReputationSQL+` WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

// TopByReputation returns the users with the highest reputation.
func (r *UserRepository) TopByReputation(limit int) ([]models.AuthorStat, error) {
	rows, err := r.DB.Query(`SELECT id, username, avatar, reputation FROM users
		WHERE reputation > 0 ORDER BY reputation DESC, username LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AuthorStat
	for rows.Next() {
		var a models.AuthorStat
		if err := rows.Scan(&a.ID, &a.Username, &a.Avatar, &a.Reputation); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetRole changes a user's role ("user", "moderator" or "admin").
func (r *UserRepository) SetRole(id int, role string) error {
	_, err := r.DB.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, id)
	return err
}

// PromoteAdminByEmail makes the user with this email an admin; it reports
// whether such a user exists.
func (r *UserRepository) PromoteAdminByEmail(email string) (bool, error) {
	res, err := r.DB.Exec(`UPDATE users SET role = 'admin' WHERE email = ? COLLATE NOCASE`, email)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Ban blocks the user until the given time (models.PermanentBan for ever) and
// ends their sessions.
func (r *UserRepository) Ban(id int, until time.Time, reason string) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`UPDATE users SET banned_until = ?, ban_reason = ? WHERE id = ?`,
		until.UTC().Format("2006-01-02 15:04:05"), reason, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *UserRepository) Unban(id int) error {
	_, err := r.DB.Exec(`UPDATE users SET banned_until = NULL, ban_reason = '' WHERE id = ?`, id)
	return err
}

// AdminList finds users for the admin panel by name or email, role and
// ban state ("banned", "active" or "").
func (r *UserRepository) AdminList(q, role, state string, limit int) ([]models.User, error) {
	query := `SELECT id, email, username, avatar, reputation, role, banned_until, ban_reason, created_at,
		(SELECT COUNT(*) FROM posts WHERE user_id = users.id), (SELECT COUNT(*) FROM comments WHERE user_id = users.id AND is_deleted = 0)
		FROM users WHERE 1 = 1`
	var args []any
	if q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(q)) + "%"
		query += ` AND (ulower(username) LIKE ? ESCAPE '\' OR ulower(email) LIKE ? ESCAPE '\')`
		args = append(args, like, like)
	}
	if role != "" {
		query += ` AND role = ?`
		args = append(args, role)
	}
	switch state {
	case "banned":
		query += ` AND banned_until > datetime('now')`
	case "active":
		query += ` AND (banned_until IS NULL OR banned_until <= datetime('now'))`
	}
	query += ` ORDER BY CASE role WHEN 'admin' THEN 0 WHEN 'moderator' THEN 1 ELSE 2 END, reputation DESC, id LIMIT ?`
	args = append(args, limit)
	rows, err := r.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.User
	for rows.Next() {
		var u models.User
		var banned sql.NullTime
		if err := rows.Scan(&u.ID, &u.Email, &u.Username, &u.Avatar, &u.Reputation, &u.Role, &banned, &u.BanReason,
			&u.CreatedAt, &u.PostCount, &u.CommentCount); err != nil {
			return nil, err
		}
		if banned.Valid {
			u.BannedUntil = &banned.Time
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UserFiles lists the uploaded files that belong to a user, so they can be
// removed from disk when the account is deleted.
type UserFiles struct {
	Avatars    []string
	Cover      string
	PostImages []string
}

func (r *UserRepository) Files(id int) (UserFiles, error) {
	var f UserFiles
	if err := r.DB.QueryRow(`SELECT cover_image FROM users WHERE id = ?`, id).Scan(&f.Cover); err != nil {
		return f, err
	}
	for _, q := range []struct {
		sql string
		dst *[]string
	}{
		{`SELECT file_path FROM user_photos WHERE user_id = ?`, &f.Avatars},
		{`SELECT image_path FROM posts WHERE user_id = ? AND image_path != ''`, &f.PostImages},
	} {
		rows, err := r.DB.Query(q.sql, id)
		if err != nil {
			return f, err
		}
		for rows.Next() {
			var s string
			if rows.Scan(&s) == nil {
				*q.dst = append(*q.dst, s)
			}
		}
		rows.Close()
	}
	return f, nil
}

// Delete removes the account; posts, comments, messages and the rest go with
// it through ON DELETE CASCADE.
func (r *UserRepository) Delete(id int) error {
	_, err := r.DB.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

// CreateOAuth creates an account for a social login. The password hash is a
// marker that bcrypt can never match, so password login is impossible.
func (r *UserRepository) CreateOAuth(email, username string) (int, error) {
	res, err := r.DB.Exec(`INSERT INTO users (email, username, password_hash) VALUES (?, ?, '!oauth')`, email, username)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

// IdentityUser returns the user linked to a provider account (0 if none).
func (r *UserRepository) IdentityUser(provider, subject string) (int, error) {
	var id int
	err := r.DB.QueryRow(`SELECT user_id FROM user_identities WHERE provider = ? AND subject = ?`, provider, subject).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func (r *UserRepository) LinkIdentity(provider, subject string, userID int) error {
	_, err := r.DB.Exec(`INSERT OR IGNORE INTO user_identities (provider, subject, user_id) VALUES (?, ?, ?)`, provider, subject, userID)
	return err
}
