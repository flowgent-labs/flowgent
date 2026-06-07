package executor

import (
	"context"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg"
)

// ─── Condition Executor ────────────────────────────────

type ConditionExecutor struct{}

func (e *ConditionExecutor) TaskType() model.TaskType { return model.TaskCondition }

func (e *ConditionExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	expr := plan.NodeSpec.Expression
	if expr == "" {
		expr = "${input.result == true}"
	}
	result := utils.EvalCondition(expr, scope)
	return &model.TaskResult{Output: map[string]any{"result": result}}, nil
}
