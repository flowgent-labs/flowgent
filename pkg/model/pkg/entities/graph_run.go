package entities

import "time"

// GraphRunStatus is the lifecycle state of an agentgraph run.
type GraphRunStatus string

const (
	GraphRunPending   GraphRunStatus = "PENDING"
	GraphRunRunning   GraphRunStatus = "RUNNING"
	GraphRunCompleted GraphRunStatus = "COMPLETED"
	GraphRunFailed    GraphRunStatus = "FAILED"
	GraphRunPaused    GraphRunStatus = "PAUSED"
	GraphRunCancelled GraphRunStatus = "CANCELLED"
)

// GraphRun is a single execution of an agentgraph.
// The Status field shadows BaseEntity.Status to track run execution state.
type GraphRun struct {
	SubGraphs map[string]*SubGraph `json:"subgraphs,omitempty" yaml:"-" db:"-"`
	BaseEntity
	Snapshot             AgentGraph     `json:"snapshot" yaml:"snapshot" db:"snapshot"`
	NodeRuns             []NodeRun      `json:"node_runs,omitempty" yaml:"-" db:"-"`
	StateVersion         int64          `json:"state_version,omitempty" yaml:"state_version,omitempty" db:"-"`
	LastEventSeq         int64          `json:"last_event_seq,omitempty" yaml:"last_event_seq,omitempty" db:"-"`
	AgentGraphID         string         `json:"agent_graph_id" yaml:"agent_graph_id" db:"agent_graph_id"`
	AgentGraphName       string         `json:"agent_graph_name" yaml:"agent_graph_name" db:"-"`
	AgentGraphRevisionID string         `json:"agent_graph_revision_id" yaml:"agent_graph_revision_id" db:"agent_graph_revision_id"`
	AgentGraphRevision   int64          `json:"agent_graph_revision" yaml:"agent_graph_revision" db:"-"`
	Status               GraphRunStatus `json:"status" yaml:"status"`
	Input                map[string]any `json:"input" yaml:"input" db:"input"`
	Output               map[string]any `json:"output" yaml:"output"`
	Error                string         `json:"error" yaml:"error"`
	RunInstruction       string         `json:"run_instruction,omitempty" yaml:"run_instruction,omitempty"`
	SummarizeEnabled     bool           `json:"summarize_enabled" yaml:"summarize_enabled"`
	SummarizeOverride    *bool          `json:"summarize_override,omitempty" yaml:"summarize_override,omitempty" db:"-"`
	ContextSnapshot      map[string]any `json:"context_snapshot" yaml:"context_snapshot"`
	TriggerType          string         `json:"trigger_type,omitempty" yaml:"trigger_type,omitempty" db:"trigger_type"`
	TriggerSource        string         `json:"trigger_source,omitempty" yaml:"trigger_source,omitempty" db:"trigger_source"`
	TriggerPayload       map[string]any `json:"trigger_payload,omitempty" yaml:"trigger_payload,omitempty" db:"trigger_payload"`
	StartedAt            *time.Time     `json:"started_at" yaml:"started_at"`
	FinishedAt           *time.Time     `json:"finished_at" yaml:"finished_at"`

	K8sNamespace     string            `json:"namespace,omitempty"`
	RuntimeMode      RuntimeMode       `json:"runtime_mode" yaml:"runtime_mode"`
	RuntimeClusterID string            `json:"runtime_cluster_id,omitempty" yaml:"runtime_cluster_id,omitempty"`
	Labels           map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// GraphRunMetrics is the server-aggregated operational view used by the console.
// Buckets are ordered, gap-filled UTC windows so clients never aggregate a
// truncated Run page or guess missing intervals.
type GraphRunMetrics struct {
	Total             int64                  `json:"total"`
	Running           int64                  `json:"running"`
	Completed         int64                  `json:"completed"`
	Failed            int64                  `json:"failed"`
	Cancelled         int64                  `json:"cancelled"`
	SuccessRate       float64                `json:"success_rate"`
	FailureRate       float64                `json:"failure_rate"`
	AverageDurationMs int64                  `json:"average_duration_ms"`
	Buckets           []GraphRunMetricBucket `json:"buckets"`
}

type GraphRunMetricBucket struct {
	StartTime         time.Time `json:"start_time"`
	EndTime           time.Time `json:"end_time"`
	Running           int64     `json:"running"`
	Completed         int64     `json:"completed"`
	Failed            int64     `json:"failed"`
	AverageDurationMs int64     `json:"average_duration_ms"`
}

// GraphRunLifecycleUpdate is the narrow JobMaster-owned persistence contract for a
// run lifecycle transition. Keeping this separate from GraphRun prevents a
// lifecycle write from accidentally replacing definition, trigger, variables,
// output, or scheduling metadata.
type GraphRunLifecycleUpdate struct {
	Status     GraphRunStatus `json:"status"`
	Error      string         `json:"error,omitempty"`
	StartedAt  *time.Time     `json:"started_at,omitempty"`
	FinishedAt *time.Time     `json:"finished_at,omitempty"`
}

// Normalize supplies defaults for a newly admitted GraphRun.
func (r *GraphRun) Normalize() {
	if r.RuntimeMode == "" {
		r.RuntimeMode = RuntimeModeApplication
	}
}

// SetTrigger populates the flat trigger columns from a TriggerInfo value.
func (f *GraphRun) SetTrigger(t TriggerInfo) {
	f.TriggerType = t.Type
	f.TriggerSource = t.Source
	f.TriggerPayload = t.Payload
}

// GetTrigger reconstructs a TriggerInfo from the flat trigger columns.
func (f *GraphRun) GetTrigger() TriggerInfo {
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
