package handler

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// OpenDB opens a SQLite database at the given path, sets recommended pragmas
// (WAL mode, busy_timeout, foreign_keys), and returns the *sql.DB handle.
func OpenDB(path string) (*sql.DB, error) {
	// Pragmas go in the DSN: the driver runs them on every pooled connection.
	// db.Exec would set them on one connection only, leaving the rest with
	// busy_timeout=0 (instant SQLITE_BUSY) and foreign_keys off (no cascades).
	// _txlock=immediate takes the write lock at BEGIN, so busy_timeout covers
	// transactions that read then write.
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db %s: %w", path, err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return db, nil
}
