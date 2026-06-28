package lock

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DistributedLock provides a generic distributed locking interface backed by
// memory (local), PostgreSQL, or Redis.
type DistributedLock interface {
	TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error)
	Release(ctx context.Context, key string) error
	Extend(ctx context.Context, key string, ttl time.Duration) error
}

// ─── Memory implementation ───────────────────────────────────

type MemoryLock struct {
	mu         sync.Mutex
	locks      map[string]lockEntry
	lockValues map[string]string
}

type lockEntry struct {
	value     string
	expiresAt time.Time
}

func NewMemoryLock() *MemoryLock {
	return &MemoryLock{
		locks:      make(map[string]lockEntry),
		lockValues: make(map[string]string),
	}
}

func (m *MemoryLock) TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	value := uuid.New().String()
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, ok := m.locks[key]; ok && time.Now().Before(entry.expiresAt) {
		return false, nil
	}
	m.locks[key] = lockEntry{value: value, expiresAt: time.Now().Add(ttl)}
	m.lockValues[key] = value
	return true, nil
}

func (m *MemoryLock) Release(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	val, ok := m.lockValues[key]
	if !ok {
		return nil
	}
	if entry, ok := m.locks[key]; ok && entry.value == val {
		delete(m.locks, key)
	}
	delete(m.lockValues, key)
	return nil
}

func (m *MemoryLock) Extend(ctx context.Context, key string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	val, ok := m.lockValues[key]
	if !ok {
		return fmt.Errorf("lock %s not held", key)
	}
	if entry, ok := m.locks[key]; ok && entry.value == val {
		entry.expiresAt = time.Now().Add(ttl)
		m.locks[key] = entry
		return nil
	}
	return fmt.Errorf("lock %s lost", key)
}

// ─── Factory ─────────────────────────────────────────────────

// Config selects the distributed lock backend.
type Config struct {
	Type    string // "memory", "postgres", "redis"
	PGConn  string // PostgreSQL connection string (for "postgres")
	RedisCA any    // Redis client (for "redis")
}

// New creates a DistributedLock based on the provided config.
func New(cfg Config) (DistributedLock, error) {
	switch cfg.Type {
	case "memory", "":
		slog.Debug("using in-memory distributed lock")
		return NewMemoryLock(), nil
	case "postgres":
		slog.Debug("using PostgreSQL distributed lock")
		return NewPostgresLock(cfg.PGConn)
	case "redis":
		slog.Debug("using Redis distributed lock")
		if cfg.RedisCA == nil {
			return nil, fmt.Errorf("redis client required for redis lock")
		}
		return NewRedisLock(cfg.RedisCA)
	default:
		return nil, fmt.Errorf("unsupported lock type: %s", cfg.Type)
	}
}
