package store

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BasePostgresStore holds the PostgreSQL connection pool and helpers.
// Entity store methods are defined on types that embed this base.
type BasePostgresStore struct {
	Pool   *pgxpool.Pool
	DSN    string
	Schema string
}

// InitPool creates the pgxpool connection pool.
func (b *BasePostgresStore) InitPool(ctx context.Context) error {
	cfg, err := pgxpool.ParseConfig(b.DSN)
	log.Printf("[pg] Init: connecting to %s", b.DSN)
	if err != nil {
		return fmt.Errorf("parse pg config: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	schema := b.Schema
	if schema == "" {
		schema = "public"
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, fmt.Sprintf("SET search_path TO %s", schema))
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	b.Pool = pool
	return nil
}

// ClosePool shuts down the connection pool.
func (b *BasePostgresStore) ClosePool() {
	if b.Pool != nil {
		b.Pool.Close()
	}
}
