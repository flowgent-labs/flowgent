package task

import (
	"context"
	"encoding/json"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TaskPostgresStore wraps store.PostgresGenericStore[entities.TaskRunInfo].
type TaskPostgresStore struct {
	inner *store.PostgresGenericStore[entities.TaskRunInfo]
}

func NewTaskPostgresStore(pool *pgxpool.Pool) *TaskPostgresStore {
	return &TaskPostgresStore{
		inner: &store.PostgresGenericStore[entities.TaskRunInfo]{
			Pool: pool, Table: "task_runs", IDCol: "id",
		},
	}
}

func (s *TaskPostgresStore) Get(ctx context.Context, id string) (*entities.TaskRunInfo, error) {
	return s.inner.Get(ctx, id)
}
func (s *TaskPostgresStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.TaskRunInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *TaskPostgresStore) Save(ctx context.Context, e *entities.TaskRunInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *TaskPostgresStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

func (s *TaskPostgresStore) GetByExecID(ctx context.Context, execID string) (*entities.TaskRunInfo, error) {
	rows, err := s.inner.Pool.Query(ctx, "SELECT "+utils.Columns[entities.TaskRunInfo]()+" FROM task_runs WHERE exec_id=$1", execID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, pgx.ErrNoRows
	}
	var out entities.TaskRunInfo
	if err := utils.ScanStruct(rows, &out); err != nil {
		return nil, err
	}
	return &out, rows.Err()
}

func (s *TaskPostgresStore) CreateTaskRun(ctx context.Context, e *entities.TaskRunInfo) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	e.CreatedAt = now
	e.UpdatedAt = now
	return s.inner.Save(ctx, e)
}

func (s *TaskPostgresStore) UpdateTaskRun(ctx context.Context, e *entities.TaskRunInfo) error {
	output, err := json.Marshal(e.Output)
	if err != nil {
		return err
	}
	input, err := json.Marshal(e.Input)
	if err != nil {
		return err
	}
	_, err = s.inner.Pool.Exec(ctx,
		`INSERT INTO task_runs (id, agentflow_run_id, node_id, status, input, output, error, retry_count, max_retries, exec_id, parent_task_run_id, sequence, started_at, finished_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 ON CONFLICT (id) DO UPDATE SET
		     status            = EXCLUDED.status,
		     input             = COALESCE(EXCLUDED.input, task_runs.input),
		     output            = COALESCE(EXCLUDED.output, task_runs.output),
		     error             = COALESCE(EXCLUDED.error, task_runs.error),
		     retry_count       = EXCLUDED.retry_count,
		     max_retries       = COALESCE(EXCLUDED.max_retries, task_runs.max_retries),
		     exec_id           = COALESCE(EXCLUDED.exec_id, task_runs.exec_id),
		     parent_task_run_id = COALESCE(EXCLUDED.parent_task_run_id, task_runs.parent_task_run_id),
		     sequence          = EXCLUDED.sequence,
		     started_at        = COALESCE(EXCLUDED.started_at, task_runs.started_at),
		     finished_at       = COALESCE(EXCLUDED.finished_at, task_runs.finished_at),
		     updated_at        = NOW()`,
		e.ID, e.AgentFlowRunID, e.NodeID, e.Status, input, output, e.Error, e.RetryCount, e.MaxRetries, e.ExecID, e.ParentTaskRunID, e.Sequence, e.StartedAt, e.FinishedAt)
	return err
}

func (s *TaskPostgresStore) ListByFlowRun(ctx context.Context, flowRunID string) ([]*entities.TaskRunInfo, error) {
	rows, err := s.inner.Pool.Query(ctx, "SELECT "+utils.Columns[entities.TaskRunInfo]()+" FROM task_runs WHERE agentflow_run_id=$1 ORDER BY sequence ASC", flowRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*entities.TaskRunInfo
	for rows.Next() {
		task := &entities.TaskRunInfo{}
		if err := utils.ScanStruct(rows, task); err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}
