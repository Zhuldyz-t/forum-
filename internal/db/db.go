package db

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

// InitDB opens the SQLite database and configures it for concurrent use.
// WAL journal mode allows multiple readers alongside a single writer.
// busy_timeout=5000 makes SQLite retry for up to 5 s before returning "database is locked",
// instead of failing immediately under concurrent write pressure.
func InitDB(path string) (*sql.DB, error) {
	database, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	if err := database.Ping(); err != nil {
		return nil, err
	}

	// SQLite supports only one concurrent writer; cap the pool to prevent "database is locked" errors.
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	return database, nil
}
