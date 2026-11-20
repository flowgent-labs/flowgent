// Package storage owns Flowgent's shared SQL implementation and storage
// lifecycle. Entity repositories import this package for the common stores and
// resource-scoping helpers.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IStorage is the minimal top-level storage interface.
type IStorage interface {
	DB() any
	Close() error
}

type StorageManager struct {
	IStorage
}

func NewStorageManager(cfg *config.FlowgentConfig) *StorageManager {
	if cfg.Storage.Type == "POSTGRE" {
		pg := cfg.Storage.Postgres
		// Prefer explicit DSN (set via FLOWGENT__STORAGE__POSTGRES__DSN or YAML)
		if pg.Dsn != "" {
			pool := NewPostgresPool(context.Background(), pg.Dsn, pg.Schema)
			if err := RunMigrationsPG(pool, "postgres"); err != nil {
				slog.Warn("PG migrations failed (non-fatal)", "err", err)
			}
			return &StorageManager{IStorage: &pgStorage{Pool: pool}}
		}
		ssl := "disable"
		if pg.UseSSL {
			ssl = "require"
		}
		dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			pg.Host, pg.Port, pg.Username, pg.Password, pg.Database, ssl)
		pool := NewPostgresPool(context.Background(), dsn, pg.Schema)
		if err := RunMigrationsPG(pool, "postgres"); err != nil {
			slog.Warn("PG migrations failed (non-fatal)", "err", err)
		}
		return &StorageManager{IStorage: &pgStorage{Pool: pool}}
	}

	dir := cfg.Storage.SQLite.Dir
	if dir == "" {
		dir = "~/.flowgent/sqlite"
	}
	conn := NewSQLiteConn(context.Background(), dir)
	return &StorageManager{IStorage: &sqliteStorage{Conn: conn}}
}

// InitStorage creates the configured PostgreSQL or SQLite storage backend.
func InitStorage(cfg *config.FlowgentConfig) IStorage {
	return NewStorageManager(cfg)
}

type pgStorage struct{ Pool *pgxpool.Pool }

func (s *pgStorage) DB() any      { return s.Pool }
func (s *pgStorage) Close() error { s.Pool.Close(); return nil }

type sqliteStorage struct{ Conn *sql.DB }

func (s *sqliteStorage) DB() any      { return s.Conn }
func (s *sqliteStorage) Close() error { return s.Conn.Close() }
