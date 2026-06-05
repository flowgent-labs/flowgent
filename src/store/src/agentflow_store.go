package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/flowgent-labs/flowgent/model/src"
)

// IAgentFlowStore manages AgentFlow definitions.
type IAgentFlowStore interface {
	SaveAgentFlow(ctx context.Context, def *model.AgentFlowVersion) error
	GetAgentFlow(ctx context.Context, agentFlowID string) (*model.AgentFlowVersion, error)
	GetAgentFlowVersion(ctx context.Context, agentFlowID string, version int64) (*model.AgentFlowVersion, error)
	ListAgentFlows(ctx context.Context) ([]model.AgentFlowVersion, error)
	DeleteAgentFlow(ctx context.Context, id string) error
	SaveAgentFlowSpec(ctx context.Context, spec *model.AgentFlowSpec, createdBy, comment string) error
	GetAgentFlowSpec(ctx context.Context, agentFlowID string) (*model.AgentFlowSpec, error)
}

// ─── PostgresStore methods — agentflow ────────────────────────

func (s *PostgresStore) SaveAgentFlow(ctx context.Context, def *model.AgentFlowVersion) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO agentflow_definitions (agentflow_id, version, definition, created_by, comment)
		 VALUES ($1,$2,$3,$4,$5) ON CONFLICT (agentflow_id, version) DO NOTHING`,
		def.AgentFlowID, def.Version, def.Definition, def.CreatedBy, def.Comment)
	return err
}

func (s *PostgresStore) GetAgentFlow(ctx context.Context, agentFlowID string) (*model.AgentFlowVersion, error) {
	row := s.Pool.QueryRow(ctx,
		`SELECT agentflow_id, version, definition, COALESCE(created_by, '') as created_by, COALESCE(comment, '') as comment, created_at
		 FROM public.agentflow_definitions WHERE agentflow_id=$1 ORDER BY version DESC LIMIT 1`, agentFlowID)
	return scanAgentFlowVersion(row)
}

func (s *PostgresStore) GetAgentFlowVersion(ctx context.Context, agentFlowID string, version int64) (*model.AgentFlowVersion, error) {
	row := s.Pool.QueryRow(ctx,
		`SELECT agentflow_id, version, definition, COALESCE(created_by, '') as created_by, COALESCE(comment, '') as comment, created_at
		 FROM public.agentflow_definitions WHERE agentflow_id=$1 AND version=$2`, agentFlowID, version)
	return scanAgentFlowVersion(row)
}

func (s *PostgresStore) ListAgentFlows(ctx context.Context) ([]model.AgentFlowVersion, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT agentflow_id, version, definition, COALESCE(created_by, '') as created_by, COALESCE(comment, '') as comment, created_at
		 FROM public.agentflow_definitions ORDER BY created_at DESC`)
	log.Printf("[pg] ListAgentFlows ERROR: %v", err)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	log.Printf("[pg] ListAgentFlows: starting query")
	var defs []model.AgentFlowVersion
	for rows.Next() {
		d, err := scanAgentFlowVersionRow(rows)
		if err != nil {
			log.Printf("[pg] scanAgentFlowVersionRow error: %v", err)
			continue
		}
		defs = append(defs, *d)
	}
	// Verify: log which PG server we are connected to
	var pgHost string
	if err := s.Pool.QueryRow(ctx, "SELECT 1 AS connectivity_test").Scan(&pgHost); err == nil {
		log.Printf("[pg] PG connectivity: OK (SELECT 1 = %s)", pgHost)
	}
	if err := s.Pool.QueryRow(ctx, "SELECT 1").Scan(&pgHost); err != nil {
		log.Printf("[pg] WARN: SELECT 1 failed: %v", err)
	} else {
		log.Printf("[pg] PG pool working, SELECT 1 = %s", pgHost)
	}
	// Debug: what tables can we see?
	rows2, _ := s.Pool.Query(ctx, "SELECT schemaname, tablename FROM pg_tables WHERE tablename LIKE '%agentflow%'")
	if rows2 != nil {
		defer rows2.Close()
		for rows2.Next() {
			var sn, tn string
			if err := rows2.Scan(&sn, &tn); err == nil {
				log.Printf("[pg] visible table: %s.%s", sn, tn)
			}
		}
	}
	var dbName string
	if err := s.Pool.QueryRow(ctx, "SELECT current_database()").Scan(&dbName); err == nil {
		log.Printf("[pg] current database: %s", dbName)
	}
	var searchPath string
	if err := s.Pool.QueryRow(ctx, "SHOW search_path").Scan(&searchPath); err == nil {
		log.Printf("[pg] search_path: %s", searchPath)
	}
	log.Printf("[pg] ListAgentFlows: returning %d rows", len(defs))
	return defs, nil
}

func (s *PostgresStore) DeleteAgentFlow(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM agentflow_definitions WHERE agentflow_id=$1`, id)
	return err
}

func (s *PostgresStore) SaveAgentFlowSpec(ctx context.Context, spec *model.AgentFlowSpec, createdBy, comment string) error {
	defJSON, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	var nextVer int64
	_ = s.Pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(version),0)+1 FROM agentflow_definitions WHERE agentflow_id=$1`, spec.ID).Scan(&nextVer)
	if nextVer == 0 {
		nextVer = 1
	}
	_, err = s.Pool.Exec(ctx,
		`INSERT INTO agentflow_definitions (agentflow_id,version,definition,created_by,comment,priority,tenant_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (agentflow_id,version) DO NOTHING`,
		spec.ID, nextVer, defJSON, createdBy, comment, string(spec.Priority), spec.TenantID)
	return err
}

func (s *PostgresStore) GetAgentFlowSpec(ctx context.Context, agentFlowID string) (*model.AgentFlowSpec, error) {
	ver, err := s.GetAgentFlow(ctx, agentFlowID)
	if err != nil {
		return nil, err
	}
	var spec model.AgentFlowSpec
	if err := json.Unmarshal(ver.Definition, &spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

// ─── SQLiteStore methods — agentflow ──────────────────────────

func (s *SQLiteStore) SaveAgentFlow(ctx context.Context, def *model.AgentFlowVersion) error {
	definitionJSON, err := json.Marshal(def.Definition)
	if err != nil {
	}
	_, err = s.Conn.ExecContext(ctx,
		`INSERT OR REPLACE INTO agentflow_definitions (agentflow_id, version, definition, checksum, created_by, comment)
		 VALUES (?1, ?2, ?3, '', ?4, ?5)`,
		def.AgentFlowID, def.Version, definitionJSON, def.CreatedBy, def.Comment)
	return err
}

func (s *SQLiteStore) GetAgentFlow(ctx context.Context, agentFlowID string) (*model.AgentFlowVersion, error) {
	row := s.Conn.QueryRowContext(ctx,
		`SELECT agentflow_id, version, definition, created_by, comment, created_at
		 FROM agentflow_definitions WHERE agentflow_id = ?1
		 ORDER BY version DESC LIMIT 1`, agentFlowID)
	return scanAgentFlowVersion(row)
}

func (s *SQLiteStore) GetAgentFlowVersion(ctx context.Context, agentFlowID string, version int64) (*model.AgentFlowVersion, error) {
	row := s.Conn.QueryRowContext(ctx,
		`SELECT agentflow_id, version, definition, created_by, comment, created_at
		 FROM agentflow_definitions WHERE agentflow_id = ?1 AND version = ?2`,
		agentFlowID, version)
	return scanAgentFlowVersion(row)
}

func (s *SQLiteStore) ListAgentFlows(ctx context.Context) ([]model.AgentFlowVersion, error) {
	rows, err := s.Conn.QueryContext(ctx,
		`SELECT agentflow_id, version, definition, created_by, comment, created_at
		 FROM agentflow_definitions ORDER BY agentflow_id, version DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var defs []model.AgentFlowVersion
	for rows.Next() {
		d, err := scanAgentFlowVersionRow(rows)
		if err != nil {
			return nil, err
		}
		defs = append(defs, *d)
	}
	return defs, nil
}

func (s *SQLiteStore) DeleteAgentFlow(ctx context.Context, agentFlowID string) error {
	_, err := s.Conn.ExecContext(ctx, `DELETE FROM agentflow_definitions WHERE agentflow_id=?`, agentFlowID)
	return err
}

func (s *SQLiteStore) SaveAgentFlowSpec(ctx context.Context, spec *model.AgentFlowSpec, createdBy, comment string) error {
	defJSON, _ := json.Marshal(spec)
	_, err := s.Conn.ExecContext(ctx, `INSERT INTO agentflow_definitions (agentflow_id, version, definition, created_by, comment) VALUES (?,1,?,?,?)`, spec.ID, defJSON, createdBy, comment)
	return err
}

func (s *SQLiteStore) GetAgentFlowSpec(ctx context.Context, agentFlowID string) (*model.AgentFlowSpec, error) {
	ver, err := s.GetAgentFlow(ctx, agentFlowID)
	if err != nil {
		return nil, err
	}
	var spec model.AgentFlowSpec
	if err := json.Unmarshal(ver.Definition, &spec); err != nil {
		return nil, err
	}
	return &spec, nil
}
