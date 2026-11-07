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

	FlowID            string         `json:"flow_id" yaml:"flow_id" db:"flow_id"`
	FlowName          string         `json:"flow_name" yaml:"flow_name" db:"-"`
	AgentFlowID       string         `json:"-" yaml:"-" db:"-"` // deprecated runtime alias
	FlowRevisionID    string         `json:"flow_revision_id" yaml:"flow_revision_id" db:"flow_revision_id"`
	FlowRevision      int64          `json:"flow_revision" yaml:"flow_revision" db:"-"`
	Version           int64          `json:"-" yaml:"-" db:"-"` // deprecated runtime alias
	Status            RunStatus      `json:"status" yaml:"status"`
	Input             map[string]any `json:"input" yaml:"input" db:"input"`
	Vars              map[string]any `json:"-" yaml:"-" db:"-"` // deprecated runtime alias
	Output            map[string]any `json:"output" yaml:"output"`
	Error             string         `json:"error" yaml:"error"`
	RunInstruction    string         `json:"run_instruction,omitempty" yaml:"run_instruction,omitempty"`
	SummarizeEnabled  bool           `json:"summarize_enabled" yaml:"summarize_enabled"`
	SummarizeOverride *bool          `json:"summarize_override,omitempty" yaml:"summarize_override,omitempty" db:"-"`
	ContextSnapshot   map[string]any `json:"context_snapshot" yaml:"context_snapshot"`
	TriggerType       string         `json:"trigger_type,omitempty" yaml:"trigger_type,omitempty" db:"trigger_type"`
	TriggerSource     string         `json:"trigger_source,omitempty" yaml:"trigger_source,omitempty" db:"trigger_source"`
	TriggerPayload    map[string]any `json:"trigger_payload,omitempty" yaml:"trigger_payload,omitempty" db:"trigger_payload"`
	StartedAt         *time.Time     `json:"started_at" yaml:"started_at"`
	FinishedAt        *time.Time     `json:"finished_at" yaml:"finished_at"`

	SharedMemory map[string]any            `json:"shared_memory,omitempty" yaml:"shared_memory,omitempty" db:"-"`
	ExecPlans    map[string]*ExecutionPlan `json:"exec_plans,omitempty" yaml:"exec_plans,omitempty" db:"-"`

	K8sNamespace     string            `json:"namespace,omitempty"`
	RuntimeMode      RuntimeMode       `json:"runtime_mode" yaml:"runtime_mode"`
	RuntimeClusterID string            `json:"runtime_cluster_id,omitempty" yaml:"runtime_cluster_id,omitempty"`
	Labels           map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// RunMetrics is the server-aggregated operational view used by the console.
// Buckets are ordered, gap-filled UTC windows so clients never aggregate a
// truncated Run page or guess missing intervals.
type RunMetrics struct {
	Total             int64             `json:"total"`
	Running           int64             `json:"running"`
	Completed         int64             `json:"completed"`
	Failed            int64             `json:"failed"`
	Cancelled         int64             `json:"cancelled"`
	SuccessRate       float64           `json:"success_rate"`
	FailureRate       float64           `json:"failure_rate"`
	AverageDurationMs int64             `json:"average_duration_ms"`
	Buckets           []RunMetricBucket `json:"buckets"`
}

type RunMetricBucket struct {
	StartTime         time.Time `json:"start_time"`
	EndTime           time.Time `json:"end_time"`
	Running           int64     `json:"running"`
	Completed         int64     `json:"completed"`
	Failed            int64     `json:"failed"`
	AverageDurationMs int64     `json:"average_duration_ms"`
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

// NormalizeAliases accepts legacy in-process fields while keeping the public
// and persistence contract canonical.
func (f *FlowRunInfo) NormalizeAliases() {
	if f.FlowName == "" {
		f.FlowName = f.AgentFlowID
	}
	if f.AgentFlowID == "" {
		f.AgentFlowID = f.FlowName
	}
	// Prior callers supplied the namespace-local flow name in FlowID. Keep
	// accepting that input until the DAO resolves it to the stable identity.
	if f.FlowName == "" && f.FlowID != "" {
		f.FlowName = f.FlowID
		f.AgentFlowID = f.FlowID
	}
	if f.Input == nil {
		f.Input = f.Vars
	}
	if f.Vars == nil {
		f.Vars = f.Input
	}
	if f.FlowRevision == 0 {
		f.FlowRevision = f.Version
	}
	if f.Version == 0 {
		f.Version = f.FlowRevision
	}
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
