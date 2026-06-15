package taskplan

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// PlanToTaskRun converts an ExecutionPlan to a TaskRunInfo for persistence.
// ExecutionPlans are the runtime unit of work; TaskRunInfo is the persisted entity.
// The conversion maps PlanID → ExecID, TaskID → ID.
func PlanToTaskRun(plan *entities.ExecutionPlan) *entities.TaskRunInfo {
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
	return &entities.TaskRunInfo{
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
	Get(ctx context.Context, id string) (*entities.TaskRunInfo, error)
	Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.TaskRunInfo], error)
	Save(ctx context.Context, entity *entities.TaskRunInfo) error
	Delete(ctx context.Context, id string) error
	GetByExecID(ctx context.Context, execID string) (*entities.TaskRunInfo, error)
	CreateTaskRun(ctx context.Context, entity *entities.TaskRunInfo) error
	UpdateTaskRun(ctx context.Context, entity *entities.TaskRunInfo) error
	ListByFlowRun(ctx context.Context, flowRunID string) ([]*entities.TaskRunInfo, error)
}
