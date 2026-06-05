package store

import (
	"context"
	"fmt"
	"os"

	"github.com/flowgent-labs/flowgent/config/src/config"
)

type IStore interface {
	IAgentFlowStore
	IFlowRunStore
	ITaskPlanStore
	IAgentStore
	IApprovalStore
	INotifierStore
	ILlmProviderStore
	DB() any
	Close() error
}

type StoreManager struct {
	IStore
}

func NewStoreManager(cfg *config.FlowgentConfig) *StoreManager {
	var s IStore
	dsn := os.Getenv("FLOWGENT_DATABASE_URL")
	if dsn != "" {
		pool := NewPostgresPool(context.Background(), dsn, "public")
		s = newPgStore(pool)
		return &StoreManager{IStore: s}
	}
	if cfg.Storage.Type == "POSTGRE" {
		pg := cfg.Storage.Postgres
		ssl := "disable"
		if pg.UseSSL { ssl = "require" }
		dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			pg.Host, pg.Port, pg.Username, pg.Password, pg.Database, ssl)
		pool := NewPostgresPool(context.Background(), dsn, "public")
		s = newPgStore(pool)
		return &StoreManager{IStore: s}
	}
	dir := cfg.Storage.SQLite.Dir
	if dir == "" { dir = "~/.flowgent/sqlite" }
	conn := NewSQLiteConn(context.Background(), dir)
	s = newSqStore(conn)
	return &StoreManager{IStore: s}
}

func StoreDSNFromEnv() string {
	if u := os.Getenv("FLOWGENT_DATABASE_URL"); u != "" { return u }
	return ""
}
