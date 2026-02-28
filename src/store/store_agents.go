package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/src/model"
)

// ─── Agent CRUD ──────────────────────────────────────────────

func (s *PostgresStore) SaveAgent(ctx context.Context, agent *model.AgentDef) error {
	agent.UpdatedAt = time.Now()
	if agent.CreatedAt.IsZero() {
		agent.CreatedAt = agent.UpdatedAt
	}
	if agent.TenantID == "" {
		agent.TenantID = "default"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agents (name, model, soul, instruction, output_schema, temperature, max_tokens, tenant_id, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (name) DO UPDATE SET model=$2, soul=$3, instruction=$4, output_schema=$5,
		 temperature=$6, max_tokens=$7, tenant_id=$8, updated_at=$10`,
		agent.Name, agent.Model, agent.Soul, agent.Instruction, toJSON(agent.OutputSchema),
		agent.Temperature, agent.MaxTokens, agent.TenantID, agent.CreatedAt, agent.UpdatedAt)
	return err
}

func (s *PostgresStore) GetAgent(ctx context.Context, name string) (*model.AgentDef, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT name, model, soul, instruction, output_schema, temperature, max_tokens, tenant_id, created_at, updated_at
		 FROM agents WHERE name=$1`, name)
	return scanAgent(row)
}

func (s *PostgresStore) ListAgents(ctx context.Context, tenantID string) ([]model.AgentDef, error) {
	var rows *sql.Rows
	var err error
	if tenantID == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT name, model, soul, instruction, output_schema, temperature, max_tokens, tenant_id, created_at, updated_at
			 FROM agents ORDER BY name`)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT name, model, soul, instruction, output_schema, temperature, max_tokens, tenant_id, created_at, updated_at
			 FROM agents WHERE tenant_id=$1 ORDER BY name`, tenantID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agents []model.AgentDef
	for rows.Next() {
		a, err := scanAgentRow(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, *a)
	}
	return agents, rows.Err()
}

func (s *PostgresStore) DeleteAgent(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agents WHERE name=$1`, name)
	return err
}

// ─── AgentFlow Definition Dynamic CRUD ──────────────────────

func (s *PostgresStore) DeleteAgentFlowDefinition(ctx context.Context, agentFlowID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM agentflow_definitions WHERE agentflow_id=$1`, agentFlowID)
	return err
}

func (s *PostgresStore) UpdateAgentFlowSpec(ctx context.Context, spec *model.AgentFlowSpec, createdBy, comment string) error {
	defJSON, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal agentflow spec: %w", err)
	}
	// Two-step: get next version first (avoids PG parameter ambiguity with $1 in subquery)
	var nextVer int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version),0)+1 FROM agentflow_definitions WHERE agentflow_id=$1`, spec.ID).Scan(&nextVer); err != nil {
		nextVer = 1
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO agentflow_definitions (agentflow_id, version, definition, created_by, comment, priority, tenant_id, namespace, mode, labels)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (agentflow_id, version) DO NOTHING`,
		spec.ID, nextVer, defJSON, createdBy, comment,
		spec.Priority, spec.TenantID, spec.Namespace, string(spec.EffectiveMode()), toJSON(spec.Labels))
	return err
}

func (s *PostgresStore) GetAgentFlowSpec(ctx context.Context, agentFlowID string) (*model.AgentFlowSpec, error) {
	ver, err := s.GetLatestAgentFlowDefinition(ctx, agentFlowID)
	if err != nil {
		return nil, err
	}
	var spec model.AgentFlowSpec
	if err := json.Unmarshal(ver.Definition, &spec); err != nil {
		return nil, fmt.Errorf("unmarshal agentflow spec: %w", err)
	}
	return &spec, nil
}

// ─── AgentFlow Run Management ───────────────────────────────

func (s *PostgresStore) DeleteAgentFlowRun(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agentflow_runs WHERE id=$1`, id)
	return err
}

func (s *PostgresStore) CancelAgentFlowRun(ctx context.Context, id string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE agentflow_runs SET status=$1, updated_at=$2, finished_at=$3 WHERE id=$4`,
		model.RunCancelled, now, now, id)
	return err
}

// ─── Scanner helpers ────────────────────────────────────────

func scanAgent(s scanner) (*model.AgentDef, error) {
	var a model.AgentDef
	var schemaB []byte
	var temp sql.NullFloat64
	if err := s.Scan(&a.Name, &a.Model, &a.Soul, &a.Instruction, &schemaB, &temp, &a.MaxTokens, &a.TenantID, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	if schemaB != nil {
		json.Unmarshal(schemaB, &a.OutputSchema)
	}
	if temp.Valid {
		a.Temperature = &temp.Float64
	}
	return &a, nil
}

func scanAgentRow(r rowsScanner) (*model.AgentDef, error) {
	return scanAgent(r)
}

// ─── SQLite implementations ─────────────────────────────────

func (s *SQLiteStore) SaveAgent(ctx context.Context, agent *model.AgentDef) error {
	agent.UpdatedAt = time.Now()
	if agent.CreatedAt.IsZero() {
		agent.CreatedAt = agent.UpdatedAt
	}
	if agent.TenantID == "" {
		agent.TenantID = "default"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agents (name, model, soul, instruction, output_schema, temperature, max_tokens, tenant_id, created_at, updated_at)
		 VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10)
		 ON CONFLICT (name) DO UPDATE SET model=?2, soul=?3, instruction=?4, output_schema=?5,
		 temperature=?6, max_tokens=?7, tenant_id=?8, updated_at=?10`,
		agent.Name, agent.Model, agent.Soul, agent.Instruction, toJSON(agent.OutputSchema),
		agent.Temperature, agent.MaxTokens, agent.TenantID, agent.CreatedAt, agent.UpdatedAt)
	return err
}

func (s *SQLiteStore) GetAgent(ctx context.Context, name string) (*model.AgentDef, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT name, model, soul, instruction, output_schema, temperature, max_tokens, tenant_id, created_at, updated_at
		 FROM agents WHERE name=?1`, name)
	return scanAgent(row)
}

func (s *SQLiteStore) ListAgents(ctx context.Context, tenantID string) ([]model.AgentDef, error) {
	var rows *sql.Rows
	var err error
	if tenantID == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT name, model, soul, instruction, output_schema, temperature, max_tokens, tenant_id, created_at, updated_at
			 FROM agents ORDER BY name`)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT name, model, soul, instruction, output_schema, temperature, max_tokens, tenant_id, created_at, updated_at
			 FROM agents WHERE tenant_id=?1 ORDER BY name`, tenantID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agents []model.AgentDef
	for rows.Next() {
		a, err := scanAgentRow(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, *a)
	}
	return agents, rows.Err()
}

func (s *SQLiteStore) DeleteAgent(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agents WHERE name=?1`, name)
	return err
}

func (s *SQLiteStore) DeleteAgentFlowDefinition(ctx context.Context, agentFlowID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM agentflow_definitions WHERE agentflow_id=?1`, agentFlowID)
	return err
}

func (s *SQLiteStore) UpdateAgentFlowSpec(ctx context.Context, spec *model.AgentFlowSpec, createdBy, comment string) error {
	defJSON, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal agentflow spec: %w", err)
	}
	var nextVer int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version),0)+1 FROM agentflow_definitions WHERE agentflow_id=?1`, spec.ID).Scan(&nextVer); err != nil {
		nextVer = 1
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO agentflow_definitions (agentflow_id, version, definition, created_by, comment, priority, tenant_id, namespace, mode, labels)
		 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)`,
		spec.ID, nextVer, defJSON, createdBy, comment,
		spec.Priority, spec.TenantID, spec.Namespace, string(spec.EffectiveMode()), toJSON(spec.Labels))
	return err
}

func (s *SQLiteStore) GetAgentFlowSpec(ctx context.Context, agentFlowID string) (*model.AgentFlowSpec, error) {
	ver, err := s.GetLatestAgentFlowDefinition(ctx, agentFlowID)
	if err != nil {
		return nil, err
	}
	var spec model.AgentFlowSpec
	if err := json.Unmarshal(ver.Definition, &spec); err != nil {
		return nil, fmt.Errorf("unmarshal agentflow spec: %w", err)
	}
	return &spec, nil
}

func (s *SQLiteStore) DeleteAgentFlowRun(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agentflow_runs WHERE id=?1`, id)
	return err
}

func (s *SQLiteStore) CancelAgentFlowRun(ctx context.Context, id string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE agentflow_runs SET status=?1, updated_at=?2, finished_at=?3 WHERE id=?4`,
		model.RunCancelled, now, now, id)
	return err
}
