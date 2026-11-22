package executor

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// SubGraphs are composite JM work and must never block a TM slot or report a
// child graph successful before its completion contract is satisfied. Legacy
// callers retain the dispatched output, with an explicit nonterminal outcome.
type SubGraphExecutor struct{}

func (e *SubGraphExecutor) NodeKind() entities.NodeKind { return entities.SubGraphNode }
func (e *SubGraphExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.NodeResult, error) {
	return &entities.NodeResult{State: entities.NodeRunWaiting, Output: map[string]any{"status": "dispatched"}}, nil
}
