package executor

import (
	"context"
	"github.com/flowgent-labs/flowgent/src/model"
)

// ─── Noop Executor ─────────────────────────────────────

type NoopExecutor struct{}

func (e *NoopExecutor) TaskType() model.TaskType { return model.TaskNoop }

func (e *NoopExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	return &model.TaskResult{Output: nil}, nil
}

