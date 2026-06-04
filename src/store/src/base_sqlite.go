package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// BaseSQLiteStore holds the SQLite database handle.
// Entity store methods are defined on types that embed this base.
type BaseSQLiteStore struct {
	Conn *sql.DB
	Dir  string
}

// InitDB opens the SQLite database and runs migrations.
func (b *BaseSQLiteStore) InitDB(ctx context.Context) error {
	if err := os.MkdirAll(b.Dir, 0755); err != nil {
		return fmt.Errorf("create sqlite dir: %w", err)
	}
	dbPath := filepath.Join(b.Dir, "flowgent.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	b.Conn = db
	if err := RunMigrations(db, "sqlite"); err != nil {
		return fmt.Errorf("sqlite migrations: %w", err)
	}
	return nil
}

// CloseDB closes the database connection.
func (b *BaseSQLiteStore) CloseDB() {
	if b.Conn != nil {
		b.Conn.Close()
	}
}
