package executor

import (
	"context"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ─── Noop Executor ─────────────────────────────────────

type NoopExecutor struct{}

func (e *NoopExecutor) NodeKind() entities.NodeKind { return entities.NoopNode }

func (e *NoopExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.NodeResult, error) {
	out := map[string]any{"result": true}
	if plan != nil {
		for k, v := range plan.Input {
			out[k] = v
		}
	}
	return &entities.NodeResult{Output: out}, nil
}
