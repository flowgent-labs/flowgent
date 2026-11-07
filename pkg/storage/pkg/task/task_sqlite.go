package task

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
)

type TaskSQLiteStore struct{ conn *sql.DB }

func NewTaskSQLiteStore(conn *sql.DB) *TaskSQLiteStore { return &TaskSQLiteStore{conn: conn} }
func (s *TaskSQLiteStore) scan(row interface{ Scan(...any) error }) (*entities.TaskRunInfo, error) {
	var record nodeRecord
	if err := utils.ScanStruct(row, &record); err != nil {
		return nil, err
	}
	return record.entity(), nil
}
func (s *TaskSQLiteStore) Get(ctx context.Context, id string) (*entities.TaskRunInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_node_run").SQLiteWhere()
	args := append([]any{id}, scopeArgs...)
	return s.scan(s.conn.QueryRowContext(ctx, `SELECT `+utils.Columns[nodeRecord]()+` FROM orh_node_run WHERE id=?1 AND status<>'DELETED' AND (`+scopeWhere+`)`, args...))
}
func (s *TaskSQLiteStore) GetByExecID(ctx context.Context, executionID string) (*entities.TaskRunInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_node_run").SQLiteWhere()
	args := append([]any{executionID}, scopeArgs...)
	return s.scan(s.conn.QueryRowContext(ctx, `SELECT `+utils.Columns[nodeRecord]()+` FROM orh_node_run WHERE execution_id=?1 AND status<>'DELETED' AND (`+scopeWhere+`)`, args...))
}
func (s *TaskSQLiteStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.TaskRunInfo], error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 {
		req.Size = 20
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_node_run").SQLiteWhere()
	var total int64
	if err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM orh_node_run WHERE status<>'DELETED' AND (`+scopeWhere+`)`, scopeArgs...).Scan(&total); err != nil {
		return nil, err
	}
	args := append(scopeArgs, req.Size, (req.Page-1)*req.Size)
	rows, err := s.conn.QueryContext(ctx, `SELECT `+utils.Columns[nodeRecord]()+` FROM orh_node_run WHERE status<>'DELETED' AND (`+scopeWhere+`) ORDER BY created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.TaskRunInfo, 0)
	for rows.Next() {
		item, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return entities.NewPage(items, total, req), rows.Err()
}
func (s *TaskSQLiteStore) Save(ctx context.Context, item *entities.TaskRunInfo) error {
	return s.UpdateTaskRun(ctx, item)
}
func (s *TaskSQLiteStore) CreateTaskRun(ctx context.Context, item *entities.TaskRunInfo) error {
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	return s.UpdateTaskRun(ctx, item)
}
func (s *TaskSQLiteStore) UpdateTaskRun(ctx context.Context, item *entities.TaskRunInfo) error {
	item.NormalizeAliases()
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if item.ExecutionID == "" {
		item.ExecutionID = item.ID
	}
	if item.Status == "" {
		item.Status = entities.TaskPending
	}
	scope := storage.FlowgentSqlScopeForTable(ctx, "orh_node_run")
	if scope.Where == "0=1" {
		return storage.ErrFlowgentSqlScopeDenied
	}
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var namespace string
	if err = tx.QueryRowContext(ctx, `SELECT namespace_id FROM orh_run WHERE id=?`, item.RunID).Scan(&namespace); err != nil {
		return fmt.Errorf("resolve run namespace: %w", err)
	}
	if item.Namespace != "" && item.Namespace != namespace {
		return fmt.Errorf("node run namespace mismatch")
	}
	item.Namespace = namespace
	metadata := map[string]any{}
	for key, value := range item.Metadata {
		metadata[key] = value
	}
	metadata["max_retries"] = item.MaxRetries
	metadata["sequence"] = item.Sequence
	workspace := item.WorkspaceVersion
	if item.Checkpoint != nil && workspace == "" {
		workspace = "execution:" + item.ExecutionID
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO orh_node_run(id,namespace_id,run_id,node_key,attempt,agent_revision_id,status,input,output,error,execution_memory,checkpoint,workspace_version,parent_node_run_id,execution_id,lease_owner,lease_expires_at,fencing_token,last_heartbeat_at,started_at,finished_at,description,created_by,updated_by,metadata) VALUES(?,?,?,?,?,NULLIF(?,''),?,?,?,?,?,?,NULLIF(?,''),NULLIF(?,''),?,NULLIF(?,''),?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,input=COALESCE(excluded.input,orh_node_run.input),output=COALESCE(excluded.output,orh_node_run.output),error=excluded.error,execution_memory=COALESCE(excluded.execution_memory,orh_node_run.execution_memory),checkpoint=COALESCE(excluded.checkpoint,orh_node_run.checkpoint),workspace_version=COALESCE(excluded.workspace_version,orh_node_run.workspace_version),lease_owner=excluded.lease_owner,lease_expires_at=excluded.lease_expires_at,fencing_token=excluded.fencing_token,last_heartbeat_at=excluded.last_heartbeat_at,started_at=COALESCE(excluded.started_at,orh_node_run.started_at),finished_at=excluded.finished_at,metadata=excluded.metadata WHERE orh_node_run.run_id=excluded.run_id AND orh_node_run.node_key=excluded.node_key AND orh_node_run.attempt=excluded.attempt AND orh_node_run.fencing_token<=excluded.fencing_token`, item.ID, namespace, item.RunID, item.NodeKey, item.Attempt, item.AgentRevisionID, item.Status, taskString(taskJSON(item.Input)), taskString(taskJSON(item.Output)), taskString(taskError(item.Error)), taskString(taskJSON(item.ExecutionMemory)), taskString(taskJSON(item.Checkpoint)), workspace, item.ParentNodeRunID, item.ExecutionID, item.LeaseOwner, item.LeaseExpiresAt, item.FencingToken, item.LastHeartbeatAt, item.StartedAt, item.FinishedAt, item.Description, item.UpdatedBy, item.UpdatedBy, taskString(taskJSON(metadata)))
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("stale node-run fencing token")
	}
	if item.Checkpoint != nil {
		if err = appendCheckpointSQLite(ctx, tx, item, workspace); err != nil {
			return err
		}
	}
	if scope.Where != "1=1" {
		where, args0 := scope.SQLiteWhere()
		args := append([]any{item.ID}, args0...)
		var visible int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM orh_node_run WHERE id=? AND (`+where+`)`, args...).Scan(&visible); err != nil {
			return err
		}
		if visible != 1 {
			return storage.ErrFlowgentSqlScopeDenied
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	item.CreatedAt = time.Now().UTC()
	item.UpdatedAt = item.CreatedAt
	item.RowVersion = 1
	return nil
}
func taskString(value []byte) any {
	if value == nil {
		return nil
	}
	return string(value)
}
func appendCheckpointSQLite(ctx context.Context, tx *sql.Tx, item *entities.TaskRunInfo, workspace string) error {
	payload := string(taskJSON(item.Checkpoint))
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM orh_node_checkpoint WHERE node_run_id=? AND json(checkpoint)=json(?) AND workspace_version=? AND fencing_token=?)`, item.ID, payload, workspace, item.FencingToken).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	var sequence int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM orh_node_checkpoint WHERE node_run_id=?`, item.ID).Scan(&sequence); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO orh_node_checkpoint(id,namespace_id,node_run_id,sequence,execution_memory,checkpoint,workspace_version,fencing_token,created_by,metadata) VALUES(?,?,?,?,?,?,?,?,?,?)`, uuid.NewString(), item.Namespace, item.ID, sequence, taskString(taskJSON(item.ExecutionMemory)), payload, workspace, item.FencingToken, item.UpdatedBy, taskString(taskJSON(item.Metadata)))
	return err
}
func (s *TaskSQLiteStore) ListByFlowRun(ctx context.Context, runID string) ([]*entities.TaskRunInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_node_run").SQLiteWhere()
	args := append([]any{runID}, scopeArgs...)
	rows, err := s.conn.QueryContext(ctx, `SELECT `+utils.Columns[nodeRecord]()+` FROM orh_node_run WHERE run_id=?1 AND status<>'DELETED' AND (`+scopeWhere+`) ORDER BY node_key,attempt`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.TaskRunInfo, 0)
	for rows.Next() {
		item, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *TaskSQLiteStore) Delete(ctx context.Context, id string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_node_run").SQLiteWhere()
	args := append([]any{id}, scopeArgs...)
	result, err := s.conn.ExecContext(ctx, `UPDATE orh_node_run SET status='DELETED' WHERE id=?1 AND (`+scopeWhere+`)`, args...)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("node run not found or outside authorization scope")
	}
	return nil
}
