package taskplan

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/model/src"
)

// PlanToTaskRun converts an ExecutionPlan to a TaskRun for persistence.
// ExecutionPlans are the runtime unit of work; TaskRun is the persisted entity.
// The conversion maps PlanID → ExecID, TaskID → ID.
func PlanToTaskRun(plan *model.ExecutionPlan) *model.TaskRun {
	output := map[string]any{}
	errStr := ""
	if plan.Result != nil {
		output = plan.Result.Output
		errStr = plan.Result.Error
	}
	now := time.Now()
	createdAt := plan.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	return &model.TaskRun{
		ID:             plan.TaskID,
		AgentFlowRunID: plan.AgentFlowRunID,
		NodeID:         plan.NodeID,
		Status:         plan.State,
		Input:          plan.Input,
		Output:         output,
		Error:          errStr,
		RetryCount:     plan.RetryCount,
		MaxRetries:     plan.MaxRetries,
		ExecID:         plan.PlanID,
		CreatedAt:      createdAt,
		UpdatedAt:      now,
		StartedAt:      plan.StartedAt,
		FinishedAt:     plan.FinishedAt,
	}
}

// ITaskPlanStore is the task run entity store interface.
type ITaskPlanStore interface {
	Get(ctx context.Context, id string) (*model.TaskRun, error)
	Select(ctx context.Context, req model.PageRequest) (*model.Page[model.TaskRun], error)
	Save(ctx context.Context, entity *model.TaskRun) error
	Delete(ctx context.Context, id string) error
	GetByExecID(ctx context.Context, execID string) (*model.TaskRun, error)
	CreateTaskRun(ctx context.Context, entity *model.TaskRun) error
	UpdateTaskRun(ctx context.Context, entity *model.TaskRun) error
	ListByFlowRun(ctx context.Context, flowRunID string) ([]*model.TaskRun, error)
}
