package executor

import (
	"context"
	"github.com/flowgent-labs/flowgent/src/model"
)

// ─── Join Executor ─────────────────────────────────────

type JoinExecutor struct{}

func (e *JoinExecutor) TaskType() model.TaskType { return model.TaskJoin }

func (e *JoinExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	// Join waits for all map children to complete and aggregates results.
	// The JM handles the aggregation; this marks the join as done.
	results, _ := plan.Input["results"].([]any)
	return &model.TaskResult{Output: map[string]any{
		"results": results,
		"count":   len(results),
	}}, nil
}

