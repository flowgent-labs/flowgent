package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// NewSQLiteConn opens a SQLite connection and runs migrations.
func NewSQLiteConn(ctx context.Context, dir string) *sql.DB {
	if err := os.MkdirAll(dir, 0755); err != nil {
		panic(fmt.Sprintf("create sqlite dir: %v", err))
	}
	dbPath := filepath.Join(dir, "flowgent.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal=WAL&_busy_timeout=5000")
	if err != nil { panic(fmt.Sprintf("open sqlite: %v", err)) }
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := RunMigrations(db, "sqlite"); err != nil {
		panic(fmt.Sprintf("sqlite migrations: %v", err))
	}
	return db
}

// sqStore implements IStore backed by SQLite.
type sqStore struct{ SQLiteStore }

func newSqStore(conn *sql.DB) *sqStore {
	return &sqStore{SQLiteStore: SQLiteStore{Conn: conn}}
}

type SQLiteStore struct {
	Conn *sql.DB
	Dir  string
}

func NewSQLiteStore(dir string) *SQLiteStore {
	return &SQLiteStore{Dir: dir}
}

func (s *SQLiteStore) DB() any { return s.Conn }

func (s *SQLiteStore) Init(ctx context.Context) error {
	if err := os.MkdirAll(s.Dir, 0755); err != nil {
		return fmt.Errorf("create sqlite dir: %w", err)
	}
	dbPath := filepath.Join(s.Dir, "flowgent.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal=WAL&_busy_timeout=5000")
	if err != nil { return fmt.Errorf("open sqlite: %w", err) }
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s.Conn = db
	return RunMigrations(db, "sqlite")
}

func (s *SQLiteStore) Close() error {
	if s.Conn != nil { s.Conn.Close() }
	return nil
}

type scanner interface{ Scan(...any) error }
type rowsScanner interface{ Scan(...any) error }
