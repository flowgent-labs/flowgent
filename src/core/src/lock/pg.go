package lock

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// PostgresLock implements DistributedLock using a PostgreSQL advisory table.
type PostgresLock struct {
	db    *sql.DB
	mu    sync.RWMutex
	locks map[string]string // key → lock value
}

// NewPostgresLock creates a PostgreSQL-backed distributed lock.
func NewPostgresLock(connStr string) (*PostgresLock, error) {
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("open pg: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping pg: %w", err)
	}

	// Ensure the dlocks table exists
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS dlocks (
		lock_key   TEXT PRIMARY KEY,
		lock_value TEXT NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		return nil, fmt.Errorf("create dlocks table: %w", err)
	}

	return &PostgresLock{
		db:    db,
		locks: make(map[string]string),
	}, nil
}

func (p *PostgresLock) TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	value := uuid.New().String()
	expiresAt := time.Now().Add(ttl)

	result, err := p.db.ExecContext(ctx,
		`INSERT INTO dlocks (lock_key, lock_value, expires_at)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (lock_key) DO UPDATE
		 SET lock_value = $2, expires_at = $3
		 WHERE dlocks.expires_at < NOW()`,
		key, value, expiresAt,
	)
	if err != nil {
		return false, fmt.Errorf("acquire lock %s: %w", key, err)
	}

	rows, _ := result.RowsAffected()
	if rows > 0 {
		p.mu.Lock()
		p.locks[key] = value
		p.mu.Unlock()
		return true, nil
	}
	return false, nil
}

func (p *PostgresLock) Release(ctx context.Context, key string) error {
	p.mu.RLock()
	value, ok := p.locks[key]
	p.mu.RUnlock()
	if !ok {
		return nil
	}

	result, err := p.db.ExecContext(ctx,
		`DELETE FROM dlocks WHERE lock_key = $1 AND lock_value = $2`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("release lock %s: %w", key, err)
	}
	if n, _ := result.RowsAffected(); n > 0 {
		p.mu.Lock()
		delete(p.locks, key)
		p.mu.Unlock()
	}
	return nil
}

func (p *PostgresLock) Extend(ctx context.Context, key string, ttl time.Duration) error {
	p.mu.RLock()
	value, ok := p.locks[key]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("lock %s not held", key)
	}

	expiresAt := time.Now().Add(ttl)
	result, err := p.db.ExecContext(ctx,
		`UPDATE dlocks SET expires_at = $1 WHERE lock_key = $2 AND lock_value = $3`,
		expiresAt, key, value,
	)
	if err != nil {
		return fmt.Errorf("extend lock %s: %w", key, err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("lock %s lost", key)
	}
	return nil
}

// CleanupExpired removes expired locks from the table.
func (p *PostgresLock) CleanupExpired(ctx context.Context) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM dlocks WHERE expires_at < NOW()`)
	return err
}
