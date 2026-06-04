package store

import (
	"context"
	"fmt"
	"log"
	"os"
)

// StoreManager is the unified entry point for store implementations.
// It embeds Store so all persistence operations are directly available.
type StoreManager struct {
	Store
}

// StoreManagerConfig mirrors config.StorageConfig, decoupled from config.
type StoreManagerConfig struct {
	Type     string // "POSTGRE" | "SQLITE" | ""
	DSN      string // direct DSN override (e.g. FLOWGENT_DATABASE_URL)
	SQLite   SQLiteConfig
	Postgres PostgresConfig
}

type SQLiteConfig struct {
	Dir string
}

type PostgresConfig struct {
	Host           string
	Port           int
	Database       string
	Schema         string
	Username       string
	Password       string
	MinConnections int
	MaxConnections int
	UseSSL         bool
}

// NewStoreManager creates the correct Store implementation from config.
func NewStoreManager(cfg *StoreManagerConfig) *StoreManager {
	var s Store

	switch {
	case cfg.DSN != "":
		log.Printf("StoreManager: using external PG DSN")
		s = NewPostgresStore(cfg.DSN)

	case cfg.Type == "POSTGRE":
		pg := cfg.Postgres
		sslMode := "disable"
		if pg.UseSSL {
			sslMode = "require"
		}
		dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			pg.Host, pg.Port, pg.Username, pg.Password, pg.Database, sslMode)
		ps := NewPostgresStore(dsn)
		ps.SetPoolConfig(pg.MinConnections, pg.MaxConnections)
		if pg.Schema != "" {
			ps.SetSchema(pg.Schema)
		}
		if err := ps.Init(context.Background()); err != nil {
			log.Fatalf("Failed to init Postgres: %v", err)
		}
		s = ps

	default: // SQLITE or empty
		dir := cfg.SQLite.Dir
		if dir == "" {
			dir = "~/.flowgent/sqlite"
		}
		sq := NewSQLiteStore(dir)
		if err := sq.Init(context.Background()); err != nil {
			log.Fatalf("Failed to init SQLite: %v", err)
		}
		s = sq
	}

	if s == nil {
		log.Fatalf("StoreManager: failed to create store")
	}
	return &StoreManager{Store: s}
}

// StoreDSNFromEnv returns FLOWGENT_DATABASE_URL if set.
func StoreDSNFromEnv() string {
	if u := os.Getenv("FLOWGENT_DATABASE_URL"); u != "" {
		return u
	}
	return ""
}
