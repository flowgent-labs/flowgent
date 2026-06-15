package executor

import (
	"context"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ─── Join Executor ─────────────────────────────────────

type JoinExecutor struct{}

func (e *JoinExecutor) TaskType() entities.TaskType { return entities.TaskJoin }

func (e *JoinExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.TaskResult, error) {
	// Join waits for all map children to complete and aggregates results.
	// The JM handles the aggregation; this marks the join as done.
	results, _ := plan.Input["results"].([]any)
	return &entities.TaskResult{Output: map[string]any{
		"results": results,
		"count":   len(results),
	}}, nil
}
