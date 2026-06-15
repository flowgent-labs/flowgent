package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// HumanApprovalStore is the narrow interface for creating human approval records.
// Implementations call the apiserver REST API (never direct DB).
type HumanApprovalStore interface {
	CreateApproval(ctx context.Context, approval *entities.ApprovalInfo) error
}

// HumanExecutor handles human-in-the-loop approval tasks.
type HumanExecutor struct {
	store HumanApprovalStore
}

func NewHumanExecutor(store HumanApprovalStore) *HumanExecutor {
	return &HumanExecutor{store: store}
}

func (e *HumanExecutor) TaskType() entities.TaskType { return entities.TaskHuman }

func (e *HumanExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.TaskResult, error) {
	timeout := 24 * time.Hour
	if plan.NodeSpec.Approval != nil && plan.NodeSpec.Approval.Timeout.IsPositive() {
		timeout = plan.NodeSpec.Approval.Timeout.ToDuration()
	}

	approval := &entities.ApprovalInfo{
		TaskRunID: plan.TaskID,
		Timeout:   timeout,
		Status:    "PENDING",
	}
	if err := e.store.CreateApproval(ctx, approval); err != nil {
		return nil, fmt.Errorf("create human approval: %w", err)
	}

	onApprove := "continue"
	onReject := "continue"
	if plan.NodeSpec.Approval != nil {
		onApprove = plan.NodeSpec.Approval.OnApprove
		onReject = plan.NodeSpec.Approval.OnReject
	}

	return &entities.TaskResult{Output: map[string]any{
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
