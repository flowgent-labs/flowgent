package task

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TaskPostgresStore struct{ pool *pgxpool.Pool }

func NewTaskPostgresStore(pool *pgxpool.Pool) *TaskPostgresStore {
	return &TaskPostgresStore{pool: pool}
}
func (s *TaskPostgresStore) scan(row interface{ Scan(...any) error }) (*entities.TaskRunInfo, error) {
	var record nodeRecord
	if err := utils.ScanStruct(row, &record); err != nil {
		return nil, err
	}
	return record.entity(), nil
}
func (s *TaskPostgresStore) Get(ctx context.Context, id string) (*entities.TaskRunInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_node_run").PostgresWhere(2)
	args := append([]any{id}, scopeArgs...)
	return s.scan(s.pool.QueryRow(ctx, `SELECT `+utils.Columns[nodeRecord]()+` FROM orh_node_run WHERE id=$1 AND status<>'DELETED' AND (`+scopeWhere+`)`, args...))
}
func (s *TaskPostgresStore) GetByExecID(ctx context.Context, executionID string) (*entities.TaskRunInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_node_run").PostgresWhere(2)
	args := append([]any{executionID}, scopeArgs...)
	return s.scan(s.pool.QueryRow(ctx, `SELECT `+utils.Columns[nodeRecord]()+` FROM orh_node_run WHERE execution_id=$1 AND status<>'DELETED' AND (`+scopeWhere+`)`, args...))
}
func (s *TaskPostgresStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.TaskRunInfo], error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 {
		req.Size = 20
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_node_run").PostgresWhere(1)
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM orh_node_run WHERE status<>'DELETED' AND (`+scopeWhere+`)`, scopeArgs...).Scan(&total); err != nil {
		return nil, err
	}
	pos := len(scopeArgs) + 1
	args := append(scopeArgs, req.Size, (req.Page-1)*req.Size)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT %s FROM orh_node_run WHERE status<>'DELETED' AND (%s) ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, utils.Columns[nodeRecord](), scopeWhere, pos, pos+1), args...)
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
func (s *TaskPostgresStore) Save(ctx context.Context, item *entities.TaskRunInfo) error {
	return s.UpdateTaskRun(ctx, item)
}
func (s *TaskPostgresStore) CreateTaskRun(ctx context.Context, item *entities.TaskRunInfo) error {
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	return s.UpdateTaskRun(ctx, item)
}
func (s *TaskPostgresStore) UpdateTaskRun(ctx context.Context, item *entities.TaskRunInfo) error {
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
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var namespace string
	if err = tx.QueryRow(ctx, `SELECT namespace_id FROM orh_run WHERE id=$1`, item.RunID).Scan(&namespace); err != nil {
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
	result, err := tx.Exec(ctx, `INSERT INTO orh_node_run(id,namespace_id,run_id,node_key,attempt,agent_revision_id,status,input,output,error,execution_memory,checkpoint,workspace_version,parent_node_run_id,execution_id,lease_owner,lease_expires_at,fencing_token,last_heartbeat_at,started_at,finished_at,description,created_by,updated_by,metadata) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,$12,NULLIF($13,''),NULLIF($14,''),$15,NULLIF($16,''),$17,$18,$19,$20,$21,$22,$23,$23,$24) ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status,input=COALESCE(EXCLUDED.input,orh_node_run.input),output=COALESCE(EXCLUDED.output,orh_node_run.output),error=EXCLUDED.error,execution_memory=COALESCE(EXCLUDED.execution_memory,orh_node_run.execution_memory),checkpoint=COALESCE(EXCLUDED.checkpoint,orh_node_run.checkpoint),workspace_version=COALESCE(EXCLUDED.workspace_version,orh_node_run.workspace_version),lease_owner=EXCLUDED.lease_owner,lease_expires_at=EXCLUDED.lease_expires_at,fencing_token=EXCLUDED.fencing_token,last_heartbeat_at=EXCLUDED.last_heartbeat_at,started_at=COALESCE(EXCLUDED.started_at,orh_node_run.started_at),finished_at=EXCLUDED.finished_at,metadata=EXCLUDED.metadata WHERE orh_node_run.run_id=EXCLUDED.run_id AND orh_node_run.node_key=EXCLUDED.node_key AND orh_node_run.attempt=EXCLUDED.attempt AND orh_node_run.fencing_token<=EXCLUDED.fencing_token`, item.ID, namespace, item.RunID, item.NodeKey, item.Attempt, item.AgentRevisionID, item.Status, taskJSON(item.Input), taskJSON(item.Output), taskError(item.Error), taskJSON(item.ExecutionMemory), taskJSON(item.Checkpoint), workspace, item.ParentNodeRunID, item.ExecutionID, item.LeaseOwner, item.LeaseExpiresAt, item.FencingToken, item.LastHeartbeatAt, item.StartedAt, item.FinishedAt, item.Description, item.UpdatedBy, taskJSON(metadata))
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("stale node-run fencing token")
	}
	if item.Checkpoint != nil {
		if err = appendCheckpointPG(ctx, tx, item, workspace); err != nil {
			return err
		}
	}
	if scope.Where != "1=1" {
		where, args0 := scope.PostgresWhere(2)
		args := append([]any{item.ID}, args0...)
		var visible int
		if err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM orh_node_run WHERE id=$1 AND (`+where+`)`, args...).Scan(&visible); err != nil {
			return err
		}
		if visible != 1 {
			return storage.ErrFlowgentSqlScopeDenied
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	item.CreatedAt = time.Now().UTC()
	item.UpdatedAt = item.CreatedAt
	item.RowVersion = 1
	return nil
}
func appendCheckpointPG(ctx context.Context, tx pgx.Tx, item *entities.TaskRunInfo, workspace string) error {
	payload := taskJSON(item.Checkpoint)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orh_node_checkpoint WHERE node_run_id=$1 AND checkpoint=$2::jsonb AND workspace_version=$3 AND fencing_token=$4)`, item.ID, payload, workspace, item.FencingToken).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM orh_node_checkpoint WHERE node_run_id=$1`, item.ID).Scan(&sequence); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO orh_node_checkpoint(id,namespace_id,node_run_id,sequence,execution_memory,checkpoint,workspace_version,fencing_token,created_by,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, uuid.NewString(), item.Namespace, item.ID, sequence, taskJSON(item.ExecutionMemory), payload, workspace, item.FencingToken, item.UpdatedBy, taskJSON(item.Metadata))
	return err
}
func (s *TaskPostgresStore) ListByFlowRun(ctx context.Context, runID string) ([]*entities.TaskRunInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_node_run").PostgresWhere(2)
	args := append([]any{runID}, scopeArgs...)
	rows, err := s.pool.Query(ctx, `SELECT `+utils.Columns[nodeRecord]()+` FROM orh_node_run WHERE run_id=$1 AND status<>'DELETED' AND (`+scopeWhere+`) ORDER BY node_key,attempt`, args...)
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
func (s *TaskPostgresStore) Delete(ctx context.Context, id string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_node_run").PostgresWhere(2)
	args := append([]any{id}, scopeArgs...)
	result, err := s.pool.Exec(ctx, `UPDATE orh_node_run SET status='DELETED' WHERE id=$1 AND (`+scopeWhere+`)`, args...)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("node run not found or outside authorization scope")
	}
	return nil
}

var _ = json.Valid
