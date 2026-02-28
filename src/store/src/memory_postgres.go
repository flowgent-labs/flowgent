package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/flowgent-labs/flowgent/model/src"
)

// PGNodeMemoryStore implements NodeMemoryStore using PostgreSQL.
type PGNodeMemoryStore struct {
	db *sql.DB
}

func NewPGNodeMemoryStore(db *sql.DB) (*PGNodeMemoryStore, error) {
	s := &PGNodeMemoryStore{db: db}
	if err := s.migrate(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *PGNodeMemoryStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS node_memories (
			flow_id    VARCHAR(255) NOT NULL,
			node_id    VARCHAR(255) NOT NULL DEFAULT '',
			content    TEXT NOT NULL DEFAULT '',
			embedding  JSONB,
			metadata   JSONB DEFAULT '{}',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (flow_id, node_id)
		)
	`)
	return err
}

func (s *PGNodeMemoryStore) GetMemory(ctx context.Context, flowID, nodeID string) (*model.NodeMemory, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT flow_id, node_id, content, embedding, metadata, created_at, updated_at
		 FROM node_memories WHERE flow_id=$1 AND node_id=$2`, flowID, nodeID)
	return scanNodeMemory(row)
}

func (s *PGNodeMemoryStore) UpsertMemory(ctx context.Context, mem *model.NodeMemory) error {
	now := time.Now().UTC()
	if mem.CreatedAt.IsZero() {
		mem.CreatedAt = now
	}
	mem.UpdatedAt = now

	emb, _ := json.Marshal(mem.Embedding)
	meta, _ := json.Marshal(mem.Metadata)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO node_memories (flow_id, node_id, content, embedding, metadata, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (flow_id, node_id) DO UPDATE SET content=$3, embedding=$4, metadata=$5, updated_at=$7`,
		mem.FlowID, mem.NodeID, mem.Content, emb, meta, mem.CreatedAt, mem.UpdatedAt)
	return err
}

func (s *PGNodeMemoryStore) SearchMemory(ctx context.Context, flowID string, embedding []float32, topK int) ([]model.NodeMemory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT flow_id, node_id, content, embedding, metadata, created_at, updated_at
		 FROM node_memories WHERE flow_id=$1 ORDER BY updated_at DESC LIMIT $2`, flowID, topK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.NodeMemory
	for rows.Next() {
		m, err := scanNodeMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (s *PGNodeMemoryStore) ListFlowMemories(ctx context.Context, flowID string) ([]model.NodeMemory, error) {
	return s.SearchMemory(ctx, flowID, nil, 100)
}

func (s *PGNodeMemoryStore) DeleteMemory(ctx context.Context, flowID, nodeID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM node_memories WHERE flow_id=$1 AND node_id=$2`, flowID, nodeID)
	return err
}

func (s *PGNodeMemoryStore) Close() error { return nil }
