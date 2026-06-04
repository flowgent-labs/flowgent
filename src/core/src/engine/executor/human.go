package executor

import (
	"context"
	"fmt"
	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
	"time"
)

// ─── Human Executor ────────────────────────────────────

type HumanExecutor struct {
	store store.IStore
}

func NewHumanExecutor(store store.IStore) *HumanExecutor {
	return &HumanExecutor{store: store}
}

func (e *HumanExecutor) TaskType() model.TaskType { return model.TaskHuman }

func (e *HumanExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	timeout := 24 * time.Hour
	if plan.NodeSpec.Approval != nil && plan.NodeSpec.Approval.Timeout.IsPositive() {
		timeout = plan.NodeSpec.Approval.Timeout.ToDuration()
	}

	approval := &model.HumanApproval{
		TaskRunID: plan.TaskID,
		Timeout:   timeout,
		Status:    "PENDING",
	}
	if err := e.store.CreateHumanApproval(ctx, approval); err != nil {
		return nil, fmt.Errorf("create human approval: %w", err)
	}

	onApprove := "continue"
	onReject := "continue"
	if plan.NodeSpec.Approval != nil {
		onApprove = plan.NodeSpec.Approval.OnApprove
		onReject = plan.NodeSpec.Approval.OnReject
	}

	return &model.TaskResult{Output: map[string]any{
		"approval_token": approval.Token,
		"timeout":        timeout.String(),
		"status":         "WAITING_HUMAN",
		"on_approve":     resolveAction(onApprove),
		"on_reject":      resolveAction(onReject),
	}}, nil
}

func resolveAction(action string) string {
	switch action {
	case "continue", "abort", "skip":
		return action
	default:
		return "continue"
	}
}
