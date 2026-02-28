package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/flowgent-labs/flowgent/src/model"
)


type PostgresStore struct {
	dsn            string
	db             *sql.DB
	minConnections int
	maxConnections int
	schema         string
}

func NewPostgresStore(dsn string) *PostgresStore {
	return &PostgresStore{dsn: dsn}
}

func (s *PostgresStore) DB() *sql.DB {
	return s.db
}

func (s *PostgresStore) SetPoolConfig(minConn, maxConn int) {
	s.minConnections = minConn
	s.maxConnections = maxConn
}

func (s *PostgresStore) SetSchema(schema string) {
	s.schema = schema
}

func (s *PostgresStore) Init(ctx context.Context) error {
	db, err := sql.Open("pgx", s.dsn)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	minConn := s.minConnections
	maxConn := s.maxConnections
	if minConn <= 0 {
		minConn = 2
	}
	if maxConn <= 0 {
		maxConn = 20
	}
	db.SetMaxOpenConns(maxConn)
	db.SetMaxIdleConns(minConn)
	db.SetConnMaxLifetime(time.Hour)
	s.db = db

	if s.schema != "" {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", s.schema)); err != nil {
			return fmt.Errorf("set schema: %w", err)
		}
	}

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	// TODO: fix postgres migration SQL (currently SQLite-specific)
	_ = RunMigrations(db, "postgres")
	return nil
}

func (s *PostgresStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// --- AgentFlow definitions ---

func (s *PostgresStore) SaveAgentFlowDefinition(ctx context.Context, def *model.AgentFlowVersion) error {
	definitionJSON, err := json.Marshal(def.Definition)
	if err != nil {
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO agentflow_definitions (agentflow_id, version, definition, checksum, created_by, comment)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (agentflow_id) DO UPDATE
		 SET version = $2, definition = $3, checksum = $4, created_by = $5, comment = $6, created_at = NOW()`,
		def.AgentFlowID, def.Version, definitionJSON, "", def.CreatedBy, def.Comment)
	return err
}

func (s *PostgresStore) GetLatestAgentFlowDefinition(ctx context.Context, agentFlowID string) (*model.AgentFlowVersion, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT agentflow_id, version, definition, created_by, comment, created_at
		 FROM agentflow_definitions WHERE agentflow_id = $1
		 ORDER BY version DESC LIMIT 1`, agentFlowID)
	return scanAgentFlowVersion(row)
}

func (s *PostgresStore) GetAgentFlowDefinition(ctx context.Context, agentFlowID string, version int64) (*model.AgentFlowVersion, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT agentflow_id, version, definition, created_by, comment, created_at
		 FROM agentflow_definitions WHERE agentflow_id = $1 AND version = $2`,
		agentFlowID, version)
	return scanAgentFlowVersion(row)
}

func (s *PostgresStore) ListAgentFlowDefinitions(ctx context.Context) ([]model.AgentFlowVersion, error) {
	rows, err := s.db.QueryContext(ctx,
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
	return defs, rows.Err()
}

// --- AgentFlow runs ---

func (s *PostgresStore) CreateAgentFlowRun(ctx context.Context, run *model.AgentFlowRun) error {
	run.ID = uuid.New().String()
	run.CreatedAt = time.Now()
	run.UpdatedAt = run.CreatedAt
	triggerPayload, _ := json.Marshal(run.Trigger.Payload)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agentflow_runs (id, agentflow_id, version, status, vars, trigger_type, trigger_source, trigger_payload, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		run.ID, run.AgentFlowID, run.Version, run.Status, toJSON(run.Vars), run.Trigger.Type, run.Trigger.Source, triggerPayload, run.CreatedAt, run.UpdatedAt)
	return err
}

func (s *PostgresStore) UpdateAgentFlowRun(ctx context.Context, run *model.AgentFlowRun) error {
	run.UpdatedAt = time.Now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE agentflow_runs SET status=$1, vars=$2, output=$3, error=$4, updated_at=$5, started_at=$6, finished_at=$7 WHERE id=$8`,
		run.Status, toJSON(run.Vars), toJSON(run.Output), run.Error, run.UpdatedAt, run.StartedAt, run.FinishedAt, run.ID)
	return err
}

func (s *PostgresStore) GetAgentFlowRun(ctx context.Context, id string) (*model.AgentFlowRun, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, agentflow_id, version, status, vars, output, error, trigger_type, trigger_source, trigger_payload, created_at, updated_at, started_at, finished_at
		 FROM agentflow_runs WHERE id = $1`, id)
	return scanAgentFlowRun(row)
}

func (s *PostgresStore) ListAgentFlowRuns(ctx context.Context, agentFlowID string, limit int) ([]model.AgentFlowRun, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows *sql.Rows
	var err error
	if agentFlowID == "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, agentflow_id, version, status, vars, output, error, trigger_type, trigger_source, trigger_payload, created_at, updated_at, started_at, finished_at
			 FROM agentflow_runs ORDER BY created_at DESC LIMIT $1`, limit)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, agentflow_id, version, status, vars, output, error, trigger_type, trigger_source, trigger_payload, created_at, updated_at, started_at, finished_at
			 FROM agentflow_runs WHERE agentflow_id = $1 ORDER BY created_at DESC LIMIT $2`,
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
	return runs, rows.Err()
}

func (s *PostgresStore) ListActiveRuns(ctx context.Context) ([]model.AgentFlowRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agentflow_id, version, status, vars, output, error, trigger_type, trigger_source, trigger_payload, created_at, updated_at, started_at, finished_at
		 FROM agentflow_runs WHERE status IN ('RUNNING', 'PAUSED') ORDER BY created_at DESC`)
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
	return runs, rows.Err()
}

// --- Task runs ---

func (s *PostgresStore) CreateTaskRun(ctx context.Context, task *model.TaskRun) error {
	task.ID = uuid.New().String()
	task.CreatedAt = time.Now()
	task.UpdatedAt = task.CreatedAt
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO task_runs (id, agentflow_run_id, node_id, status, input, output, error, retry_count, max_retries, exec_id, parent_task_run_id, sequence, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		task.ID, task.AgentFlowRunID, task.NodeID, task.Status, toJSON(task.Input), toJSON(task.Output), task.Error, task.RetryCount, task.MaxRetries, task.ExecID, task.ParentTaskRunID, task.Sequence, task.CreatedAt, task.UpdatedAt)
	return err
}

func (s *PostgresStore) UpdateTaskRun(ctx context.Context, task *model.TaskRun) error {
	task.UpdatedAt = time.Now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE task_runs SET status=$1, input=$2, output=$3, error=$4, retry_count=$5, max_retries=$6, exec_id=$7, parent_task_run_id=$8, sequence=$9, updated_at=$10, started_at=$11, finished_at=$12 WHERE id=$13`,
		task.Status, toJSON(task.Input), toJSON(task.Output), task.Error, task.RetryCount, task.MaxRetries, task.ExecID, task.ParentTaskRunID, task.Sequence, task.UpdatedAt, task.StartedAt, task.FinishedAt, task.ID)
	return err
}

func (s *PostgresStore) GetTaskRun(ctx context.Context, id string) (*model.TaskRun, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, agentflow_run_id, node_id, status, input, output, error, retry_count, max_retries, exec_id, parent_task_run_id, sequence, created_at, updated_at, started_at, finished_at
		 FROM task_runs WHERE id = $1`, id)
	return scanTaskRun(row)
}

func (s *PostgresStore) GetTaskRunsByAgentFlowRun(ctx context.Context, agentFlowRunID string) ([]model.TaskRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agentflow_run_id, node_id, status, input, output, error, retry_count, max_retries, exec_id, parent_task_run_id, sequence, created_at, updated_at, started_at, finished_at
		 FROM task_runs WHERE agentflow_run_id = $1 ORDER BY created_at`, agentFlowRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []model.TaskRun
	for rows.Next() {
		t, err := scanTaskRunRow(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *t)
	}
	return tasks, rows.Err()
}

func (s *PostgresStore) GetTaskRunByExecID(ctx context.Context, execID string) (*model.TaskRun, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, agentflow_run_id, node_id, status, input, output, error, retry_count, max_retries, exec_id, parent_task_run_id, sequence, created_at, updated_at, started_at, finished_at
		 FROM task_runs WHERE exec_id = $1`, execID)
	return scanTaskRun(row)
}

// --- Human approvals ---

func (s *PostgresStore) CreateHumanApproval(ctx context.Context, approval *model.HumanApproval) error {
	approval.Token = uuid.New().String()
	approval.CreatedAt = time.Now()
	approval.UpdatedAt = approval.CreatedAt
	if approval.Timeout > 0 {
		expires := approval.CreatedAt.Add(approval.Timeout)
		approval.ExpiresAt = &expires
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO human_approvals (task_run_id, token, status, timeout_seconds, created_at, updated_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		approval.TaskRunID, approval.Token, approval.Status, int(approval.Timeout.Seconds()), approval.CreatedAt, approval.UpdatedAt, approval.ExpiresAt)
	return err
}

func (s *PostgresStore) GetHumanApproval(ctx context.Context, token string) (*model.HumanApproval, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT task_run_id, token, status, approved, comment, timeout_seconds, created_at, updated_at, expires_at, resolved_at
		 FROM human_approvals WHERE token = $1`, token)
	return scanHumanApproval(row)
}

func (s *PostgresStore) UpdateHumanApproval(ctx context.Context, approval *model.HumanApproval) error {
	approval.UpdatedAt = time.Now()
	if approval.Approved != nil {
		now := time.Now()
		approval.ResolvedAt = &now
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE human_approvals SET status=$1, approved=$2, comment=$3, updated_at=$4, resolved_at=$5 WHERE token=$6`,
		approval.Status, approval.Approved, approval.Comment, approval.UpdatedAt, approval.ResolvedAt, approval.Token)
	return err
}

func (s *PostgresStore) GetPendingApprovals(ctx context.Context) ([]model.HumanApproval, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_run_id, token, status, approved, comment, timeout_seconds, created_at, updated_at, expires_at, resolved_at
		 FROM human_approvals WHERE status = 'PENDING' AND (expires_at IS NULL OR expires_at > NOW())`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var approvals []model.HumanApproval
	for rows.Next() {
		a, err := scanHumanApprovalRow(rows)
		if err != nil {
			return nil, err
		}
		approvals = append(approvals, *a)
	}
	return approvals, rows.Err()
}


// --- Supervisor log ---

func (s *PostgresStore) LogSupervisorDecision(ctx context.Context, agentFlowRunID, taskRunID string, input, decision map[string]any) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO supervisor_log (agentflow_run_id, task_run_id, input_snapshot, decision) VALUES ($1, $2, $3, $4)`,
		agentFlowRunID, taskRunID, toJSON(input), toJSON(decision))
	return err
}
// ExecutionPlan stubs (TODO: full SQL implementation)
func (s *PostgresStore) SaveExecutionPlan(ctx context.Context, plan *model.ExecutionPlan) error { return nil }
func (s *PostgresStore) LoadExecutionPlan(ctx context.Context, planID string) (*model.ExecutionPlan, error) { return nil, nil }
func (s *PostgresStore) ListExecutionPlans(ctx context.Context, agentFlowRunID string) ([]*model.ExecutionPlan, error) { return nil, nil }
func (s *PostgresStore) SaveCheckpoint(ctx context.Context, planID string, cp *model.TaskCheckpoint) error { return nil }
func (s *PostgresStore) LoadCheckpoint(ctx context.Context, planID string) (*model.TaskCheckpoint, error) { return nil, nil }
func (s *PostgresStore) ClaimLease(ctx context.Context, planID, tmID string, dur time.Duration) error { return nil }
func (s *PostgresStore) ReleaseLease(ctx context.Context, planID string) error { return nil }
