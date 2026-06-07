package executor

import (
	"context"
	"fmt"

	"github.com/flowgent-labs/flowgent/model/src"
)

// TaskExecutor executes a single ExecutionPlan. Each task type has its
// own implementation, registered in the TaskExecutorRouter.
type TaskExecutor interface {
	TaskType() model.TaskType
	Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error)
}

// TaskExecutorRouter dispatches an ExecutionPlan to the correct TaskExecutor by plan.TaskType.
type TaskExecutorRouter struct {
	executors map[model.TaskType]TaskExecutor
}

func NewTaskExecutorRouter() *TaskExecutorRouter {
	return &TaskExecutorRouter{executors: make(map[model.TaskType]TaskExecutor)}
}

func (r *TaskExecutorRouter) Register(exec TaskExecutor) {
	r.executors[exec.TaskType()] = exec
}

func (r *TaskExecutorRouter) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	exec, ok := r.executors[plan.TaskType]
	if !ok {
		return nil, fmt.Errorf("no executor registered for task type %q", plan.TaskType)
	}
	return exec.Execute(ctx, plan, scope)
}
