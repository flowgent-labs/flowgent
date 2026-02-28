package model

import "time"

// TaskType is the execution type of an ExecutionPlan.
// Maps from NodeType but adds runtime-only types (join, subflow).
type TaskType string

const (
	TaskAgent      TaskType = "agent"
	TaskCondition  TaskType = "condition"
	TaskTool       TaskType = "tool"
	TaskSkill      TaskType = "skill"
	TaskSupervisor TaskType = "supervisor"
	TaskSubflow    TaskType = "subflow"
	TaskTribunal   TaskType = "tribunal"
	TaskMap        TaskType = "map"
	TaskJoin       TaskType = "join"
	TaskHuman      TaskType = "human"
	TaskSandbox    TaskType = "sandbox"
	TaskNoop       TaskType = "noop"
)

// ExecutionPlan is the primary runtime object — the unit of work that flows
// through the system. It is serializable, resumable, retryable, and
// reassignable between TaskManagers.
type ExecutionPlan struct {
	PlanID         string     `json:"plan_id"`
	AgentFlowRunID string     `json:"agentflow_run_id"`
	TaskID         string     `json:"task_id"`
	TaskType       TaskType   `json:"task_type"`
	NodeID         string     `json:"node_id"`

	State      TaskStatus `json:"state"`
	RetryCount int       `json:"retry_count"`
	MaxRetries int       `json:"max_retries"`

	AssignedTMID  string     `json:"assigned_tm_id,omitempty"`
	LeaseExpireAt *time.Time `json:"lease_expire_at,omitempty"`

	Input      map[string]any  `json:"input"`
	Result     *TaskResult     `json:"result,omitempty"`
	Checkpoint *TaskCheckpoint `json:"checkpoint,omitempty"`

	NodeSpec *NodeSpec `json:"node_spec"`

	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// NodeSpec is the simplified node definition embedded in an ExecutionPlan.
// It decouples the plan from the full AgentFlowSpec — a TM only needs
// the plan, not the original spec.
type NodeSpec struct {
	ID               string               `json:"id"`
	Type             NodeType             `json:"type"`
	Agent            string               `json:"agent,omitempty"`
	Skill            string               `json:"skill,omitempty"`
	AgentFlowID      string               `json:"agentflow,omitempty"`
	Tool             string               `json:"tool,omitempty"`
	Source           string               `json:"source,omitempty"`
	Expression       string               `json:"expression,omitempty"`
	Instruction      string               `json:"instruction,omitempty"`
	Strategy         map[string]any       `json:"strategy,omitempty"`
	Concurrency      int                  `json:"concurrency,omitempty"`
	Retry            *RetryPolicy         `json:"retry,omitempty"`
	Approval         *HumanApprovalConfig `json:"approval,omitempty"`
	SupervisorConfig *SupervisorConfig    `json:"supervisor_config,omitempty"`
	ChildNode        *NodeSpec            `json:"child_node,omitempty"`
	RawInput         map[string]any       `json:"raw_input,omitempty"` // YAML-defined input
	// Sandbox fields
	Runtime   string            `json:"runtime,omitempty"`
	Script    string            `json:"script,omitempty"`
	Timeout   string            `json:"timeout,omitempty"`
	Resources *SandboxResources `json:"resources,omitempty"`
}

// TaskResult holds the outcome of a single task execution.
type TaskResult struct {
	Output map[string]any `json:"output,omitempty"`
	Error  string         `json:"error,omitempty"`
}

// TaskCheckpoint captures agent execution state for resume-after-failure.
type TaskCheckpoint struct {
	Messages       []MessageEntry `json:"messages,omitempty"`
	Scratchpad     string         `json:"scratchpad,omitempty"`
	ToolCallState  map[string]any `json:"tool_call_state,omitempty"`
	LastStep       int            `json:"last_step"`
	Intermediates  []any          `json:"intermediates,omitempty"`
	CheckpointedAt time.Time      `json:"checkpointed_at"`
}

// MessageEntry is a single message in an agent conversation history.
type MessageEntry struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// NodeSpecFromNode converts a full Node to the lightweight NodeSpec
// embedded in ExecutionPlans.
func NodeSpecFromNode(n *Node) *NodeSpec {
	if n == nil {
		return nil
	}
	spec := &NodeSpec{
		ID:               n.ID,
		Type:             n.Type,
		Agent:            n.Agent,
		Skill:            n.Skill,
		AgentFlowID:      n.AgentFlowID,
		Tool:             n.Tool,
		Source:           n.Source,
		Expression:       n.Expression,
		Instruction:      n.Instruction,
		Strategy:         n.Strategy,
		Concurrency:      n.Concurrency,
		Retry:            n.Retry,
		Approval:         n.Approval,
		SupervisorConfig: n.SupervisorConfig,
		Runtime:          n.Runtime,
		Script:           n.Script,
		Timeout:          n.Timeout,
		Resources:        n.Resources,
		RawInput:         n.Input,
	}
	if n.Node != nil {
		spec.ChildNode = NodeSpecFromNode(n.Node)
	}
	return spec
}
