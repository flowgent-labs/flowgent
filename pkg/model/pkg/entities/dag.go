package entities

import (
	"fmt"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg"
)

var nodeTypes = map[NodeType]struct{}{
	AgentNode: {}, ToolNode: {}, SkillNode: {}, MapNode: {}, AgentFlowNode: {},
	ConditionNode: {}, CommitteeNode: {}, HumanNode: {}, SupervisorNode: {},
	SandboxNode: {}, JoinNode: {}, NoopNode: {},
}

// ValidateNode verifies the canonical node discriminator recursively.
func ValidateNode(node *Node) error {
	if node == nil {
		return fmt.Errorf("node is required")
	}
	if node.ID == "" {
		return fmt.Errorf("node id is required")
	}
	if _, ok := nodeTypes[node.Kind]; !ok {
		return fmt.Errorf("node %q has unsupported kind %q", node.ID, node.Kind)
	}
	if node.Node != nil {
		return ValidateNode(node.Node)
	}
	return nil
}

// NodeType is the type of a DAG node.
type NodeType string

const (
	AgentNode      NodeType = "agent"
	ToolNode       NodeType = "tool"
	SkillNode      NodeType = "skill"
	MapNode        NodeType = "map"
	AgentFlowNode  NodeType = "agentflow"
	ConditionNode  NodeType = "condition"
	CommitteeNode  NodeType = "committee"
	HumanNode      NodeType = "human"
	SupervisorNode NodeType = "supervisor"
	SandboxNode    NodeType = "sandbox"
	JoinNode       NodeType = "join"
	NoopNode       NodeType = "noop"
)

// Node represents a single node in the agentflow DAG. The manifest contract is
// one flat object whose discriminator is kind.
type Node struct {
	ID               string                  `json:"id" yaml:"id"`
	Kind             NodeType                `json:"kind" yaml:"kind"`
	Solution         string                  `json:"solution,omitempty" yaml:"solution,omitempty"`
	Agent            string                  `json:"agent,omitempty" yaml:"agent,omitempty"`
	Skill            string                  `json:"skill,omitempty" yaml:"skill,omitempty"`
	Tool             string                  `json:"tool,omitempty" yaml:"tool,omitempty"`
	Source           string                  `json:"source,omitempty" yaml:"source,omitempty"`
	Expression       string                  `json:"expression,omitempty" yaml:"expression,omitempty"`
	Instruction      string                  `json:"instruction,omitempty" yaml:"instruction,omitempty"`
	Strategy         map[string]any          `json:"strategy,omitempty" yaml:"strategy,omitempty"`
	Retry            *RetryPolicy            `json:"retry,omitempty" yaml:"retry,omitempty"`
	Node             *Node                   `json:"node,omitempty" yaml:"node,omitempty"`
	Concurrency      int                     `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	Approval         *HumanApprovalConfig    `json:"approval,omitempty" yaml:"approval,omitempty"`
	SupervisorConfig *SupervisorConfig       `json:"supervisor_config,omitempty" yaml:"supervisor_config,omitempty"`
	AgentFlowID      string                  `json:"agentflow,omitempty" yaml:"agentflow,omitempty"`
	OutputSchema     map[string]any          `json:"output_schema,omitempty" yaml:"output_schema,omitempty"`
	Runtime          string                  `json:"runtime,omitempty" yaml:"runtime,omitempty"`
	Script           string                  `json:"script,omitempty" yaml:"script,omitempty"`
	Timeout          string                  `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Resources        *model.SandboxResources `json:"resources,omitempty" yaml:"resources,omitempty"`
	NetworkPolicy    *model.NetworkPolicy    `json:"network_policy,omitempty" yaml:"network_policy,omitempty"`
	Workspace        string                  `json:"workspace,omitempty" yaml:"workspace,omitempty"`
	Args             map[string]any          `json:"args,omitempty" yaml:"args,omitempty"`
}

// Edge represents a directed edge in the DAG.
type Edge struct {
	From      string `json:"from" yaml:"from"`
	To        string `json:"to" yaml:"to"`
	Condition *bool  `json:"condition,omitempty" yaml:"condition,omitempty"`
}

// RetryPolicy defines the retry behavior for a node.
type RetryPolicy struct {
	Max      int                `json:"max" yaml:"max"`
	Initial  utils.UnitDuration `json:"initial" yaml:"initial"`
	MaxDelay utils.UnitDuration `json:"max_delay" yaml:"max_delay"`
	Factor   float64            `json:"factor" yaml:"factor"`
}

// HumanApprovalConfig defines the approval gate configuration for human nodes.
type HumanApprovalConfig struct {
	Timeout   utils.UnitDuration `json:"timeout" yaml:"timeout"`
	OnApprove string             `json:"on_approve" yaml:"on_approve"`
	OnReject  string             `json:"on_reject" yaml:"on_reject"`
}

// SupervisorConfig defines the constraints for supervisor nodes.
type SupervisorConfig struct {
	MaxRetries     int      `json:"max_retries" yaml:"max_retries"`
	MaxNodes       int      `json:"max_nodes" yaml:"max_nodes"`
	MaxInjections  int      `json:"max_injections" yaml:"max_injections"`
	AllowedActions []string `json:"allowed_actions" yaml:"allowed_actions"`
}
