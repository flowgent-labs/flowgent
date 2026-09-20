package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
)

// MemorySQLiteStore wraps storage.SQLiteGenericStore[entities.MemoryInfo].
type MemorySQLiteStore struct {
	inner *storage.SQLiteGenericStore[entities.MemoryInfo]
}

func NewMemorySQLiteStore(conn *sql.DB) *MemorySQLiteStore {
	return &MemorySQLiteStore{
		inner: &storage.SQLiteGenericStore[entities.MemoryInfo]{
			Conn: conn, Table: "llm_memory", IDCol: "id",
		},
	}
}

func (s *MemorySQLiteStore) Get(ctx context.Context, id string) (*entities.MemoryInfo, error) {
	return s.inner.Get(ctx, id)
}
func (s *MemorySQLiteStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.MemoryInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *MemorySQLiteStore) Save(ctx context.Context, e *entities.MemoryInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *MemorySQLiteStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

// UpsertMemory creates or updates the memory entry for (flowID, nodeID).
func (s *MemorySQLiteStore) UpsertMemory(ctx context.Context, mem *entities.MemoryInfo) error {
	now := time.Now().UTC()
	if mem.CreatedAt.IsZero() {
		mem.CreatedAt = now
	}
	mem.UpdatedAt = now

	emb, _ := json.Marshal(mem.Embedding)
	meta, _ := json.Marshal(mem.Metadata)
	scope := s.inner.SqlScope(ctx)
	if scope.Where == "0=1" {
		return storage.ErrFlowgentSqlScopeDenied
	}
	tx, err := s.inner.Conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO llm_memory (id, flow_id, node_id, content, embedding, metadata, created_at, updated_at)
		 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)
		 ON CONFLICT (flow_id, node_id) DO UPDATE SET content=?4, embedding=?5, metadata=?6, updated_at=?8`,
		uuid.New().String(), mem.FlowID, mem.NodeID, mem.Content, string(emb), string(meta), now, now)
	if err != nil {
		return err
	}
	if scope.Where != "1=1" {
		scopeWhere, scopeArgs := scope.SQLiteWhere()
		args := append([]any{mem.FlowID, mem.NodeID}, scopeArgs...)
		var visible int
		if err := tx.QueryRowContext(ctx,
			"SELECT COUNT(1) FROM llm_memory WHERE flow_id=? AND node_id=? AND ("+scopeWhere+")", args...).Scan(&visible); err != nil {
			return err
		}
		if visible != 1 {
			return storage.ErrFlowgentSqlScopeDenied
		}
	}
	return tx.Commit()
}

// SearchMemory returns the top-K memories within a flow most similar
// to the given embedding. Falls back to ordered by updated_at DESC.
func (s *MemorySQLiteStore) SearchMemory(ctx context.Context, flowID string, embedding []float32, topK int) ([]entities.MemoryInfo, error) {
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).SQLiteWhere()
	args := append([]any{flowID}, scopeArgs...)
	args = append(args, topK)
	rows, err := s.inner.Conn.QueryContext(ctx,
		`SELECT flow_id, node_id, content, embedding, metadata, created_at, updated_at
		 FROM llm_memory WHERE flow_id=?1 AND (`+scopeWhere+`) ORDER BY updated_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entities.MemoryInfo
	for rows.Next() {
		m, err := scanNodeMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListByFlow returns all memories for a flow definition, ordered by recency.
func (s *MemorySQLiteStore) ListByFlow(ctx context.Context, flowID string) ([]entities.MemoryInfo, error) {
	return s.SearchMemory(ctx, flowID, nil, 100)
}

type scanner interface{ Scan(dest ...any) error }

func scanNodeMemory(s scanner) (entities.MemoryInfo, error) {
	var m entities.MemoryInfo
	var embStr, metaStr string
	err := s.Scan(&m.FlowID, &m.NodeID, &m.Content, &embStr, &metaStr, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return m, err
	}
	if embStr != "" && embStr != "null" {
		json.Unmarshal([]byte(embStr), &m.Embedding)
	}
	if metaStr != "" && metaStr != "null" {
		json.Unmarshal([]byte(metaStr), &m.Metadata)
	}
	return m, nil
}
