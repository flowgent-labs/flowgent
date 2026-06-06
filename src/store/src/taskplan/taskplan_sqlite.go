package taskplan

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
)

// TaskPlanSQLiteStore wraps store.SQLiteGenericStore[model.TaskRun].
type TaskPlanSQLiteStore struct {
	inner *store.SQLiteGenericStore[model.TaskRun]
}

func NewTaskPlanSQLiteStore(conn *sql.DB) *TaskPlanSQLiteStore {
	return &TaskPlanSQLiteStore{
		inner: &store.SQLiteGenericStore[model.TaskRun]{
			Conn: conn, Table: "task_runs", IDCol: "id",
		},
	}
}

func (s *TaskPlanSQLiteStore) Get(ctx context.Context, id string) (*model.TaskRun, error) { return s.inner.Get(ctx, id) }
func (s *TaskPlanSQLiteStore) Select(ctx context.Context, page, pageSize int) (*model.Page[model.TaskRun], error) { return s.inner.Select(ctx, page, pageSize) }
func (s *TaskPlanSQLiteStore) Save(ctx context.Context, e *model.TaskRun) error { return s.inner.Save(ctx, e) }
func (s *TaskPlanSQLiteStore) Delete(ctx context.Context, id string) error { return s.inner.Delete(ctx, id) }

func (s *TaskPlanSQLiteStore) GetByExecID(ctx context.Context, execID string) (*model.TaskRun, error) {
	row := s.inner.Conn.QueryRowContext(ctx, "SELECT * FROM task_runs WHERE exec_id=?1", execID)
	return scanTaskRun(row)
}

func (s *TaskPlanSQLiteStore) CreateTaskRun(ctx context.Context, e *model.TaskRun) error {
	e.ID = uuid.New().String()
	now := time.Now().UTC()
	e.CreatedAt = now
	e.UpdatedAt = now
	return s.inner.Save(ctx, e)
}

func (s *TaskPlanSQLiteStore) UpdateTaskRun(ctx context.Context, e *model.TaskRun) error {
	_, err := s.inner.Conn.ExecContext(ctx,
		`UPDATE task_runs SET status=?1, output=?2, error=?3, retry_count=?4, started_at=?5, finished_at=?6, updated_at=CURRENT_TIMESTAMP WHERE id=?7`,
		string(e.Status), e.Output, e.Error, e.RetryCount, e.StartedAt, e.FinishedAt, e.ID)
	return err
}

func (s *TaskPlanSQLiteStore) ListByFlowRun(ctx context.Context, flowRunID string) ([]*model.TaskRun, error) {
	rows, err := s.inner.Conn.QueryContext(ctx, "SELECT * FROM task_runs WHERE agentflow_run_id=?1 ORDER BY sequence ASC", flowRunID)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []*model.TaskRun
	for rows.Next() {
		e, err := scanTaskRun(rows)
		if err != nil { return nil, err }
		out = append(out, e)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scanTaskRun(s scanner) (*model.TaskRun, error) {
	var e model.TaskRun
	var inputStr, outputStr sql.NullString
	var startedAt, finishedAt sql.NullTime
	err := s.Scan(
		&e.ID, &e.AgentFlowRunID, &e.NodeID, &e.Status,
		&inputStr, &outputStr, &e.Error,
		&e.RetryCount, &e.MaxRetries, &e.ExecID,
		&e.ParentTaskRunID, &e.Sequence,
		&e.CreatedAt, &e.UpdatedAt,
		&startedAt, &finishedAt,
	)
	if err != nil { return nil, err }
	if inputStr.Valid { json.Unmarshal([]byte(inputStr.String), &e.Input) }
	if outputStr.Valid { json.Unmarshal([]byte(outputStr.String), &e.Output) }
	if startedAt.Valid { e.StartedAt = &startedAt.Time }
	if finishedAt.Valid { e.FinishedAt = &finishedAt.Time }
	return &e, nil
}
