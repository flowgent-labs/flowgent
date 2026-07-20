package executor

import (
	"context"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ─── Noop Executor ─────────────────────────────────────

type NoopExecutor struct{}

func (e *NoopExecutor) TaskType() entities.TaskType { return entities.TaskNoop }

func (e *NoopExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.TaskResult, error) {
	out := map[string]any{"result": true}
	for k, v := range plan.Input {
		out[k] = v
	}
	return &entities.TaskResult{Output: out}, nil
}
