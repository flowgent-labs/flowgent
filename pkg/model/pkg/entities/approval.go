package entities

import "time"

// ApprovalInfo represents a pending human approval gate.
type ApprovalInfo struct {
	TaskRunID      string        `json:"task_run_id" yaml:"task_run_id"`
	AgentFlowRunID string        `json:"agentflow_run_id" yaml:"agentflow_run_id"`
	Token          string        `json:"token" yaml:"token"`
	Status         string        `json:"status" yaml:"status"`
	Approved       *bool         `json:"approved" yaml:"approved"`
	Comment        string        `json:"comment" yaml:"comment"`
	Timeout        time.Duration `json:"timeout" yaml:"timeout"`
	CreatedAt      time.Time     `json:"created_at" yaml:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at" yaml:"updated_at"`
	ExpiresAt      *time.Time    `json:"expires_at" yaml:"expires_at"`
	ResolvedAt     *time.Time    `json:"resolved_at" yaml:"resolved_at"`
}
