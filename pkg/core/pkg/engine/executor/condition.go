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
	// The slot worker passes scope as {"input": plan.Input}. Expand it so
	// expressions like ${A.score} > 0.8 can look up dep nodes at the top level.
	merged := make(map[string]map[string]any)
	for k, v := range scope {
		merged[k] = v
	}
	if inputScope, ok := scope["input"]; ok {
		for k, v := range inputScope {
			if _, exists := merged[k]; !exists {
				if mv, ok := v.(map[string]any); ok {
					merged[k] = mv
				}
			}
		}
	}
	result := utils.EvalCondition(expr, merged)
	return &entities.TaskResult{Output: map[string]any{"result": result}}, nil
}
