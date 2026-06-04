package store

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/flowgent-labs/flowgent/config/src/config"
)

// IBaseStore provides DB access.
type IBaseStore interface {
	DB() any
}

// IStore composes all entity store interfaces (JPA SessionFactory pattern).
// Implementations: PostgresStore, SQLiteStore.
type IStore interface {
	IBaseStore
	IAgentFlowStore
	IFlowRunStore
	ITaskPlanStore
	IAgentStore
	IApprovalStore
	INotifierStore
	ILlmProviderStore
}

// StoreManager is the unified entry point for store implementations.
// It embeds IStore so all persistence operations are directly available.
type StoreManager struct {
	IStore
}

// NewStoreManager creates the correct IStore implementation from FlowgentConfig.
func NewStoreManager(cfg *config.FlowgentConfig) *StoreManager {
	var s IStore

	switch {
	case StoreDSNFromEnv() != "":
		log.Printf("StoreManager: using external PG DSN")
		s = NewPostgresStore(StoreDSNFromEnv())

	case cfg.Storage.Type == "POSTGRE":
		pg := cfg.Storage.Postgres
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

	default:
		dir := cfg.Storage.SQLite.Dir
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
	return &StoreManager{IStore: s}
}

// StoreDSNFromEnv returns FLOWGENT_DATABASE_URL if set.
func StoreDSNFromEnv() string {
	if u := os.Getenv("FLOWGENT_DATABASE_URL"); u != "" {
		return u
	}
	return ""
}
