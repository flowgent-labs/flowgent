package taskplan

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/google/uuid"
)

// TaskPlanSQLiteStore wraps store.SQLiteGenericStore[entities.TaskRunInfo].
type TaskPlanSQLiteStore struct {
	inner *store.SQLiteGenericStore[entities.TaskRunInfo]
}

func NewTaskPlanSQLiteStore(conn *sql.DB) *TaskPlanSQLiteStore {
	return &TaskPlanSQLiteStore{
		inner: &store.SQLiteGenericStore[entities.TaskRunInfo]{
			Conn: conn, Table: "task_runs", IDCol: "id",
		},
	}
}

func (s *TaskPlanSQLiteStore) Get(ctx context.Context, id string) (*entities.TaskRunInfo, error) {
	return s.inner.Get(ctx, id)
}
func (s *TaskPlanSQLiteStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.TaskRunInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *TaskPlanSQLiteStore) Save(ctx context.Context, e *entities.TaskRunInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *TaskPlanSQLiteStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

func (s *TaskPlanSQLiteStore) GetByExecID(ctx context.Context, execID string) (*entities.TaskRunInfo, error) {
	row := s.inner.Conn.QueryRowContext(ctx, "SELECT * FROM task_runs WHERE exec_id=?1", execID)
	return scanTaskRun(row)
}

func (s *TaskPlanSQLiteStore) CreateTaskRun(ctx context.Context, e *entities.TaskRunInfo) error {
	e.ID = uuid.New().String()
	now := time.Now().UTC()
	e.CreatedAt = now
	e.UpdatedAt = now
	return s.inner.Save(ctx, e)
}

func (s *TaskPlanSQLiteStore) UpdateTaskRun(ctx context.Context, e *entities.TaskRunInfo) error {
	output, err := json.Marshal(e.Output)
	if err != nil {
		return err
	}
	_, err = s.inner.Conn.ExecContext(ctx,
		`UPDATE task_runs SET status=?1, output=?2, error=?3, retry_count=?4, started_at=?5, finished_at=?6, updated_at=CURRENT_TIMESTAMP WHERE id=?7`,
		string(e.Status), output, e.Error, e.RetryCount, e.StartedAt, e.FinishedAt, e.ID)
	return err
}

func (s *TaskPlanSQLiteStore) ListByFlowRun(ctx context.Context, flowRunID string) ([]*entities.TaskRunInfo, error) {
	rows, err := s.inner.Conn.QueryContext(ctx, "SELECT * FROM task_runs WHERE agentflow_run_id=?1 ORDER BY sequence ASC", flowRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*entities.TaskRunInfo
	for rows.Next() {
		e, err := scanTaskRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scanTaskRun(s scanner) (*entities.TaskRunInfo, error) {
	var e entities.TaskRunInfo
	var inputStr, outputStr sql.NullString
	var startedAt, finishedAt sql.NullTime
	var createdAtStr, updatedAtStr string
	err := s.Scan(
		&e.ID, &e.AgentFlowRunID, &e.NodeID, &e.Status,
		&inputStr, &outputStr, &e.Error,
		&e.RetryCount, &e.MaxRetries, &e.ExecID,
		&e.ParentTaskRunID, &e.Sequence,
		&startedAt, &finishedAt,
		&e.Description, &e.TenantID,
		&createdAtStr, &e.CreatedBy, &updatedAtStr, &e.UpdatedBy, &e.DelFlag,
	)
	if err != nil {
		return nil, err
	}
	if inputStr.Valid {
		json.Unmarshal([]byte(inputStr.String), &e.Input)
	}
	if outputStr.Valid {
		json.Unmarshal([]byte(outputStr.String), &e.Output)
	}
	if startedAt.Valid {
		e.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		e.FinishedAt = &finishedAt.Time
	}
	// created_at/updated_at come back as TEXT (SQLite has no native
	// timestamp type) — database/sql cannot scan a string directly into
	// *time.Time, so parse it explicitly (mirrors utils.ScanStruct).
	if t, err := utils.ParseTime(createdAtStr); err == nil {
		e.CreatedAt = t
	}
	if t, err := utils.ParseTime(updatedAtStr); err == nil {
		e.UpdatedAt = t
	}
	return &e, nil
}
