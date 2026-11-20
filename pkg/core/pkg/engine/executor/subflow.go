package executor

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// Subflows are composite JM work and must never block a TM slot or report a
// child graph successful before its completion contract is satisfied. Legacy
// callers retain the dispatched output, with an explicit nonterminal outcome.
type SubflowExecutor struct{}

func (e *SubflowExecutor) TaskType() entities.TaskType { return entities.TaskSubflow }
func (e *SubflowExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.TaskResult, error) {
	return &entities.TaskResult{State: entities.TaskWaiting, Output: map[string]any{"status": "dispatched"}}, nil
}
