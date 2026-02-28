package executor

import (
	"context"
	"github.com/flowgent-labs/flowgent/model/src"
)

// ─── Tribunal Executor ─────────────────────────────────

type TribunalExecutor struct{}

func (e *TribunalExecutor) TaskType() model.TaskType { return model.TaskTribunal }

func (e *TribunalExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	strategy := plan.NodeSpec.Strategy
	decisionType := "majority"
	if strategy != nil {
		if t, ok := strategy["type"].(string); ok {
			decisionType = t
		}
	}

	votes, _ := plan.Input["votes"].([]any)
	approveCount := 0
	totalCount := len(votes)
	for _, v := range votes {
		if m, ok := v.(map[string]any); ok {
			if dec, ok := m["decision"].(bool); ok && dec {
				approveCount++
			}
		}
	}

	var approved bool
	switch decisionType {
	case "majority":
		approved = approveCount > totalCount/2
	case "unanimous":
		approved = approveCount == totalCount
	case "any":
		approved = approveCount >= 1
	case "majority_strict":
		approved = approveCount >= (totalCount*2)/3
	default:
		approved = approveCount > totalCount/2
	}

	confidence := 0.0
	if totalCount > 0 {
		confidence = float64(approveCount) / float64(totalCount)
	}

	return &model.TaskResult{Output: map[string]any{
		"decision":   approved,
		"confidence": confidence,
		"approve":    approveCount,
		"total":      totalCount,
		"strategy":   decisionType,
	}}, nil
}
