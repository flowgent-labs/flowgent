package entities

import "time"

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

// FlowRunInfo is a single execution of an agentflow.
// The Status field shadows BaseEntity.Status to track run execution state.
type FlowRunInfo struct {
	BaseEntity

	AgentFlowID string         `json:"agentflow_id" yaml:"agentflow_id"`
	Version     int64          `json:"version" yaml:"version"`
	Status      RunStatus      `json:"status" yaml:"status"`
	Vars        map[string]any `json:"vars" yaml:"vars"`
	Output      map[string]any `json:"output" yaml:"output"`
	Error       string         `json:"error" yaml:"error"`
	Trigger     TriggerInfo    `json:"trigger" yaml:"trigger"`
	StartedAt   *time.Time     `json:"started_at" yaml:"started_at"`
	FinishedAt  *time.Time     `json:"finished_at" yaml:"finished_at"`

	SharedMemory map[string]any            `json:"shared_memory,omitempty" yaml:"shared_memory,omitempty"`
	ExecPlans    map[string]*ExecutionPlan `json:"exec_plans,omitempty" yaml:"exec_plans,omitempty"`

	Namespace string   `json:"namespace,omitempty"`
	Priority  Priority `json:"priority,omitempty"`
}

// TriggerInfo records how a run was initiated.
type TriggerInfo struct {
	Type    string         `json:"type" yaml:"type"`
	Source  string         `json:"source" yaml:"source"`
	Payload map[string]any `json:"payload" yaml:"payload"`
}
