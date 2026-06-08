package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IStore is the minimal top-level store interface.
type IStore interface {
	DB() any
	Close() error
}

type StoreManager struct {
	IStore
}

func NewStoreManager(cfg *config.FlowgentConfig) *StoreManager {
	if dsn := os.Getenv("FLOWGENT_DATABASE_URL"); dsn != "" {
		pool := NewPostgresPool(context.Background(), dsn, "public")
		return &StoreManager{IStore: &pgStore{Pool: pool}}
	}

	if cfg.Storage.Type == "POSTGRE" {
		pg := cfg.Storage.Postgres
		ssl := "disable"
		if pg.UseSSL {
			ssl = "require"
		}
		dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			pg.Host, pg.Port, pg.Username, pg.Password, pg.Database, ssl)
		pool := NewPostgresPool(context.Background(), dsn, "public")
		return &StoreManager{IStore: &pgStore{Pool: pool}}
	}

	dir := cfg.Storage.SQLite.Dir
	if dir == "" {
		dir = "~/.flowgent/sqlite"
	}
	conn := NewSQLiteConn(context.Background(), dir)
	return &StoreManager{IStore: &sqStore{Conn: conn}}
}

func StoreDSNFromEnv() string {
	if u := os.Getenv("FLOWGENT_DATABASE_URL"); u != "" {
		return u
	}
	return ""
}

type pgStore struct{ Pool *pgxpool.Pool }

func (s *pgStore) DB() any      { return s.Pool }
func (s *pgStore) Close() error { s.Pool.Close(); return nil }

type sqStore struct{ Conn *sql.DB }

func (s *sqStore) DB() any      { return s.Conn }
func (s *sqStore) Close() error { return s.Conn.Close() }
