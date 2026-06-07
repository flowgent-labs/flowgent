package memory

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg"
)

// MemoryPostgresStore wraps store.PostgresGenericStore[model.NodeMemory].
type MemoryPostgresStore struct {
	inner *store.PostgresGenericStore[model.NodeMemory]
}

func NewMemoryPostgresStore(pool *pgxpool.Pool) *MemoryPostgresStore {
	return &MemoryPostgresStore{
		inner: &store.PostgresGenericStore[model.NodeMemory]{
			Pool: pool, Table: "agent_memories", IDCol: "id",
		},
	}
}

func (s *MemoryPostgresStore) Get(ctx context.Context, id string) (*model.NodeMemory, error) {
	return s.inner.Get(ctx, id)
}
func (s *MemoryPostgresStore) Select(ctx context.Context, req model.PageRequest) (*model.Page[model.NodeMemory], error) {
	return s.inner.Select(ctx, req)
}
func (s *MemoryPostgresStore) Save(ctx context.Context, e *model.NodeMemory) error {
	return s.inner.Save(ctx, e)
}
func (s *MemoryPostgresStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

// UpsertMemory creates or updates the memory entry for (flowID, nodeID).
func (s *MemoryPostgresStore) UpsertMemory(ctx context.Context, mem *model.NodeMemory) error {
	now := time.Now().UTC()
	if mem.CreatedAt.IsZero() {
		mem.CreatedAt = now
	}
	mem.UpdatedAt = now

	emb, _ := json.Marshal(mem.Embedding)
	meta, _ := json.Marshal(mem.Metadata)

	_, err := s.inner.Pool.Exec(ctx,
		`INSERT INTO agent_memories (flow_id, node_id, content, embedding, metadata, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (flow_id, node_id) DO UPDATE SET content=$3, embedding=$4, metadata=$5, updated_at=$7`,
		mem.FlowID, mem.NodeID, mem.Content, emb, meta, mem.CreatedAt, mem.UpdatedAt)
	return err
}

// SearchMemory returns the top-K memories within a flow most similar
// to the given embedding. Falls back to ordered by updated_at DESC.
func (s *MemoryPostgresStore) SearchMemory(ctx context.Context, flowID string, embedding []float32, topK int) ([]model.NodeMemory, error) {
	rows, err := s.inner.Pool.Query(ctx,
		`SELECT flow_id, node_id, content, embedding, metadata, created_at, updated_at
		 FROM agent_memories WHERE flow_id=$1 ORDER BY updated_at DESC LIMIT $2`, flowID, topK)
	if err != nil { return nil, err }
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[model.NodeMemory])
}

// ListByFlow returns all memories for a flow definition, ordered by recency.
func (s *MemoryPostgresStore) ListByFlow(ctx context.Context, flowID string) ([]model.NodeMemory, error) {
	return s.SearchMemory(ctx, flowID, nil, 100)
}
