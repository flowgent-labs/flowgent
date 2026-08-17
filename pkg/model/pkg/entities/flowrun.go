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

	AgentFlowID    string         `json:"agentflow_id" yaml:"agentflow_id"`
	Version        int64          `json:"version" yaml:"version"`
	Status         RunStatus      `json:"status" yaml:"status"`
	Vars           map[string]any `json:"vars" yaml:"vars"`
	Output         map[string]any `json:"output" yaml:"output"`
	Error          string         `json:"error" yaml:"error"`
	TriggerType    string         `json:"trigger_type,omitempty" yaml:"trigger_type,omitempty" db:"trigger_type"`
	TriggerSource  string         `json:"trigger_source,omitempty" yaml:"trigger_source,omitempty" db:"trigger_source"`
	TriggerPayload map[string]any `json:"trigger_payload,omitempty" yaml:"trigger_payload,omitempty" db:"trigger_payload"`
	StartedAt      *time.Time     `json:"started_at" yaml:"started_at"`
	FinishedAt     *time.Time     `json:"finished_at" yaml:"finished_at"`

	SharedMemory map[string]any            `json:"shared_memory,omitempty" yaml:"shared_memory,omitempty" db:"-"`
	ExecPlans    map[string]*ExecutionPlan `json:"exec_plans,omitempty" yaml:"exec_plans,omitempty" db:"-"`

	K8sNamespace   string            `json:"namespace,omitempty"`
	ResourcePoolID string            `json:"resource_pool_id" yaml:"resource_pool_id"`
	Labels         map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// RunLifecycleUpdate is the narrow JobMaster-owned persistence contract for a
// run lifecycle transition. Keeping this separate from FlowRunInfo prevents a
// lifecycle write from accidentally replacing definition, trigger, variables,
// output, or scheduling metadata.
type RunLifecycleUpdate struct {
	Status     RunStatus  `json:"status"`
	Error      string     `json:"error,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// SetTrigger populates the flat trigger columns from a TriggerInfo value.
func (f *FlowRunInfo) SetTrigger(t TriggerInfo) {
	f.TriggerType = t.Type
	f.TriggerSource = t.Source
	f.TriggerPayload = t.Payload
}

// GetTrigger reconstructs a TriggerInfo from the flat trigger columns.
func (f *FlowRunInfo) GetTrigger() TriggerInfo {
	return TriggerInfo{
		Type:    f.TriggerType,
		Source:  f.TriggerSource,
		Payload: f.TriggerPayload,
	}
}

// TriggerInfo records how a run was initiated.
type TriggerInfo struct {
	Type    string         `json:"type" yaml:"type"`
	Source  string         `json:"source" yaml:"source"`
	Payload map[string]any `json:"payload" yaml:"payload"`
}
