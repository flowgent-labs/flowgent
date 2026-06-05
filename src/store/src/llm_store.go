package store

import (
	"context"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/model/src"
)

// ILlmProviderStore manages LLM provider configurations.
type ILlmProviderStore interface {
	SaveProvider(ctx context.Context, p *model.LlmProvider) error
	GetProvider(ctx context.Context, id string) (*model.LlmProvider, error)
	ListProviders(ctx context.Context, tenantID string) ([]model.LlmProvider, error)
	DeleteProvider(ctx context.Context, id string) error
}

// ─── In-memory LLM provider stores ────────────────────────────

var pgLlmProviders = struct {
	mu    sync.Mutex
	store map[string]*model.LlmProvider
}{store: make(map[string]*model.LlmProvider)}

var sqliteLlmProviders = struct {
	mu    sync.Mutex
	store map[string]*model.LlmProvider
}{store: make(map[string]*model.LlmProvider)}

// ─── PostgresStore methods — llm provider ─────────────────────

func (s *PostgresStore) SaveProvider(ctx context.Context, p *model.LlmProvider) error {
	pgLlmProviders.mu.Lock()
	defer pgLlmProviders.mu.Unlock()
	p.UpdatedAt = time.Now()
	pgLlmProviders.store[p.ID] = p
	return nil
}

func (s *PostgresStore) GetProvider(ctx context.Context, id string) (*model.LlmProvider, error) {
	pgLlmProviders.mu.Lock()
	defer pgLlmProviders.mu.Unlock()
	return pgLlmProviders.store[id], nil
}

func (s *PostgresStore) ListProviders(ctx context.Context, tenantID string) ([]model.LlmProvider, error) {
	pgLlmProviders.mu.Lock()
	defer pgLlmProviders.mu.Unlock()
	var out []model.LlmProvider
	for _, p := range pgLlmProviders.store {
		if tenantID == "" || p.TenantID == tenantID {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (s *PostgresStore) DeleteProvider(ctx context.Context, id string) error {
	pgLlmProviders.mu.Lock()
	defer pgLlmProviders.mu.Unlock()
	delete(pgLlmProviders.store, id)
	return nil
}

// ─── SQLiteStore methods — llm provider ───────────────────────

func (s *SQLiteStore) SaveProvider(ctx context.Context, p *model.LlmProvider) error {
	sqliteLlmProviders.mu.Lock()
	defer sqliteLlmProviders.mu.Unlock()
	p.UpdatedAt = time.Now()
	sqliteLlmProviders.store[p.ID] = p
	return nil
}

func (s *SQLiteStore) GetProvider(ctx context.Context, id string) (*model.LlmProvider, error) {
	sqliteLlmProviders.mu.Lock()
	defer sqliteLlmProviders.mu.Unlock()
	return sqliteLlmProviders.store[id], nil
}

func (s *SQLiteStore) ListProviders(ctx context.Context, tenantID string) ([]model.LlmProvider, error) {
	sqliteLlmProviders.mu.Lock()
	defer sqliteLlmProviders.mu.Unlock()
	var out []model.LlmProvider
	for _, p := range sqliteLlmProviders.store {
		if tenantID == "" || p.TenantID == tenantID {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (s *SQLiteStore) DeleteProvider(ctx context.Context, id string) error {
	sqliteLlmProviders.mu.Lock()
	defer sqliteLlmProviders.mu.Unlock()
	delete(sqliteLlmProviders.store, id)
	return nil
}
