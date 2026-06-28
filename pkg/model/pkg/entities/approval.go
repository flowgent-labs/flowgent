package entities

import "time"

// ApprovalInfo represents a pending human approval gate.
// The Status field tracks the approval decision state; it shadows BaseEntity.Status.
type ApprovalInfo struct {
	BaseEntity

	TaskRunID      string        `json:"task_run_id" yaml:"task_run_id"`
	AgentFlowRunID string        `json:"agentflow_run_id" yaml:"agentflow_run_id"`
	Token          string        `json:"token" yaml:"token"`
	Status         string        `json:"status" yaml:"status"`
	Approved       *bool         `json:"approved" yaml:"approved"`
	Comment        string        `json:"comment" yaml:"comment"`
	Timeout        time.Duration `json:"timeout" yaml:"timeout" db:"-"`
	ExpiresAt      *time.Time    `json:"expires_at" yaml:"expires_at"`
	ResolvedAt     *time.Time    `json:"resolved_at" yaml:"resolved_at"`
}
