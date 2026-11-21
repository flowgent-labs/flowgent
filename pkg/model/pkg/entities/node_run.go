package entities

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ExecutionPlan is the internal JM→TM dispatch DTO. GraphRun and NodeRun are
// the durable business resources; a plan is not a separate public resource.
type ExecutionPlan struct {
	AgentGraphID       string            `json:"agent_graph_id"`
	WorkspacePath      string            `json:"workspace_path,omitempty"`
	GraphRevision      GraphRevision     `json:"graph_revision,omitempty"`
	SubGraphInstanceID string            `json:"subgraph_instance_id,omitempty"`
	GraphNodeID        string            `json:"graph_node_id,omitempty"`
	NodeInstanceID     string            `json:"node_instance_id,omitempty"`
	DispatchEpoch      int64             `json:"dispatch_epoch,omitempty"`
	PlanID             string            `json:"plan_id"`
	GraphRunID         string            `json:"graph_run_id"`
	AgentGraphName     string            `json:"agent_graph_name"`
	Namespace          string            `json:"namespace_id,omitempty"`
	RuntimeMode        RuntimeMode       `json:"runtime_mode"`
	RuntimeClusterID   string            `json:"runtime_cluster_id"`
	NodeRunID          string            `json:"node_run_id"`
	ParentNodeRunID    string            `json:"parent_node_run_id,omitempty"`
	NodeKind           NodeKind          `json:"node_kind"`
	TraceContext       map[string]string `json:"trace_context,omitempty"`

	State      NodeRunStatus `json:"state"`
	RetryCount int           `json:"retry_count"`
	MaxRetries int           `json:"max_retries"`

	AssignedTMID     string     `json:"assigned_tm_id,omitempty"`
	LeaseExpireAt    *time.Time `json:"lease_expire_at,omitempty"`
	FencingToken     int64      `json:"fencing_token"`
	WorkspaceVersion string     `json:"workspace_version,omitempty"`

	Input      map[string]any  `json:"input"`
	Result     *NodeResult     `json:"result,omitempty"`
	Checkpoint *NodeCheckpoint `json:"checkpoint,omitempty"`

	Node *GraphNode `json:"node"`

	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// NodeResult holds the outcome of a single noderun execution.
type NodeResult struct {
	Checkpoint        *NodeCheckpoint    `json:"checkpoint,omitempty"`
	NodeRunID         string             `json:"node_run_id,omitempty"`
	DispatchEpoch     int64              `json:"dispatch_epoch,omitempty"`
	WorkspaceSnapshot *WorkspaceSnapshot `json:"workspace_snapshot,omitempty"`
	State             NodeRunStatus      `json:"state,omitempty"`
	Output            map[string]any     `json:"output,omitempty"`
	Error             string             `json:"error,omitempty"`
}

// NodeCheckpoint captures agent execution state for resume-after-failure.
type NodeCheckpoint struct {
	WorkspaceSnapshot *WorkspaceSnapshot `json:"workspace_snapshot,omitempty"`
	ScriptPath        string             `json:"script_path,omitempty"`
	Messages          []MessageEntry     `json:"messages,omitempty"`
	Scratchpad        string             `json:"scratchpad,omitempty"`
	ToolCallState     map[string]any     `json:"tool_call_state,omitempty"`
	LastStep          int                `json:"last_step"`
	Intermediates     []any              `json:"intermediates,omitempty"`
	CheckpointedAt    time.Time          `json:"checkpointed_at"`
}

// MessageEntry is a single message in an agent conversation history.
type MessageEntry struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// NodeRunStatus is the lifecycle state of a noderun run.
type NodeRunStatus string

const (
	NodeRunPending      NodeRunStatus = "PENDING"
	NodeRunRunning      NodeRunStatus = "RUNNING"
	NodeRunSucceeded    NodeRunStatus = "SUCCESS"
	NodeRunFailed       NodeRunStatus = "FAILED"
	NodeRunWaitingHuman NodeRunStatus = "WAITING_HUMAN"
	NodeRunWaiting      NodeRunStatus = "WAITING"
	NodeRunSkipped      NodeRunStatus = "SKIPPED"
	NodeRunRetrying     NodeRunStatus = "RETRYING"
)

// NodeRun records the execution of a single node within an agentgraph run.
// The Status field shadows BaseEntity.Status to track noderun execution state.
type NodeRun struct {
	WorkspaceSnapshot *WorkspaceSnapshot `json:"workspace_snapshot,omitempty" yaml:"workspace_snapshot,omitempty" db:"-"`
	GraphRevision     GraphRevision      `json:"graph_revision,omitempty" yaml:"graph_revision,omitempty" db:"-"`
	BaseEntity

	SubGraphInstanceID string         `json:"subgraph_instance_id,omitempty" yaml:"subgraph_instance_id,omitempty" db:"-"`
	GraphNodeID        string         `json:"graph_node_id,omitempty" yaml:"graph_node_id,omitempty" db:"-"`
	NodeInstanceID     string         `json:"node_instance_id,omitempty" yaml:"node_instance_id,omitempty" db:"-"`
	DispatchEpoch      int64          `json:"dispatch_epoch,omitempty" yaml:"dispatch_epoch,omitempty" db:"-"`
	GraphRunID         string         `json:"graph_run_id" yaml:"graph_run_id" db:"graph_run_id"`
	Attempt            int            `json:"attempt" yaml:"attempt"`
	AgentRevisionID    string         `json:"agent_revision_id,omitempty" yaml:"agent_revision_id,omitempty"`
	Status             NodeRunStatus  `json:"status" yaml:"status"`
	Input              map[string]any `json:"input" yaml:"input"`
	Output             map[string]any `json:"output" yaml:"output"`
	Error              string         `json:"error" yaml:"error"`
	MaxRetries         int            `json:"max_retries" yaml:"max_retries" db:"-"`
	ExecutionID        string         `json:"execution_id" yaml:"execution_id" db:"execution_id"`
	ParentNodeRunID    string         `json:"parent_node_run_id,omitempty" yaml:"parent_node_run_id,omitempty"`
	Sequence           int            `json:"sequence" yaml:"sequence" db:"-"`
	ExecutionMemory    map[string]any `json:"execution_memory,omitempty" yaml:"execution_memory,omitempty"`
	Checkpoint         map[string]any `json:"checkpoint,omitempty" yaml:"checkpoint,omitempty"`
	WorkspaceVersion   string         `json:"workspace_version,omitempty" yaml:"workspace_version,omitempty"`
	LeaseOwner         string         `json:"lease_owner,omitempty" yaml:"lease_owner,omitempty"`
	LeaseExpiresAt     *time.Time     `json:"lease_expires_at,omitempty" yaml:"lease_expires_at,omitempty"`
	FencingToken       int64          `json:"fencing_token" yaml:"fencing_token"`
	LastHeartbeatAt    *time.Time     `json:"last_heartbeat_at,omitempty" yaml:"last_heartbeat_at,omitempty"`
	StartedAt          *time.Time     `json:"started_at" yaml:"started_at"`
	FinishedAt         *time.Time     `json:"finished_at" yaml:"finished_at"`
}

func (t *NodeRun) Normalize() {
	if t.Attempt == 0 {
		t.Attempt = 1
	}
	if t.GraphNodeID == "" {
		parts := strings.Split(t.NodeInstanceID, "/")
		t.GraphNodeID = parts[len(parts)-1]
	}
	if t.ExecutionID == "" {
		t.ExecutionID = t.ID
	}
}
func CloneGraphNode(n *GraphNode) *GraphNode {
	if n == nil {
		return nil
	}
	data, err := json.Marshal(n)
	if err != nil {
		return nil
	}
	var clone GraphNode
	if err = json.Unmarshal(data, &clone); err != nil {
		return nil
	}
	return &clone
}

// NodeRunFromPlan is the single projection from internal dispatch DTO to the
// durable NodeRun. JM, TM and repositories share identities and context fields.
func NodeRunFromPlan(plan *ExecutionPlan) *NodeRun {
	node := &NodeRun{BaseEntity: BaseEntity{ID: plan.NodeRunID, Namespace: plan.Namespace, CreatedAt: plan.CreatedAt, Metadata: map[string]any{"node": plan.Node, "trace_context": plan.TraceContext}}, GraphRunID: plan.GraphRunID, SubGraphInstanceID: plan.SubGraphInstanceID, GraphNodeID: plan.GraphNodeID, NodeInstanceID: plan.NodeInstanceID, GraphRevision: plan.GraphRevision, DispatchEpoch: plan.DispatchEpoch, Attempt: plan.RetryCount + 1, Status: plan.State, Input: plan.Input, MaxRetries: plan.MaxRetries, ExecutionID: fmt.Sprintf("%s-attempt-%d", plan.PlanID, plan.RetryCount+1), ParentNodeRunID: plan.ParentNodeRunID, Sequence: plan.RetryCount + 1, FencingToken: plan.FencingToken, WorkspaceVersion: plan.WorkspaceVersion, StartedAt: plan.StartedAt, FinishedAt: plan.FinishedAt}
	if plan.Result != nil {
		node.Output = plan.Result.Output
		node.Error = plan.Result.Error
		node.WorkspaceSnapshot = plan.Result.WorkspaceSnapshot
		if node.WorkspaceSnapshot != nil {
			node.WorkspaceVersion = node.WorkspaceSnapshot.ID
		}
	}
	checkpoint := plan.Checkpoint
	if plan.Result != nil && plan.Result.Checkpoint != nil {
		checkpoint = plan.Result.Checkpoint
	}
	if checkpoint != nil {
		data, _ := json.Marshal(checkpoint)
		_ = json.Unmarshal(data, &node.Checkpoint)
		node.ExecutionMemory = map[string]any{"messages": checkpoint.Messages, "last_step": checkpoint.LastStep}
	}
	return node
}

func (n *NodeRun) Result() *NodeResult {
	result := &NodeResult{NodeRunID: n.ID, DispatchEpoch: n.DispatchEpoch, State: n.Status, Output: n.Output, Error: n.Error, WorkspaceSnapshot: n.WorkspaceSnapshot}
	if n.Checkpoint != nil {
		data, _ := json.Marshal(n.Checkpoint)
		_ = json.Unmarshal(data, &result.Checkpoint)
	}
	return result
}
