package executor

import (
	"context"
	"github.com/flowgent-labs/flowgent/model/pkg"
)

// ─── Subflow Executor ──────────────────────────────────

type SubflowExecutor struct{}

func (e *SubflowExecutor) TaskType() model.TaskType { return model.TaskSubflow }

func (e *SubflowExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	// Subflow execution: the JM creates child ExecutionPlans from the
	// sub-agentflow spec and dispatches them.
	return &model.TaskResult{Output: map[string]any{"status": "dispatched"}}, nil
}
