package handlers

import (
	"database/sql"
	"forum/internal/view"
	"html/template"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB() *sql.DB {
	os.Remove(":memory:")
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=on")
	if err != nil {
		panic(err)
	}

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
			updated_at DATETIME,
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
			created_at DATETIME,
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
		`INSERT INTO categories (name) VALUES ('Общее'), ('Учёба'), ('Вопросы');`,
	}

	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			panic(err)
		}
	}

	return db
}

func setupTestTemplate() *template.Template {
	cwd, _ := os.Getwd()
	tmplPath := filepath.Join(filepath.Dir(filepath.Dir(cwd)), "templates", "*.html")
	tmpl, _ := view.ParseGlob(tmplPath)
	if tmpl == nil {
		tmpl, _ = template.New("test").Parse("{{.Error}}")
	}
	return tmpl
}
