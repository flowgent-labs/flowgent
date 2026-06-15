package executor

import (
	"context"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ─── Noop Executor ─────────────────────────────────────

type NoopExecutor struct{}

func (e *NoopExecutor) TaskType() entities.TaskType { return entities.TaskNoop }

func (e *NoopExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.TaskResult, error) {
	return &entities.TaskResult{Output: nil}, nil
}
