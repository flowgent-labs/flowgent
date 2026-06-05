package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flowgent-labs/flowgent/model/src"
)

// IAgentStore manages Agent definitions.
type IAgentStore interface {
	SaveAgent(ctx context.Context, agent *model.AgentDef) error
	GetAgent(ctx context.Context, name string) (*model.AgentDef, error)
	ListAgents(ctx context.Context, tenantID string) ([]model.AgentDef, error)
	DeleteAgent(ctx context.Context, name string) error
}

// ─── PostgresStore methods — agent ────────────────────────────

func (s *PostgresStore) SaveAgent(ctx context.Context, agent *model.AgentDef) error {
	b, _ := json.Marshal(agent)
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO agents (name, definition) VALUES ($1,$2) ON CONFLICT (name) DO UPDATE SET definition=$2`,
		agent.Name, b)
	return err
}

func (s *PostgresStore) GetAgent(ctx context.Context, name string) (*model.AgentDef, error) {
	row := s.Pool.QueryRow(ctx, `SELECT definition FROM agents WHERE name=$1`, name)
	var b []byte
	if err := row.Scan(&b); err != nil {
		return nil, err
	}
	var a model.AgentDef
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *PostgresStore) ListAgents(ctx context.Context, tenantID string) ([]model.AgentDef, error) {
	rows, err := s.Pool.Query(ctx, `SELECT definition FROM agents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agents []model.AgentDef
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			continue
		}
		var a model.AgentDef
		if err := json.Unmarshal(b, &a); err != nil {
			continue
		}
		agents = append(agents, a)
	}
	return agents, nil
}

func (s *PostgresStore) DeleteAgent(ctx context.Context, name string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM agents WHERE name=$1`, name)
	return err
}

// ─── SQLiteStore methods — agent ──────────────────────────────

func (s *SQLiteStore) SaveAgent(ctx context.Context, agent *model.AgentDef) error {
	return nil
}

func (s *SQLiteStore) GetAgent(ctx context.Context, name string) (*model.AgentDef, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) ListAgents(ctx context.Context, tenantID string) ([]model.AgentDef, error) {
	return nil, nil
}

func (s *SQLiteStore) DeleteAgent(ctx context.Context, name string) error {
	_, err := s.Conn.ExecContext(ctx, `DELETE FROM agents WHERE name=?`, name)
	return err
}
