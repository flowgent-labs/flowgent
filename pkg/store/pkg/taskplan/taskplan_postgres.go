package taskplan

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TaskPlanPostgresStore wraps store.PostgresGenericStore[entities.TaskRunInfo].
type TaskPlanPostgresStore struct {
	inner *store.PostgresGenericStore[entities.TaskRunInfo]
}

func NewTaskPlanPostgresStore(pool *pgxpool.Pool) *TaskPlanPostgresStore {
	return &TaskPlanPostgresStore{
		inner: &store.PostgresGenericStore[entities.TaskRunInfo]{
			Pool: pool, Table: "task_runs", IDCol: "id",
		},
	}
}

func (s *TaskPlanPostgresStore) Get(ctx context.Context, id string) (*entities.TaskRunInfo, error) {
	return s.inner.Get(ctx, id)
}
func (s *TaskPlanPostgresStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.TaskRunInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *TaskPlanPostgresStore) Save(ctx context.Context, e *entities.TaskRunInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *TaskPlanPostgresStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

func (s *TaskPlanPostgresStore) GetByExecID(ctx context.Context, execID string) (*entities.TaskRunInfo, error) {
	rows, err := s.inner.Pool.Query(ctx, "SELECT * FROM task_runs WHERE exec_id=$1", execID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[entities.TaskRunInfo])
}

func (s *TaskPlanPostgresStore) CreateTaskRun(ctx context.Context, e *entities.TaskRunInfo) error {
	e.ID = uuid.New().String()
	now := time.Now().UTC()
	e.CreatedAt = now
	e.UpdatedAt = now
	return s.inner.Save(ctx, e)
}

func (s *TaskPlanPostgresStore) UpdateTaskRun(ctx context.Context, e *entities.TaskRunInfo) error {
	_, err := s.inner.Pool.Exec(ctx,
		`UPDATE task_runs SET status=$1, output=$2, error=$3, retry_count=$4, started_at=$5, finished_at=$6, updated_at=NOW() WHERE id=$7`,
		e.Status, e.Output, e.Error, e.RetryCount, e.StartedAt, e.FinishedAt, e.ID)
	return err
}

func (s *TaskPlanPostgresStore) ListByFlowRun(ctx context.Context, flowRunID string) ([]*entities.TaskRunInfo, error) {
	rows, err := s.inner.Pool.Query(ctx, "SELECT * FROM task_runs WHERE agentflow_run_id=$1 ORDER BY sequence ASC", flowRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[entities.TaskRunInfo])
}
