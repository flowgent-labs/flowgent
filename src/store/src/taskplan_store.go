package store

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/model/src"
)

// ITaskPlanStore manages TaskRuns, ExecutionPlans, Checkpoints, Leases, and Supervisor logs.
type ITaskPlanStore interface {
	CreateTaskRun(ctx context.Context, task *model.TaskRun) error
	UpdateTaskRun(ctx context.Context, task *model.TaskRun) error
	GetTaskRun(ctx context.Context, id string) (*model.TaskRun, error)
	ListTaskRunsByFlow(ctx context.Context, flowRunID string) ([]model.TaskRun, error)
	GetTaskRunByExecID(ctx context.Context, execID string) (*model.TaskRun, error)

	SavePlan(ctx context.Context, plan *model.ExecutionPlan) error
	LoadPlan(ctx context.Context, planID string) (*model.ExecutionPlan, error)
	ListPlans(ctx context.Context, flowRunID string) ([]*model.ExecutionPlan, error)

	SaveCheckpoint(ctx context.Context, planID string, cp *model.TaskCheckpoint) error
	LoadCheckpoint(ctx context.Context, planID string) (*model.TaskCheckpoint, error)

	ClaimLease(ctx context.Context, planID, tmID string, dur time.Duration) error
	ReleaseLease(ctx context.Context, planID string) error

	LogSupervisor(ctx context.Context, flowRunID, taskRunID string, input, decision map[string]any) error
}
