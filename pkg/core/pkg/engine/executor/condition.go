package executor

import (
	"context"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ─── Condition Executor ────────────────────────────────

type ConditionExecutor struct{}

func (e *ConditionExecutor) TaskType() entities.TaskType { return entities.TaskCondition }

func (e *ConditionExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.TaskResult, error) {
	expr := plan.NodeSpec.Expression
	if expr == "" {
		expr = "${input.result == true}"
	}
	result := utils.EvalCondition(expr, scope)
	return &entities.TaskResult{Output: map[string]any{"result": result}}, nil
}
