// Package model defines the shared domain types for the Flowgent engine.
//
// File: run.go — Runtime execution records consumed by JobManager, API Server, Controller.
//
//	AgentFlowRun, TaskRun, RunStatus, TaskStatus, TriggerInfo.
package model

import "time"

// ─── AgentFlow run ───────────────────────────────────────────

// RunStatus is the lifecycle state of an agentflow run.
type RunStatus string

const (
	RunPending   RunStatus = "PENDING"
	RunRunning   RunStatus = "RUNNING"
	RunCompleted RunStatus = "COMPLETED"
	RunFailed    RunStatus = "FAILED"
	RunPaused    RunStatus = "PAUSED"
	RunCancelled RunStatus = "CANCELLED"
)

// AgentFlowRun is a single execution of an agentflow.
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
	SharedMemory map[string]any `json:"shared_memory,omitempty" yaml:"shared_memory,omitempty"`

	// ExecPlans holds all ExecutionPlans for this run, keyed by plan_id.
	ExecPlans map[string]*ExecutionPlan `json:"exec_plans,omitempty" yaml:"exec_plans,omitempty"`

	// Multi-tenant & scheduling metadata (propagated from AgentFlowSpec at trigger time).
	TenantID  string   `json:"tenant_id,omitempty"`
	Namespace string   `json:"namespace,omitempty"`
	Priority  Priority `json:"priority,omitempty"`
}

// TriggerInfo records how a run was initiated.
type TriggerInfo struct {
	Type    string         `json:"type" yaml:"type"`
	Source  string         `json:"source" yaml:"source"`
	Payload map[string]any `json:"payload" yaml:"payload"`
}

// ─── Task run ────────────────────────────────────────────────

