package db

import (
	"database/sql"
	"strings"

	"github.com/mattn/go-sqlite3"
)

// DriverName is SQLite with extra SQL functions registered on every connection.
const DriverName = "sqlite3_forum"

func init() {
	sql.Register(DriverName, &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			// SQLite's lower()/LIKE only fold ASCII; ulower folds Cyrillic
			// too, for case-insensitive search.
			return conn.RegisterFunc("ulower", strings.ToLower, true)
		},
	})
}

// InitDB opens the SQLite database and configures it for concurrent use.
// WAL journal mode allows multiple readers alongside a single writer.
// busy_timeout=5000 makes SQLite retry for up to 5 s before returning "database is locked",
// instead of failing immediately under concurrent write pressure.
func InitDB(path string) (*sql.DB, error) {
	database, err := sql.Open(DriverName, path+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
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
