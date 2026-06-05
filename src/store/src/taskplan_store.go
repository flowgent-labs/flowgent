package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/flowgent-labs/flowgent/model/src"
)

// ITaskPlanStore manages task runs, execution plans, checkpoints, leases, and supervisor logs.
type ITaskPlanStore interface {
	CreateTaskRun(ctx context.Context, task *model.TaskRun) error
	UpdateTaskRun(ctx context.Context, task *model.TaskRun) error
	GetTaskRun(ctx context.Context, id string) (*model.TaskRun, error)
	ListTaskRunsByFlow(ctx context.Context, runID string) ([]model.TaskRun, error)
	GetTaskRunByExecID(ctx context.Context, execID string) (*model.TaskRun, error)
	SavePlan(ctx context.Context, plan *model.ExecutionPlan) error
	LoadPlan(ctx context.Context, id string) (*model.ExecutionPlan, error)
	ListPlans(ctx context.Context, runID string) ([]*model.ExecutionPlan, error)
	SaveCheckpoint(ctx context.Context, planID string, cp *model.TaskCheckpoint) error
	LoadCheckpoint(ctx context.Context, planID string) (*model.TaskCheckpoint, error)
	ClaimLease(ctx context.Context, planID, tmID string, dur time.Duration) error
	ReleaseLease(ctx context.Context, planID string) error
	LogSupervisor(ctx context.Context, agentFlowRunID, taskRunID string, input, decision map[string]any) error
}

// ─── PostgresStore methods — taskplan ─────────────────────────

func (s *PostgresStore) CreateTaskRun(ctx context.Context, task *model.TaskRun) error {
	task.ID = newUUID()
	task.CreatedAt = time.Now()
	task.UpdatedAt = task.CreatedAt
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO task_runs (id,agentflow_run_id,node_id,status,input,output,error,retry_count,max_retries,exec_id,parent_task_run_id,sequence,created_at,updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		task.ID, task.AgentFlowRunID, task.NodeID, task.Status, toJSON(task.Input), toJSON(task.Output), task.Error,
		task.RetryCount, task.MaxRetries, task.ExecID, task.ParentTaskRunID, task.Sequence, task.CreatedAt, task.UpdatedAt)
	return err
}

func (s *PostgresStore) UpdateTaskRun(ctx context.Context, task *model.TaskRun) error {
	task.UpdatedAt = time.Now()
	_, err := s.Pool.Exec(ctx,
		`UPDATE task_runs SET status=$1,input=$2,output=$3,error=$4,retry_count=$5,max_retries=$6,exec_id=$7,parent_task_run_id=$8,sequence=$9,updated_at=$10,started_at=$11,finished_at=$12 WHERE id=$13`,
		task.Status, toJSON(task.Input), toJSON(task.Output), task.Error, task.RetryCount, task.MaxRetries, task.ExecID, task.ParentTaskRunID, task.Sequence, task.UpdatedAt, task.StartedAt, task.FinishedAt, task.ID)
	return err
}

func (s *PostgresStore) GetTaskRun(ctx context.Context, id string) (*model.TaskRun, error) {
	row := s.Pool.QueryRow(ctx, `SELECT id,agentflow_run_id,node_id,status,input,output,error,retry_count,max_retries,exec_id,parent_task_run_id,sequence,created_at,updated_at,started_at,finished_at FROM task_runs WHERE id=$1`, id)
	return scanTaskRun(row)
}

func (s *PostgresStore) ListTaskRunsByFlow(ctx context.Context, runID string) ([]model.TaskRun, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id,agentflow_run_id,node_id,status,input,output,error,retry_count,max_retries,exec_id,parent_task_run_id,sequence,created_at,updated_at,started_at,finished_at FROM task_runs WHERE agentflow_run_id=$1 ORDER BY created_at`, runID)
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
	row := s.Pool.QueryRow(ctx, `SELECT id,agentflow_run_id,node_id,status,input,output,error,retry_count,max_retries,exec_id,parent_task_run_id,sequence,created_at,updated_at,started_at,finished_at FROM task_runs WHERE exec_id=$1`, execID)
	return scanTaskRun(row)
}

func (s *PostgresStore) SavePlan(ctx context.Context, plan *model.ExecutionPlan) error {
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
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO task_runs (id,agentflow_run_id,node_id,status,input,output,error,retry_count,max_retries,exec_id,created_at,updated_at,started_at,finished_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		 ON CONFLICT (id) DO UPDATE SET status=$4,input=$5,output=$6,error=$7,retry_count=$8,updated_at=$12,started_at=$13,finished_at=$14`,
		plan.TaskID, plan.AgentFlowRunID, plan.NodeID, string(plan.State), toJSON(plan.Input), toJSON(output), errStr,
		plan.RetryCount, plan.MaxRetries, plan.PlanID, plan.CreatedAt, now, plan.StartedAt, plan.FinishedAt)
	return err
}

func (s *PostgresStore) LoadPlan(ctx context.Context, id string) (*model.ExecutionPlan, error) {
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

func (s *PostgresStore) ListPlans(ctx context.Context, runID string) ([]*model.ExecutionPlan, error) {
	tasks, err := s.ListTaskRunsByFlow(ctx, runID)
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

func (s *PostgresStore) SaveCheckpoint(ctx context.Context, planID string, cp *model.TaskCheckpoint) error {
	return nil
}

func (s *PostgresStore) LoadCheckpoint(ctx context.Context, planID string) (*model.TaskCheckpoint, error) {
	return nil, nil
}

func (s *PostgresStore) ClaimLease(ctx context.Context, planID, tmID string, dur time.Duration) error {
	return nil
}

func (s *PostgresStore) ReleaseLease(ctx context.Context, planID string) error { return nil }

func (s *PostgresStore) LogSupervisor(ctx context.Context, agentFlowRunID, taskRunID string, input, decision map[string]any) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO supervisor_log (agentflow_run_id,task_run_id,input_snapshot,decision) VALUES ($1,$2,$3,$4)`,
		agentFlowRunID, taskRunID, toJSON(input), toJSON(decision))
	return err
}

// ─── SQLiteStore methods — taskplan ───────────────────────────

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

func (s *SQLiteStore) LogSupervisor(ctx context.Context, agentFlowRunID, taskRunID string, input, decision map[string]any) error {
	_, err := s.Conn.ExecContext(ctx,
		`INSERT INTO supervisor_log (id,agentflow_run_id,task_run_id,input_snapshot,decision) VALUES (?1,?2,?3,?4,?5)`,
		uuid.New().String(), agentFlowRunID, taskRunID, toJSON(input), toJSON(decision))
	return err
}
