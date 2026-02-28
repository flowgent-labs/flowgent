package model

import (
	"context"
	"time"
)

type RunStatus string

const (
	RunPending   RunStatus = "PENDING"
	RunRunning   RunStatus = "RUNNING"
	RunCompleted RunStatus = "COMPLETED"
	RunFailed    RunStatus = "FAILED"
	RunPaused    RunStatus = "PAUSED"
	RunCancelled RunStatus = "CANCELLED"
)

type AgentFlowRun struct {
	ID          string         `json:"id" yaml:"id"`
	AgentFlowID string         `json:"agentflow_id" yaml:"agentflow_id"`
	Version     int64          `json:"version" yaml:"version"`
	Status      RunStatus      `json:"status" yaml:"status"`
	Vars        map[string]any `json:"vars" yaml:"vars"`
	Output      map[string]any `json:"output" yaml:"output"`
	Error       string         `json:"error" yaml:"error"`
	Trigger     TriggerInfo    `json:"trigger" yaml:"trigger"`
	CreatedAt   time.Time      `json:"created_at" yaml:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at" yaml:"updated_at"`
	StartedAt   *time.Time     `json:"started_at" yaml:"started_at"`
	FinishedAt  *time.Time     `json:"finished_at" yaml:"finished_at"`

	// SharedMemory is visible to all ExecutionPlans in this run.
	// Used for cross-node data exchange (e.g., scan results → fix plans).
	SharedMemory map[string]any `json:"shared_memory,omitempty" yaml:"shared_memory,omitempty"`

	// ExecPlans holds all ExecutionPlans for this run, keyed by plan_id.
	ExecPlans map[string]*ExecutionPlan `json:"exec_plans,omitempty" yaml:"exec_plans,omitempty"`

	// Multi-tenant & scheduling metadata (propagated from AgentFlowSpec at trigger time).
	TenantID  string `json:"tenant_id,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Priority  Priority `json:"priority,omitempty"`
}

type TriggerInfo struct {
	Type    string         `json:"type" yaml:"type"`
	Source  string         `json:"source" yaml:"source"`
	Payload map[string]any `json:"payload" yaml:"payload"`
}

type TaskStatus string

const (
	TaskPending  TaskStatus = "PENDING"
	Running      TaskStatus = "RUNNING"
	Success      TaskStatus = "SUCCESS"
	Failed       TaskStatus = "FAILED"
	WaitingHuman TaskStatus = "WAITING_HUMAN"
	Skipped      TaskStatus = "SKIPPED"
	TaskRetrying TaskStatus = "RETRYING"
)

type TaskRun struct {
	ID              string         `json:"id" yaml:"id"`
	AgentFlowRunID  string         `json:"agentflow_run_id" yaml:"agentflow_run_id"`
	NodeID          string         `json:"node_id" yaml:"node_id"`
	Status          TaskStatus     `json:"status" yaml:"status"`
	Input           map[string]any `json:"input" yaml:"input"`
	Output          map[string]any `json:"output" yaml:"output"`
	Error           string         `json:"error" yaml:"error"`
	RetryCount      int            `json:"retry_count" yaml:"retry_count"`
	MaxRetries      int            `json:"max_retries" yaml:"max_retries"`
	ExecID          string         `json:"exec_id" yaml:"exec_id"`
	ParentTaskRunID string         `json:"parent_task_run_id" yaml:"parent_task_run_id"`
	Sequence        int            `json:"sequence" yaml:"sequence"`
	CreatedAt       time.Time      `json:"created_at" yaml:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at" yaml:"updated_at"`
	StartedAt       *time.Time     `json:"started_at" yaml:"started_at"`
	FinishedAt      *time.Time     `json:"finished_at" yaml:"finished_at"`
}

// HumanApprovalStore is the minimal interface for human approval persistence.
// Both the engine and the payments module consume this interface,
// ensuring a single approval subsystem.
type HumanApprovalStore interface {
	CreateHumanApproval(ctx context.Context, approval *HumanApproval) error
	GetHumanApproval(ctx context.Context, token string) (*HumanApproval, error)
	UpdateHumanApproval(ctx context.Context, approval *HumanApproval) error
}

type HumanApproval struct {
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
