package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	Conn *sql.DB
	Dir  string
}

func NewSQLiteStore(dir string) *SQLiteStore {
	return &SQLiteStore{Dir: dir}
}

func (s *SQLiteStore) DB() any { return s.Conn }

func (s *SQLiteStore) Init(ctx context.Context) error {
	if err := os.MkdirAll(s.Dir, 0755); err != nil {
		return fmt.Errorf("create sqlite dir: %w", err)
	}
	dbPath := filepath.Join(s.Dir, "flowgent.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal=WAL&_busy_timeout=5000")
	if err != nil { return fmt.Errorf("open sqlite: %w", err) }
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s.Conn = db
	return RunMigrations(db, "sqlite")
}

func (s *SQLiteStore) Close() error {
	if s.Conn != nil { s.Conn.Close() }
	return nil
}

// --- AgentFlow definitions ---

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

// --- AgentFlow runs ---

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

// --- Task runs ---

func (s *SQLiteStore) CreateTaskRun(ctx context.Context, task *model.TaskRun) error {
	task.ID = uuid.New().String()
	task.CreatedAt = time.Now()
	task.UpdatedAt = task.CreatedAt
	_, err := s.Conn.ExecContext(ctx,
		`INSERT INTO task_runs (id,agentflow_run_id,node_id,status,input,output,error,retry_count,max_retries,exec_id,parent_task_run_id,sequence,created_at,updated_at)
		 VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?12,?13,?14)`,
		task.ID, task.AgentFlowRunID, task.NodeID, task.Status, toJSON(task.Input), toJSON(task.Output), task.Error, task.RetryCount, task.MaxRetries, task.ExecID, task.ParentTaskRunID, task.Sequence, task.CreatedAt, task.UpdatedAt)
	return err
}

func (s *SQLiteStore) UpdateTaskRun(ctx context.Context, task *model.TaskRun) error {
	task.UpdatedAt = time.Now()
	_, err := s.Conn.ExecContext(ctx,
		`UPDATE task_runs SET status=?1,input=?2,output=?3,error=?4,retry_count=?5,max_retries=?6,exec_id=?7,parent_task_run_id=?8,sequence=?9,updated_at=?10,started_at=?11,finished_at=?12 WHERE id=?13`,
		task.Status, toJSON(task.Input), toJSON(task.Output), task.Error, task.RetryCount, task.MaxRetries, task.ExecID, task.ParentTaskRunID, task.Sequence, task.UpdatedAt, task.StartedAt, task.FinishedAt, task.ID)
	return err
}

func (s *SQLiteStore) GetTaskRun(ctx context.Context, id string) (*model.TaskRun, error) {
	row := s.Conn.QueryRowContext(ctx,
		`SELECT id,agentflow_run_id,node_id,status,input,output,error,retry_count,max_retries,exec_id,parent_task_run_id,sequence,created_at,updated_at,started_at,finished_at
		 FROM task_runs WHERE id=?1`, id)
	return scanTaskRun(row)
}

func (s *SQLiteStore) ListTaskRunsByFlow(ctx context.Context, agentFlowRunID string) ([]model.TaskRun, error) {
	rows, err := s.Conn.QueryContext(ctx,
		`SELECT id,agentflow_run_id,node_id,status,input,output,error,retry_count,max_retries,exec_id,parent_task_run_id,sequence,created_at,updated_at,started_at,finished_at
		 FROM task_runs WHERE agentflow_run_id=?1 ORDER BY created_at`, agentFlowRunID)
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
	return tasks, nil
}

func (s *SQLiteStore) GetTaskRunByExecID(ctx context.Context, execID string) (*model.TaskRun, error) {
	row := s.Conn.QueryRowContext(ctx,
		`SELECT id,agentflow_run_id,node_id,status,input,output,error,retry_count,max_retries,exec_id,parent_task_run_id,sequence,created_at,updated_at,started_at,finished_at
		 FROM task_runs WHERE exec_id=?1`, execID)
	return scanTaskRun(row)
}

// --- Human approvals ---

func (s *SQLiteStore) CreateApproval(ctx context.Context, approval *model.HumanApproval) error {
	approval.Token = uuid.New().String()
	approval.CreatedAt = time.Now()
	approval.UpdatedAt = approval.CreatedAt
	if approval.Timeout > 0 {
		expires := approval.CreatedAt.Add(approval.Timeout)
		approval.ExpiresAt = &expires
	}
	_, err := s.Conn.ExecContext(ctx,
		`INSERT INTO human_approvals (task_run_id,token,status,timeout_seconds,created_at,updated_at,expires_at)
		 VALUES (?1,?2,?3,?4,?5,?6,?7)`,
		approval.TaskRunID, approval.Token, approval.Status, int(approval.Timeout.Seconds()), approval.CreatedAt, approval.UpdatedAt, approval.ExpiresAt)
	return err
}

func (s *SQLiteStore) GetApproval(ctx context.Context, token string) (*model.HumanApproval, error) {
	row := s.Conn.QueryRowContext(ctx,
		`SELECT task_run_id,token,status,approved,comment,timeout_seconds,created_at,updated_at,expires_at,resolved_at
		 FROM human_approvals WHERE token=?1`, token)
	return scanHumanApproval(row)
}

func (s *SQLiteStore) UpdateApproval(ctx context.Context, approval *model.HumanApproval) error {
	approval.UpdatedAt = time.Now()
	if approval.Approved != nil {
		now := time.Now()
		approval.ResolvedAt = &now
	}
	_, err := s.Conn.ExecContext(ctx,
		`UPDATE human_approvals SET status=?1,approved=?2,comment=?3,updated_at=?4,resolved_at=?5 WHERE token=?6`,
		approval.Status, approval.Approved, approval.Comment, approval.UpdatedAt, approval.ResolvedAt, approval.Token)
	return err
}

func (s *SQLiteStore) ListPendingApprovals(ctx context.Context) ([]model.HumanApproval, error) {
	rows, err := s.Conn.QueryContext(ctx,
		`SELECT task_run_id,token,status,approved,comment,timeout_seconds,created_at,updated_at,expires_at,resolved_at
		 FROM human_approvals WHERE status='PENDING' AND (expires_at IS NULL OR expires_at > datetime('now'))`)
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
	return approvals, nil
}

// --- Supervisor log ---

func (s *SQLiteStore) LogSupervisor(ctx context.Context, agentFlowRunID, taskRunID string, input, decision map[string]any) error {
	_, err := s.Conn.ExecContext(ctx,
		`INSERT INTO supervisor_log (id,agentflow_run_id,task_run_id,input_snapshot,decision) VALUES (?1,?2,?3,?4,?5)`,
		uuid.New().String(), agentFlowRunID, taskRunID, toJSON(input), toJSON(decision))
	return err
}

// --- Scanner helpers ---

type scanner interface{ Scan(...any) error }
type rowsScanner interface{ Scan(...any) error }

func scanAgentFlowVersion(s scanner) (*model.AgentFlowVersion, error) {
	var d model.AgentFlowVersion
	var b []byte
	if err := s.Scan(&d.AgentFlowID, &d.Version, &b, &d.CreatedBy, &d.Comment, &d.CreatedAt); err != nil {
		return nil, err
	}
	d.Definition = b
	return &d, nil
}

func scanAgentFlowVersionRow(r rowsScanner) (*model.AgentFlowVersion, error) {
	return scanAgentFlowVersion(r)
}

func scanAgentFlowRun(s scanner) (*model.AgentFlowRun, error) {
	var r model.AgentFlowRun
	var varsB, outB, triggerPayload []byte
	var errStr sql.NullString
	var startedAt, finishedAt sql.NullTime
	if err := s.Scan(&r.ID, &r.AgentFlowID, &r.Version, &r.Status, &varsB, &outB, &errStr, &r.Trigger.Type, &r.Trigger.Source, &triggerPayload, &r.CreatedAt, &r.UpdatedAt, &r.TenantID, &r.Namespace, &r.Priority, &startedAt, &finishedAt); err != nil {
		return nil, err
	}
	if errStr.Valid {
		r.Error = errStr.String
	}
	if varsB != nil {
		json.Unmarshal(varsB, &r.Vars)
	}
	if outB != nil {
		json.Unmarshal(outB, &r.Output)
	}
	if triggerPayload != nil {
		json.Unmarshal(triggerPayload, &r.Trigger.Payload)
	}
	if startedAt.Valid {
		r.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		r.FinishedAt = &finishedAt.Time
	}
	return &r, nil
}

func scanAgentFlowRunRow(r rowsScanner) (*model.AgentFlowRun, error) {
	return scanAgentFlowRun(r)
}

func scanTaskRun(s scanner) (*model.TaskRun, error) {
	var t model.TaskRun
	var inputB, outB []byte
	var startedAt, finishedAt sql.NullTime
	var parentID sql.NullString
	if err := s.Scan(&t.ID, &t.AgentFlowRunID, &t.NodeID, &t.Status, &inputB, &outB, &t.Error, &t.RetryCount, &t.MaxRetries, &t.ExecID, &parentID, &t.Sequence, &t.CreatedAt, &t.UpdatedAt, &startedAt, &finishedAt); err != nil {
		return nil, err
	}
	if inputB != nil {
		json.Unmarshal(inputB, &t.Input)
	}
	if outB != nil {
		json.Unmarshal(outB, &t.Output)
	}
	if parentID.Valid {
		t.ParentTaskRunID = parentID.String
	}
	if startedAt.Valid {
		t.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		t.FinishedAt = &finishedAt.Time
	}
	return &t, nil
}

func scanTaskRunRow(r rowsScanner) (*model.TaskRun, error) {
	return scanTaskRun(r)
}

func scanHumanApproval(s scanner) (*model.HumanApproval, error) {
	var a model.HumanApproval
	var approved sql.NullBool
	var comment sql.NullString
	var expiresAt, resolvedAt sql.NullTime
	var timeoutSecs int
	if err := s.Scan(&a.TaskRunID, &a.Token, &a.Status, &approved, &comment, &timeoutSecs, &a.CreatedAt, &a.UpdatedAt, &expiresAt, &resolvedAt); err != nil {
		return nil, err
	}
	if approved.Valid {
		a.Approved = &approved.Bool
	}
	if comment.Valid {
		a.Comment = comment.String
	}
	a.Timeout = time.Duration(timeoutSecs) * time.Second
	if expiresAt.Valid {
		a.ExpiresAt = &expiresAt.Time
	}
	if resolvedAt.Valid {
		a.ResolvedAt = &resolvedAt.Time
	}
	return &a, nil
}

func scanHumanApprovalRow(r rowsScanner) (*model.HumanApproval, error) {
	return scanHumanApproval(r)
}

// ExecutionPlan stubs (TODO: full implementation)
func (s *SQLiteStore) SavePlan(ctx context.Context, plan *model.ExecutionPlan) error {
	return nil
}
func (s *SQLiteStore) LoadPlan(ctx context.Context, planID string) (*model.ExecutionPlan, error) {
	return nil, nil
}
func (s *SQLiteStore) ListPlans(ctx context.Context, agentFlowRunID string) ([]*model.ExecutionPlan, error) {
	return nil, nil
}
func (s *SQLiteStore) SaveCheckpoint(ctx context.Context, planID string, cp *model.TaskCheckpoint) error {
	return nil
}
func (s *SQLiteStore) LoadCheckpoint(ctx context.Context, planID string) (*model.TaskCheckpoint, error) {
	return nil, nil
}
func (s *SQLiteStore) ClaimLease(ctx context.Context, planID, tmID string, dur time.Duration) error {
	return nil
}
func (s *SQLiteStore) ReleaseLease(ctx context.Context, planID string) error { return nil }

func (s *SQLiteStore) CancelFlowRun(ctx context.Context, id string) error {
	_, err := s.Conn.ExecContext(ctx, `UPDATE agentflow_runs SET status='CANCELLED' WHERE id=?`, id)
	return err
}
func (s *SQLiteStore) DeleteFlowRun(ctx context.Context, id string) error {
	_, err := s.Conn.ExecContext(ctx, `DELETE FROM agentflow_runs WHERE id=?`, id)
	return err
}
func (s *SQLiteStore) SaveNotificationChannel(ctx context.Context, ch *model.NotifierChannel) error {
	return nil
}
func (s *SQLiteStore) GetNotificationChannel(ctx context.Context, id string) (*model.NotifierChannel, error) {
	return nil, nil
}
func (s *SQLiteStore) ListNotificationChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error) {
	return nil, nil
}
func (s *SQLiteStore) DeleteNotificationChannel(ctx context.Context, id string) error { return nil }
func (s *SQLiteStore) SaveRoute(ctx context.Context, route *model.SubscriptionRoute) error {
	return nil
}
func (s *SQLiteStore) GetRoutesByFlow(ctx context.Context, id string) ([]model.SubscriptionRoute, error) {
	return nil, nil
}
func (s *SQLiteStore) DeleteRoute(ctx context.Context, id string) error { return nil }
func (s *SQLiteStore) DeleteRoutesByPod(ctx context.Context, podID string) error {
	return nil
}
func (s *SQLiteStore) CleanupOrphanedRoutes(ctx context.Context, podID string, maxAge time.Duration) (int64, error) {
	return 0, nil
}
func (s *SQLiteStore) DeleteAgent(ctx context.Context, name string) error {
	_, err := s.Conn.ExecContext(ctx, `DELETE FROM agents WHERE name=?`, name)
	return err
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
func (s *SQLiteStore) DeleteChannel(ctx context.Context, id string) error { return nil }

// ─── LLM providers ─────────────────────────────────────

var sqliteLlmProviders = struct {
	mu    sync.Mutex
	store map[string]*model.LlmProvider
}{store: make(map[string]*model.LlmProvider)}

func (s *SQLiteStore) SaveProvider(ctx context.Context, p *model.LlmProvider) error {
	sqliteLlmProviders.mu.Lock()
	defer sqliteLlmProviders.mu.Unlock()
	p.UpdatedAt = time.Now()
	sqliteLlmProviders.store[p.ID] = p
	return nil
}
func (s *SQLiteStore) GetProvider(ctx context.Context, id string) (*model.LlmProvider, error) {
	sqliteLlmProviders.mu.Lock()
	defer sqliteLlmProviders.mu.Unlock()
	return sqliteLlmProviders.store[id], nil
}
func (s *SQLiteStore) ListProviders(ctx context.Context, tenantID string) ([]model.LlmProvider, error) {
	sqliteLlmProviders.mu.Lock()
	defer sqliteLlmProviders.mu.Unlock()
	var out []model.LlmProvider
	for _, p := range sqliteLlmProviders.store {
		if tenantID == "" || p.TenantID == tenantID {
			out = append(out, *p)
		}
	}
	return out, nil
}
func (s *SQLiteStore) DeleteProvider(ctx context.Context, id string) error {
	sqliteLlmProviders.mu.Lock()
	defer sqliteLlmProviders.mu.Unlock()
	delete(sqliteLlmProviders.store, id)
	return nil
}

func (s *SQLiteStore) GetAgent(ctx context.Context, name string) (*model.AgentDef, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *SQLiteStore) ListAgents(ctx context.Context, tenantID string) ([]model.AgentDef, error) {
	return nil, nil
}

func (s *SQLiteStore) SaveChannel(ctx context.Context, ch *model.NotifierChannel) error {
	return nil
}
func (s *SQLiteStore) GetChannel(ctx context.Context, id string) (*model.NotifierChannel, error) {
	return nil, nil
}
func (s *SQLiteStore) ListChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error) {
	return nil, nil
}
func (s *SQLiteStore) SaveAgent(ctx context.Context, agent *model.AgentDef) error { return nil }
