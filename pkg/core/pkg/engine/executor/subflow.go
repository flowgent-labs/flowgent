package executor

import (
	"context"
	"log"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ─── Subflow Executor ──────────────────────────────────

type SubflowExecutor struct{}

func (e *SubflowExecutor) TaskType() entities.TaskType { return entities.TaskSubflow }

func (e *SubflowExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.TaskResult, error) {
	log.Printf("WARNING: SubflowExecutor is a stub — sub-flow node %q will not execute child flows", plan.NodeID)
	return &entities.TaskResult{Output: map[string]any{"status": "dispatched"}}, nil
}
