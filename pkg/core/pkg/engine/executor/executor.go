package executor

import (
	"context"
	"fmt"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// TaskExecutor executes a single ExecutionPlan. Each noderun type has its
// own implementation, registered in the TaskExecutorRouter.
type TaskExecutor interface {
	NodeKind() entities.NodeKind
	Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.NodeResult, error)
}

// TaskExecutorRouter dispatches an ExecutionPlan to the correct TaskExecutor by plan.NodeKind.
type TaskExecutorRouter struct {
	executors map[entities.NodeKind]TaskExecutor
}

func NewTaskExecutorRouter() *TaskExecutorRouter {
	return &TaskExecutorRouter{executors: make(map[entities.NodeKind]TaskExecutor)}
}

func (r *TaskExecutorRouter) Register(exec TaskExecutor) {
	r.executors[exec.NodeKind()] = exec
}

func (r *TaskExecutorRouter) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.NodeResult, error) {
	exec, ok := r.executors[plan.NodeKind]
	if !ok {
		return nil, fmt.Errorf("no executor registered for noderun type %q", plan.NodeKind)
	}
	return exec.Execute(ctx, plan, scope)
}
