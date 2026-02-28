package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/flowgent-labs/flowgent/src/model"
)

// SQLiteNodeMemoryStore implements NodeMemoryStore using SQLite.
type SQLiteNodeMemoryStore struct {
	db *sql.DB
}

// NewSQLiteNodeMemoryStore creates a memory store backed by SQLite.
func NewSQLiteNodeMemoryStore(db *sql.DB) (*SQLiteNodeMemoryStore, error) {
	s := &SQLiteNodeMemoryStore{db: db}
	if err := s.migrate(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SQLiteNodeMemoryStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS node_memories (
			flow_id    TEXT NOT NULL,
			node_id    TEXT NOT NULL DEFAULT '',
			content    TEXT NOT NULL DEFAULT '',
			embedding  TEXT,
			metadata   TEXT DEFAULT '{}',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (flow_id, node_id)
		)
	`)
	return err
}

func (s *SQLiteNodeMemoryStore) GetMemory(ctx context.Context, flowID, nodeID string) (*model.NodeMemory, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT flow_id, node_id, content, embedding, metadata, created_at, updated_at
		 FROM node_memories WHERE flow_id=?1 AND node_id=?2`, flowID, nodeID)
	return scanNodeMemory(row)
}

func (s *SQLiteNodeMemoryStore) UpsertMemory(ctx context.Context, mem *model.NodeMemory) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if mem.CreatedAt.IsZero() {
		mem.CreatedAt = time.Now().UTC()
	}
	mem.UpdatedAt = time.Now().UTC()

	emb, _ := json.Marshal(mem.Embedding)
	meta, _ := json.Marshal(mem.Metadata)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO node_memories (flow_id, node_id, content, embedding, metadata, created_at, updated_at)
		 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)
		 ON CONFLICT (flow_id, node_id) DO UPDATE SET content=?3, embedding=?4, metadata=?5, updated_at=?7`,
		mem.FlowID, mem.NodeID, mem.Content, string(emb), string(meta), now, now)
	return err
}

func (s *SQLiteNodeMemoryStore) SearchMemory(ctx context.Context, flowID string, embedding []float32, topK int) ([]model.NodeMemory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT flow_id, node_id, content, embedding, metadata, created_at, updated_at
		 FROM node_memories WHERE flow_id=?1 ORDER BY updated_at DESC LIMIT ?2`, flowID, topK)
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

func (s *SQLiteNodeMemoryStore) ListFlowMemories(ctx context.Context, flowID string) ([]model.NodeMemory, error) {
	return s.SearchMemory(ctx, flowID, nil, 100)
}

func (s *SQLiteNodeMemoryStore) DeleteMemory(ctx context.Context, flowID, nodeID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM node_memories WHERE flow_id=?1 AND node_id=?2`, flowID, nodeID)
	return err
}

func (s *SQLiteNodeMemoryStore) Close() error { return nil }

func scanNodeMemory(s scanner) (*model.NodeMemory, error) {
	var m model.NodeMemory
	var embStr, metaStr string
	if err := s.Scan(&m.FlowID, &m.NodeID, &m.Content, &embStr, &metaStr, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	if embStr != "" && embStr != "null" {
		json.Unmarshal([]byte(embStr), &m.Embedding)
	}
	if metaStr != "" && metaStr != "null" {
		json.Unmarshal([]byte(metaStr), &m.Metadata)
	}
	return &m, nil
}

// Ensure uuid import is used (needed for future memory ID generation)
var _ = uuid.New
var _ = fmt.Sprintf
