package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/flowgent-labs/flowgent/src/common/tracing"
	"github.com/flowgent-labs/flowgent/src/model"
)

var pgTracer = tracing.Tracer("flowgent/postgres")

type PostgresStore struct {
	dsn    string
	pool   *pgxpool.Pool
	schema string
}

func NewPostgresStore(dsn string) *PostgresStore {
	return &PostgresStore{dsn: dsn}
}

func (s *PostgresStore) DB() any { return s.pool }

func (s *PostgresStore) SetPoolConfig(_, _ int) {} // pgxpool config set in Init

func (s *PostgresStore) SetSchema(schema string) { s.schema = schema }

func (s *PostgresStore) Init(ctx context.Context) error {
	cfg, err := pgxpool.ParseConfig(s.dsn)
	if err != nil {
		return fmt.Errorf("parse pg config: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	// Ensure every connection sets the search path.
	schema := s.schema
	if schema == "" {
		schema = "public"
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, fmt.Sprintf("SET search_path TO %s", schema))
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	s.pool = pool
	return nil
}

func (s *PostgresStore) Close() error {
	if s.pool != nil {
		s.pool.Close()
	}
	return nil
}

// ─── Helpers ────────────────────────────────────────────────

func toJSON(v any) []byte {
	if v == nil {
		return []byte("null")
	}
	b, _ := json.Marshal(v)
	return b
}

// ─── AgentFlow definitions ─────────────────────────────────

func (s *PostgresStore) SaveAgentFlowDefinition(ctx context.Context, def *model.AgentFlowVersion) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO agentflow_definitions (agentflow_id, version, definition, created_by, comment)
		 VALUES ($1,$2,$3,$4,$5) ON CONFLICT (agentflow_id, version) DO NOTHING`,
		def.AgentFlowID, def.Version, def.Definition, def.CreatedBy, def.Comment)
	return err
}

func (s *PostgresStore) GetLatestAgentFlowDefinition(ctx context.Context, agentFlowID string) (*model.AgentFlowVersion, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT agentflow_id, version, definition, created_by, comment, created_at
		 FROM agentflow_definitions WHERE agentflow_id=$1 ORDER BY version DESC LIMIT 1`, agentFlowID)
	return scanAgentFlowVersion(row)
}

func (s *PostgresStore) GetAgentFlowDefinition(ctx context.Context, agentFlowID string, version int64) (*model.AgentFlowVersion, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT agentflow_id, version, definition, created_by, comment, created_at
		 FROM agentflow_definitions WHERE agentflow_id=$1 AND version=$2`, agentFlowID, version)
	return scanAgentFlowVersion(row)
}

func (s *PostgresStore) ListAgentFlowDefinitions(ctx context.Context) ([]model.AgentFlowVersion, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT agentflow_id, version, definition, created_by, comment, created_at
		 FROM agentflow_definitions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var defs []model.AgentFlowVersion
	for rows.Next() {
		d, err := scanAgentFlowVersionRow(rows)
		if err != nil {
			continue
		}
		defs = append(defs, *d)
	}
	return defs, nil
}

func (s *PostgresStore) DeleteAgentFlowDefinition(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM agentflow_definitions WHERE agentflow_id=$1`, id)
	return err
}

func (s *PostgresStore) UpdateAgentFlowSpec(ctx context.Context, spec *model.AgentFlowSpec, createdBy, comment string) error {
	defJSON, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	var nextVer int64
	_ = s.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(version),0)+1 FROM agentflow_definitions WHERE agentflow_id=$1`, spec.ID).Scan(&nextVer)
	if nextVer == 0 {
		nextVer = 1
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO agentflow_definitions (agentflow_id,version,definition,created_by,comment,priority,tenant_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (agentflow_id,version) DO NOTHING`,
		spec.ID, nextVer, defJSON, createdBy, comment, string(spec.Priority), spec.TenantID)
	return err
}

func (s *PostgresStore) GetAgentFlowSpec(ctx context.Context, agentFlowID string) (*model.AgentFlowSpec, error) {
	ver, err := s.GetLatestAgentFlowDefinition(ctx, agentFlowID)
	if err != nil {
		return nil, err
	}
	var spec model.AgentFlowSpec
	if err := json.Unmarshal(ver.Definition, &spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

// ─── Agent definitions ─────────────────────────────────────

func (s *PostgresStore) SaveAgent(ctx context.Context, agent *model.AgentDef) error {
	b, _ := json.Marshal(agent)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO agents (name, definition) VALUES ($1,$2) ON CONFLICT (name) DO UPDATE SET definition=$2`,
		agent.Name, b)
	return err
}

func (s *PostgresStore) GetAgent(ctx context.Context, name string) (*model.AgentDef, error) {
	row := s.pool.QueryRow(ctx, `SELECT definition FROM agents WHERE name=$1`, name)
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
	rows, err := s.pool.Query(ctx, `SELECT definition FROM agents`)
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
	_, err := s.pool.Exec(ctx, `DELETE FROM agents WHERE name=$1`, name)
	return err
}

// ─── AgentFlow runs ────────────────────────────────────────

func (s *PostgresStore) CreateAgentFlowRun(ctx context.Context, run *model.AgentFlowRun) error {
	ctx, span := pgTracer.Start(ctx, "CreateAgentFlowRun",
		trace.WithAttributes(
			attribute.String("agentflow_id", run.AgentFlowID),
			attribute.String("run_id", run.ID),
		))
	defer span.End()

	run.ID = newUUID()
	run.CreatedAt = time.Now()
	run.UpdatedAt = run.CreatedAt
	tp, _ := json.Marshal(run.Trigger.Payload)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO agentflow_runs (id,agentflow_id,version,status,vars,trigger_type,trigger_source,trigger_payload,created_at,updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		run.ID, run.AgentFlowID, run.Version, run.Status, toJSON(run.Vars), run.Trigger.Type, run.Trigger.Source, tp, run.CreatedAt, run.UpdatedAt)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("error", err.Error()))
	}
	return err
}

func (s *PostgresStore) UpdateAgentFlowRun(ctx context.Context, run *model.AgentFlowRun) error {
	run.UpdatedAt = time.Now()
	_, err := s.pool.Exec(ctx,
		`UPDATE agentflow_runs SET status=$1,vars=$2,output=$3,error=$4,updated_at=$5,started_at=$6,finished_at=$7 WHERE id=$8`,
		run.Status, toJSON(run.Vars), toJSON(run.Output), run.Error, run.UpdatedAt, run.StartedAt, run.FinishedAt, run.ID)
	return err
}

func (s *PostgresStore) GetAgentFlowRun(ctx context.Context, id string) (*model.AgentFlowRun, error) {
	row := s.pool.QueryRow(ctx, `SELECT id,agentflow_id,version,status,vars,output,error,trigger_type,trigger_source,trigger_payload,created_at,updated_at,started_at,finished_at FROM agentflow_runs WHERE id=$1`, id)
	return scanAgentFlowRun(row)
}

func (s *PostgresStore) ListAgentFlowRuns(ctx context.Context, agentFlowID string, limit int) ([]model.AgentFlowRun, error) {
	if limit <= 0 { limit = 50 }
	var rows pgxRows
	var err error
	if agentFlowID == "" {
		rows, err = s.pool.Query(ctx, runSelectSQL+" ORDER BY created_at DESC LIMIT $1", limit)
	} else {
		rows, err = s.pool.Query(ctx, runSelectSQL+" WHERE agentflow_id=$1 ORDER BY created_at DESC LIMIT $2", agentFlowID, limit)
	}
	if err != nil { return nil, err }
	defer rows.Close()
	return collectRunRows(rows)
}

const runSelectSQL = `SELECT id,agentflow_id,version,status,vars,output,error,trigger_type,trigger_source,trigger_payload,created_at,updated_at,started_at,finished_at FROM agentflow_runs`

func (s *PostgresStore) ListActiveRuns(ctx context.Context) ([]model.AgentFlowRun, error) {
	rows, err := s.pool.Query(ctx, runSelectSQL+" WHERE status IN ('RUNNING','PAUSED') ORDER BY created_at DESC")
	if err != nil { return nil, err }
	defer rows.Close()
	return collectRunRows(rows)
}

func (s *PostgresStore) DeleteAgentFlowRun(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM agentflow_runs WHERE id=$1`, id)
	return err
}

func (s *PostgresStore) CancelAgentFlowRun(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE agentflow_runs SET status='CANCELLED' WHERE id=$1`, id)
	return err
}

// ─── Task runs ─────────────────────────────────────────────

func (s *PostgresStore) CreateTaskRun(ctx context.Context, task *model.TaskRun) error {
	task.ID = newUUID()
	task.CreatedAt = time.Now()
	task.UpdatedAt = task.CreatedAt
	_, err := s.pool.Exec(ctx,
		`INSERT INTO task_runs (id,agentflow_run_id,node_id,status,input,output,error,retry_count,max_retries,exec_id,parent_task_run_id,sequence,created_at,updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		task.ID, task.AgentFlowRunID, task.NodeID, task.Status, toJSON(task.Input), toJSON(task.Output), task.Error,
		task.RetryCount, task.MaxRetries, task.ExecID, task.ParentTaskRunID, task.Sequence, task.CreatedAt, task.UpdatedAt)
	return err
}

func (s *PostgresStore) UpdateTaskRun(ctx context.Context, task *model.TaskRun) error {
	task.UpdatedAt = time.Now()
	_, err := s.pool.Exec(ctx,
		`UPDATE task_runs SET status=$1,input=$2,output=$3,error=$4,retry_count=$5,max_retries=$6,exec_id=$7,parent_task_run_id=$8,sequence=$9,updated_at=$10,started_at=$11,finished_at=$12 WHERE id=$13`,
		task.Status, toJSON(task.Input), toJSON(task.Output), task.Error, task.RetryCount, task.MaxRetries, task.ExecID, task.ParentTaskRunID, task.Sequence, task.UpdatedAt, task.StartedAt, task.FinishedAt, task.ID)
	return err
}

func (s *PostgresStore) GetTaskRun(ctx context.Context, id string) (*model.TaskRun, error) {
	row := s.pool.QueryRow(ctx, `SELECT id,agentflow_run_id,node_id,status,input,output,error,retry_count,max_retries,exec_id,parent_task_run_id,sequence,created_at,updated_at,started_at,finished_at FROM task_runs WHERE id=$1`, id)
	return scanTaskRun(row)
}

func (s *PostgresStore) GetTaskRunsByAgentFlowRun(ctx context.Context, runID string) ([]model.TaskRun, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,agentflow_run_id,node_id,status,input,output,error,retry_count,max_retries,exec_id,parent_task_run_id,sequence,created_at,updated_at,started_at,finished_at FROM task_runs WHERE agentflow_run_id=$1 ORDER BY created_at`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []model.TaskRun
	for rows.Next() {
		t, err := scanTaskRunRow(rows)
		if err != nil {
			continue
		}
		tasks = append(tasks, *t)
	}
	return tasks, nil
}

func (s *PostgresStore) GetTaskRunByExecID(ctx context.Context, execID string) (*model.TaskRun, error) {
	row := s.pool.QueryRow(ctx, `SELECT id,agentflow_run_id,node_id,status,input,output,error,retry_count,max_retries,exec_id,parent_task_run_id,sequence,created_at,updated_at,started_at,finished_at FROM task_runs WHERE exec_id=$1`, execID)
	return scanTaskRun(row)
}

// ─── Human approvals ───────────────────────────────────────

func (s *PostgresStore) CreateHumanApproval(ctx context.Context, approval *model.HumanApproval) error {
	approval.Token = newUUID()
	approval.CreatedAt = time.Now()
	approval.UpdatedAt = approval.CreatedAt
	if approval.Timeout > 0 {
		exp := approval.CreatedAt.Add(approval.Timeout)
		approval.ExpiresAt = &exp
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO human_approvals (task_run_id,token,status,timeout_seconds,created_at,updated_at,expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		approval.TaskRunID, approval.Token, approval.Status, int(approval.Timeout.Seconds()), approval.CreatedAt, approval.UpdatedAt, approval.ExpiresAt)
	return err
}

func (s *PostgresStore) GetHumanApproval(ctx context.Context, token string) (*model.HumanApproval, error) {
	row := s.pool.QueryRow(ctx, `SELECT task_run_id,token,status,approved,comment,timeout_seconds,created_at,updated_at,expires_at,resolved_at FROM human_approvals WHERE token=$1`, token)
	return scanHumanApproval(row)
}

func (s *PostgresStore) UpdateHumanApproval(ctx context.Context, approval *model.HumanApproval) error {
	now := time.Now()
	approval.UpdatedAt = now
	if approval.Approved != nil {
		approval.ResolvedAt = &now
	}
	_, err := s.pool.Exec(ctx, `UPDATE human_approvals SET status=$1,approved=$2,comment=$3,updated_at=$4,resolved_at=$5 WHERE token=$6`,
		approval.Status, approval.Approved, approval.Comment, approval.UpdatedAt, approval.ResolvedAt, approval.Token)
	return err
}

func (s *PostgresStore) GetPendingApprovals(ctx context.Context) ([]model.HumanApproval, error) {
	rows, err := s.pool.Query(ctx, `SELECT task_run_id,token,status,approved,comment,timeout_seconds,created_at,updated_at,expires_at,resolved_at FROM human_approvals WHERE status='PENDING' AND (expires_at IS NULL OR expires_at > NOW())`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var approvals []model.HumanApproval
	for rows.Next() {
		a, err := scanHumanApprovalRow(rows)
		if err != nil {
			continue
		}
		approvals = append(approvals, *a)
	}
	return approvals, nil
}

// ─── Supervisor log ────────────────────────────────────────

func (s *PostgresStore) LogSupervisorDecision(ctx context.Context, agentFlowRunID, taskRunID string, input, decision map[string]any) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO supervisor_log (agentflow_run_id,task_run_id,input_snapshot,decision) VALUES ($1,$2,$3,$4)`,
		agentFlowRunID, taskRunID, toJSON(input), toJSON(decision))
	return err
}

// ─── Notifier channels ─────────────────────────────────────

func (s *PostgresStore) SaveNotifierChannel(ctx context.Context, ch *model.NotifierChannel) error { return nil }
func (s *PostgresStore) GetNotifierChannel(ctx context.Context, id string) (*model.NotifierChannel, error) { return nil, nil }
func (s *PostgresStore) ListNotifierChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error) { return nil, nil }
func (s *PostgresStore) DeleteNotifierChannel(ctx context.Context, id string) error { return nil }

// ─── Subscription routes ───────────────────────────────────

func (s *PostgresStore) SaveSubscriptionRoute(ctx context.Context, r *model.SubscriptionRoute) error { return nil }
func (s *PostgresStore) GetSubscriptionRoutesByAgentFlow(ctx context.Context, id string) ([]model.SubscriptionRoute, error) { return nil, nil }
func (s *PostgresStore) DeleteSubscriptionRoute(ctx context.Context, id string) error { return nil }
func (s *PostgresStore) DeleteSubscriptionRoutesByPod(ctx context.Context, podID string) error { return nil }
func (s *PostgresStore) CleanupOrphanedRoutes(ctx context.Context, podID string, maxAge time.Duration) (int64, error) { return 0, nil }

// ─── ExecutionPlan persistence ─────────────────────────────

func (s *PostgresStore) SaveExecutionPlan(ctx context.Context, plan *model.ExecutionPlan) error {
	now := time.Now()
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = now
	}
	output := map[string]any{}
	errStr := ""
	if plan.Result != nil {
		output = plan.Result.Output
		errStr = plan.Result.Error
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO task_runs (id,agentflow_run_id,node_id,status,input,output,error,retry_count,max_retries,exec_id,created_at,updated_at,started_at,finished_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		 ON CONFLICT (id) DO UPDATE SET status=$4,input=$5,output=$6,error=$7,retry_count=$8,updated_at=$12,started_at=$13,finished_at=$14`,
		plan.TaskID, plan.AgentFlowRunID, plan.NodeID, string(plan.State), toJSON(plan.Input), toJSON(output), errStr,
		plan.RetryCount, plan.MaxRetries, plan.PlanID, plan.CreatedAt, now, plan.StartedAt, plan.FinishedAt)
	return err
}

func (s *PostgresStore) LoadExecutionPlan(ctx context.Context, id string) (*model.ExecutionPlan, error) {
	task, err := s.GetTaskRun(ctx, id)
	if err != nil {
		return nil, err
	}
	return &model.ExecutionPlan{
		PlanID:         task.ExecID,
		AgentFlowRunID: task.AgentFlowRunID,
		TaskID:         task.ID,
		NodeID:         task.NodeID,
		State:          task.Status,
		RetryCount:     task.RetryCount,
		MaxRetries:     task.MaxRetries,
		Input:          task.Input,
		Result:         &model.TaskResult{Output: task.Output, Error: task.Error},
		CreatedAt:      task.CreatedAt,
		StartedAt:      task.StartedAt,
		FinishedAt:     task.FinishedAt,
	}, nil
}

func (s *PostgresStore) ListExecutionPlans(ctx context.Context, runID string) ([]*model.ExecutionPlan, error) {
	tasks, err := s.GetTaskRunsByAgentFlowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	plans := make([]*model.ExecutionPlan, len(tasks))
	for i, t := range tasks {
		plans[i] = &model.ExecutionPlan{
			PlanID:         t.ExecID,
			AgentFlowRunID: t.AgentFlowRunID,
			TaskID:         t.ID,
			NodeID:         t.NodeID,
			State:          t.Status,
			RetryCount:     t.RetryCount,
			MaxRetries:     t.MaxRetries,
			Input:          t.Input,
			Result:         &model.TaskResult{Output: t.Output, Error: t.Error},
			CreatedAt:      t.CreatedAt,
			StartedAt:      t.StartedAt,
			FinishedAt:     t.FinishedAt,
		}
	}
	return plans, nil
}
func (s *PostgresStore) SaveCheckpoint(ctx context.Context, planID string, cp *model.TaskCheckpoint) error { return nil }
func (s *PostgresStore) LoadCheckpoint(ctx context.Context, planID string) (*model.TaskCheckpoint, error) { return nil, nil }
func (s *PostgresStore) ClaimLease(ctx context.Context, planID, tmID string, dur time.Duration) error { return nil }
func (s *PostgresStore) ReleaseLease(ctx context.Context, planID string) error { return nil }

// ─── Row helpers ──────────────────────────────────────────

type pgxRows interface {
	Close()
	Next() bool
	Scan(...any) error
	Err() error
}

func collectRunRows(rows pgxRows) ([]model.AgentFlowRun, error) {
	var runs []model.AgentFlowRun
	for rows.Next() {
		r, err := scanAgentFlowRunRow(rows)
		if err != nil { continue }
		runs = append(runs, *r)
	}
	return runs, rows.Err()
}

// ─── UUID ──────────────────────────────────────────────────

func newUUID() string { return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
	time.Now().UnixNano()&0xFFFFFFFF, (time.Now().UnixNano()>>32)&0xFFFF,
	(time.Now().UnixNano()>>48)&0xFFFF, (time.Now().UnixNano()>>48)&0xFFFF|0x4000,
	time.Now().UnixNano()&0xFFFFFFFFFFFF) }
