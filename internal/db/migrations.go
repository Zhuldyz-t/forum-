package db

import (
	"database/sql"
	"forum/internal/models"
	"forum/internal/repository"
)

func RunMigrations(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL UNIQUE,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL UNIQUE,
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS posts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS categories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE
		);`,
		`CREATE TABLE IF NOT EXISTS post_categories (
			post_id INTEGER NOT NULL,
			category_id INTEGER NOT NULL,
			PRIMARY KEY (post_id, category_id),
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE,
			FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS comments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			post_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS post_reactions (
			user_id INTEGER NOT NULL,
			post_id INTEGER NOT NULL,
			value INTEGER NOT NULL CHECK (value IN (1, -1)),
			PRIMARY KEY (user_id, post_id),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS comment_reactions (
			user_id INTEGER NOT NULL,
			comment_id INTEGER NOT NULL,
			value INTEGER NOT NULL CHECK (value IN (1, -1)),
			PRIMARY KEY (user_id, comment_id),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (comment_id) REFERENCES comments(id) ON DELETE CASCADE
		);`,
		`INSERT OR IGNORE INTO categories (name) VALUES
			('Общее'),
			('Учёба'),
			('Вопросы'),
			('Новости'),
			('Технологии'),
			('Хобби'),
			('Другое');`,
	}

	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}

	// Idempotent schema evolution: ignore error if column already exists.
	_, _ = db.Exec(`ALTER TABLE posts ADD COLUMN updated_at DATETIME`)
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN avatar TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN bio TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN cover_image TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN cover_preset TEXT NOT NULL DEFAULT 'blue-violet'`)
	// When a post was liked, for "recent activity". SQLite can't add a column
	// with a CURRENT_TIMESTAMP default, so it's set on write; older likes stay NULL.
	_, _ = db.Exec(`ALTER TABLE post_reactions ADD COLUMN created_at DATETIME`)
	_, _ = db.Exec(`ALTER TABLE posts ADD COLUMN image_path TEXT NOT NULL DEFAULT ''`)

	followQueries := []string{
		`CREATE TABLE IF NOT EXISTS follows (
			follower_id INTEGER NOT NULL,
			following_id INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (follower_id, following_id),
			CHECK (follower_id != following_id),
			FOREIGN KEY (follower_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (following_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_follows_follower ON follows(follower_id);`,
		`CREATE INDEX IF NOT EXISTS idx_follows_following ON follows(following_id);`,
		`CREATE TABLE IF NOT EXISTS notifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			type TEXT NOT NULL,
			actor_id INTEGER NOT NULL,
			post_id INTEGER NULL,
			is_read INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (actor_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id, is_read, created_at);`,
	}
	for _, q := range followQueries {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}

	// Comment threads, editing and soft deletion.
	_, _ = db.Exec(`ALTER TABLE comments ADD COLUMN parent_id INTEGER REFERENCES comments(id) ON DELETE CASCADE`)
	_, _ = db.Exec(`ALTER TABLE comments ADD COLUMN updated_at DATETIME`)
	_, _ = db.Exec(`ALTER TABLE comments ADD COLUMN is_deleted INTEGER NOT NULL DEFAULT 0`)
	// Lets reply/mention notifications link to the exact comment.
	_, _ = db.Exec(`ALTER TABLE notifications ADD COLUMN comment_id INTEGER REFERENCES comments(id) ON DELETE CASCADE`)

	stage1 := []string{
		`CREATE INDEX IF NOT EXISTS idx_comments_post_parent ON comments(post_id, parent_id);`,
		`CREATE TABLE IF NOT EXISTS bookmarks (
			user_id INTEGER NOT NULL,
			post_id INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, post_id),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_bookmarks_post ON bookmarks(post_id);`,
	}
	for _, q := range stage1 {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}

	// Stage 2: private messages, comment reactions, polls.
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN last_seen_at DATETIME`)
	stage2 := []string{
		// One conversation per pair of users; user1_id < user2_id keeps the pair unique.
		`CREATE TABLE IF NOT EXISTS conversations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user1_id INTEGER NOT NULL,
			user2_id INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_message_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (user1_id, user2_id),
			CHECK (user1_id < user2_id),
			FOREIGN KEY (user1_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (user2_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_conversations_user2 ON conversations(user2_id);`,
		`CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id INTEGER NOT NULL,
			sender_id INTEGER NOT NULL,
			text TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_read INTEGER NOT NULL DEFAULT 0,
			is_delivered INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
			FOREIGN KEY (sender_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages(conversation_id, id);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_unread ON messages(conversation_id, is_read);`,

		// Several different reactions per user per comment, each once.
		`CREATE TABLE IF NOT EXISTS comment_emoji_reactions (
			comment_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			kind TEXT NOT NULL CHECK (kind IN ('like', 'heart', 'laugh', 'wow', 'sad', 'fire')),
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (comment_id, user_id, kind),
			FOREIGN KEY (comment_id) REFERENCES comments(id) ON DELETE CASCADE,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		// Old comment likes become «нравится» (dislikes have no counterpart).
		`INSERT OR IGNORE INTO comment_emoji_reactions (comment_id, user_id, kind)
			SELECT comment_id, user_id, 'like' FROM comment_reactions WHERE value = 1;`,

		`CREATE TABLE IF NOT EXISTS polls (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			post_id INTEGER NOT NULL UNIQUE,
			question TEXT NOT NULL,
			multiple INTEGER NOT NULL DEFAULT 0,
			ends_at DATETIME NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS poll_options (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			poll_id INTEGER NOT NULL,
			text TEXT NOT NULL,
			position INTEGER NOT NULL,
			FOREIGN KEY (poll_id) REFERENCES polls(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_poll_options_poll ON poll_options(poll_id);`,
		`CREATE TABLE IF NOT EXISTS poll_votes (
			poll_id INTEGER NOT NULL,
			option_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (option_id, user_id),
			FOREIGN KEY (poll_id) REFERENCES polls(id) ON DELETE CASCADE,
			FOREIGN KEY (option_id) REFERENCES poll_options(id) ON DELETE CASCADE,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_poll_votes_user ON poll_votes(poll_id, user_id);`,
	}
	for _, q := range stage2 {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}

	// Stage 3: reputation, achievements, best answer.
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN reputation INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE posts ADD COLUMN best_comment_id INTEGER REFERENCES comments(id) ON DELETE SET NULL`)
	// Achievement notifications: which one, and whether its toast was shown.
	_, _ = db.Exec(`ALTER TABLE notifications ADD COLUMN achievement TEXT`)
	_, _ = db.Exec(`ALTER TABLE notifications ADD COLUMN toasted INTEGER NOT NULL DEFAULT 0`)
	stage3 := []string{
		`CREATE TABLE IF NOT EXISTS achievements (
			code TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			description TEXT NOT NULL,
			position INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS user_achievements (
			user_id INTEGER NOT NULL,
			achievement_code TEXT NOT NULL,
			earned_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, achievement_code),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (achievement_code) REFERENCES achievements(code) ON DELETE CASCADE
		);`,
	}
	for _, q := range stage3 {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	// The catalogue lives in Go (models.Achievements); keep the table in sync.
	for i, a := range models.Achievements {
		if _, err := db.Exec(`INSERT INTO achievements (code, title, description, position) VALUES (?, ?, ?, ?)
			ON CONFLICT(code) DO UPDATE SET title = excluded.title, description = excluded.description, position = excluded.position`,
			a.Code, a.Title, a.Description, i); err != nil {
			return err
		}
	}
	// Reputation is derived data: recompute everyone's on start so existing
	// users get correct scores and any drift is repaired.
	if _, err := db.Exec(`UPDATE users SET reputation = ` + repository.ReputationSQL); err != nil {
		return err
	}
	// Stage 4: roles, bans, moderation, OAuth.
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user'`)
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN banned_until DATETIME`)
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN ban_reason TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE posts ADD COLUMN is_hidden INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE comments ADD COLUMN is_hidden INTEGER NOT NULL DEFAULT 0`)
	stage4 := []string{
		`CREATE TABLE IF NOT EXISTS reports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			reporter_id INTEGER NOT NULL,
			target_type TEXT NOT NULL CHECK (target_type IN ('post', 'comment')),
			target_id INTEGER NOT NULL,
			reason TEXT NOT NULL CHECK (reason IN ('spam', 'abuse', 'offtopic', 'other')),
			details TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'dismissed')),
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			resolved_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
			resolved_at DATETIME,
			UNIQUE (reporter_id, target_type, target_id),
			FOREIGN KEY (reporter_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_reports_status ON reports(status, created_at);`,
		// Moderator applications and moderators' requests to delete a user.
		`CREATE TABLE IF NOT EXISTS requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL CHECK (type IN ('moderator', 'delete_user')),
			user_id INTEGER NOT NULL,
			target_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
			text TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			decided_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
			decided_at DATETIME,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_requests_status ON requests(status, created_at);`,
		// Audit log of moderator and admin actions. actor_id is kept as a plain
		// number (no FK) so entries survive the actor's account deletion.
		`CREATE TABLE IF NOT EXISTS mod_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			actor_id INTEGER NOT NULL,
			actor_name TEXT NOT NULL,
			action TEXT NOT NULL,
			target_type TEXT NOT NULL,
			target_id INTEGER NOT NULL,
			details TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_mod_log_created ON mod_log(created_at);`,
		// OAuth accounts linked to users.
		`CREATE TABLE IF NOT EXISTS user_identities (
			provider TEXT NOT NULL,
			subject TEXT NOT NULL,
			user_id INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (provider, subject),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
	}
	for _, q := range stage4 {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}

	// Award achievements earned before they existed, quietly.
	if err := (&repository.AchievementRepository{DB: db}).Backfill(); err != nil {
		return err
	}

	// Profile photo gallery. users.avatar stays as a cached copy of the current
	// photo so post/comment queries don't need an extra join.
	photoQueries := []string{
		`CREATE TABLE IF NOT EXISTS user_photos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			file_path TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_current INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_user_photos_user ON user_photos(user_id, created_at);`,
		// Move avatars uploaded before the gallery existed into it (runs once per user).
		`INSERT INTO user_photos (user_id, file_path, is_current)
			SELECT u.id, u.avatar, 1 FROM users u
			WHERE u.avatar != ''
			  AND NOT EXISTS (SELECT 1 FROM user_photos p WHERE p.user_id = u.id);`,
	}
	for _, q := range photoQueries {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}

	return nil
}
