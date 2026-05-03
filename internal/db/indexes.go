package db

import "database/sql"

// CreateIndexes создаёт индексы для производительности
func CreateIndexes(db *sql.DB) error {
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_posts_user_id ON posts(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_created_at ON posts(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_comments_post_id ON comments(post_id)`,
		`CREATE INDEX IF NOT EXISTS idx_post_categories_category_id ON post_categories(category_id)`,
		`CREATE INDEX IF NOT EXISTS idx_post_reactions_post_id ON post_reactions(post_id)`,
		`CREATE INDEX IF NOT EXISTS idx_comment_reactions_comment_id ON comment_reactions(comment_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)`,
	}

	for _, q := range indexes {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}
