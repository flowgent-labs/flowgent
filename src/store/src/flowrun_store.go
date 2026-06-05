package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/flowgent-labs/flowgent/model/src"
)

// IFlowRunStore manages AgentFlow runs.
type IFlowRunStore interface {
	CreateFlowRun(ctx context.Context, run *model.AgentFlowRun) error
	UpdateFlowRun(ctx context.Context, run *model.AgentFlowRun) error
	GetFlowRun(ctx context.Context, id string) (*model.AgentFlowRun, error)
	ListFlowRuns(ctx context.Context, agentFlowID string, limit int) ([]model.AgentFlowRun, error)
	ListActiveRuns(ctx context.Context) ([]model.AgentFlowRun, error)
	DeleteFlowRun(ctx context.Context, id string) error
	CancelFlowRun(ctx context.Context, id string) error
}

const runSelectSQL = `SELECT id,agentflow_id,version,status,vars,output,error,trigger_type,trigger_source,trigger_payload,created_at,updated_at,tenant_id,namespace,priority,started_at,finished_at FROM agentflow_runs`

// ─── PostgresStore methods — flowrun ──────────────────────────

func (s *PostgresStore) CreateFlowRun(ctx context.Context, run *model.AgentFlowRun) error {
	ctx, span := pgTracer.Start(ctx, "CreateFlowRun",
		trace.WithAttributes(
			attribute.String("agentflow_id", run.AgentFlowID),
			attribute.String("run_id", run.ID),
		))
	defer span.End()

	run.ID = newUUID()
	run.CreatedAt = time.Now()
	run.UpdatedAt = run.CreatedAt
	tp, _ := json.Marshal(run.Trigger.Payload)
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO agentflow_runs (id,agentflow_id,version,status,vars,trigger_type,trigger_source,trigger_payload,created_at,updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		run.ID, run.AgentFlowID, run.Version, run.Status, toJSON(run.Vars), run.Trigger.Type, run.Trigger.Source, tp, run.CreatedAt, run.UpdatedAt)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("error", err.Error()))
	}
	return err
}

func (s *PostgresStore) UpdateFlowRun(ctx context.Context, run *model.AgentFlowRun) error {
	run.UpdatedAt = time.Now()
	_, err := s.Pool.Exec(ctx,
		`UPDATE agentflow_runs SET status=$1,vars=$2,output=$3,error=$4,updated_at=$5,started_at=$6,finished_at=$7 WHERE id=$8`,
		run.Status, toJSON(run.Vars), toJSON(run.Output), run.Error, run.UpdatedAt, run.StartedAt, run.FinishedAt, run.ID)
	return err
}

func (s *PostgresStore) GetFlowRun(ctx context.Context, id string) (*model.AgentFlowRun, error) {
	row := s.Pool.QueryRow(ctx, `SELECT id,agentflow_id,version,status,vars,output,error,trigger_type,trigger_source,trigger_payload,created_at,updated_at,tenant_id,namespace,priority,started_at,finished_at FROM agentflow_runs WHERE id=$1`, id)
	return scanAgentFlowRun(row)
}

func (s *PostgresStore) ListFlowRuns(ctx context.Context, agentFlowID string, limit int) ([]model.AgentFlowRun, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows pgxRows
	var err error
	if agentFlowID == "" {
		rows, err = s.Pool.Query(ctx, runSelectSQL+" ORDER BY created_at DESC LIMIT $1", limit)
	} else {
		rows, err = s.Pool.Query(ctx, runSelectSQL+" WHERE agentflow_id=$1 ORDER BY created_at DESC LIMIT $2", agentFlowID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRunRows(rows)
}

func (s *PostgresStore) ListActiveRuns(ctx context.Context) ([]model.AgentFlowRun, error) {
	rows, err := s.Pool.Query(ctx, runSelectSQL+" WHERE status IN ('RUNNING','PAUSED') ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRunRows(rows)
}

func (s *PostgresStore) DeleteFlowRun(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM agentflow_runs WHERE id=$1`, id)
	return err
}

func (s *PostgresStore) CancelFlowRun(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE agentflow_runs SET status='CANCELLED' WHERE id=$1`, id)
	return err
}

// ─── SQLiteStore methods — flowrun ────────────────────────────

func (s *SQLiteStore) CreateFlowRun(ctx context.Context, run *model.AgentFlowRun) error {
	run.ID = uuid.New().String()
	run.CreatedAt = time.Now()
	run.UpdatedAt = run.CreatedAt
	triggerPayload, _ := json.Marshal(run.Trigger.Payload)
	_, err := s.Conn.ExecContext(ctx,
		`INSERT INTO agentflow_runs (id, agentflow_id, version, status, vars, trigger_type, trigger_source, trigger_payload, created_at, updated_at)
		 VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10)`,
		run.ID, run.AgentFlowID, run.Version, run.Status, toJSON(run.Vars), run.Trigger.Type, run.Trigger.Source, triggerPayload, run.CreatedAt, run.UpdatedAt)
	return err
}

func (s *SQLiteStore) UpdateFlowRun(ctx context.Context, run *model.AgentFlowRun) error {
	run.UpdatedAt = time.Now()
	_, err := s.Conn.ExecContext(ctx,
		`UPDATE agentflow_runs SET status=?1,vars=?2,output=?3,error=?4,updated_at=?5,started_at=?6,finished_at=?7 WHERE id=?8`,
		run.Status, toJSON(run.Vars), toJSON(run.Output), run.Error, run.UpdatedAt, run.StartedAt, run.FinishedAt, run.ID)
	return err
}

func (s *SQLiteStore) GetFlowRun(ctx context.Context, id string) (*model.AgentFlowRun, error) {
	row := s.Conn.QueryRowContext(ctx,
		`SELECT id,agentflow_id,version,status,vars,output,error,trigger_type,trigger_source,trigger_payload,created_at,updated_at,started_at,finished_at
		 FROM agentflow_runs WHERE id=?1`, id)
	return scanAgentFlowRun(row)
}

func (s *SQLiteStore) ListFlowRuns(ctx context.Context, agentFlowID string, limit int) ([]model.AgentFlowRun, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows *sql.Rows
	var err error
	if agentFlowID == "" {
		rows, err = s.Conn.QueryContext(ctx,
			`SELECT id,agentflow_id,version,status,vars,output,error,trigger_type,trigger_source,trigger_payload,created_at,updated_at,started_at,finished_at
			 FROM agentflow_runs ORDER BY created_at DESC LIMIT ?1`, limit)
	} else {
		rows, err = s.Conn.QueryContext(ctx,
			`SELECT id,agentflow_id,version,status,vars,output,error,trigger_type,trigger_source,trigger_payload,created_at,updated_at,started_at,finished_at
			 FROM agentflow_runs WHERE agentflow_id=?1 ORDER BY created_at DESC LIMIT ?2`,
			agentFlowID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []model.AgentFlowRun
	for rows.Next() {
		r, err := scanAgentFlowRunRow(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, *r)
	}
	return runs, nil
}

func (s *SQLiteStore) ListActiveRuns(ctx context.Context) ([]model.AgentFlowRun, error) {
	rows, err := s.Conn.QueryContext(ctx,
		`SELECT id,agentflow_id,version,status,vars,output,error,trigger_type,trigger_source,trigger_payload,created_at,updated_at,started_at,finished_at
		 FROM agentflow_runs WHERE status IN ('RUNNING','PAUSED') ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []model.AgentFlowRun
	for rows.Next() {
		r, err := scanAgentFlowRunRow(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, *r)
	}
	return runs, nil
}

func (s *SQLiteStore) DeleteFlowRun(ctx context.Context, id string) error {
	_, err := s.Conn.ExecContext(ctx, `DELETE FROM agentflow_runs WHERE id=?`, id)
	return err
}

func (s *SQLiteStore) CancelFlowRun(ctx context.Context, id string) error {
	_, err := s.Conn.ExecContext(ctx, `UPDATE agentflow_runs SET status='CANCELLED' WHERE id=?`, id)
	return err
}
