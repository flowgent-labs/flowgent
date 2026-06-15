package executor

import (
	"context"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ─── Subflow Executor ──────────────────────────────────

type SubflowExecutor struct{}

func (e *SubflowExecutor) TaskType() entities.TaskType { return entities.TaskSubflow }

func (e *SubflowExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.TaskResult, error) {
	// Subflow execution: the JM creates child ExecutionPlans from the
	// sub-agentflow spec and dispatches them.
	return &entities.TaskResult{Output: map[string]any{"status": "dispatched"}}, nil
}
