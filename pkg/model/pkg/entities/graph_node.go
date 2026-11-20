package entities

import (
	"fmt"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg"
)

var nodeTypes = map[NodeKind]struct{}{
	AgentNode: {}, ToolNode: {}, SkillNode: {}, MapNode: {}, SubGraphNode: {},
	ConditionNode: {}, CommitteeNode: {}, HumanNode: {}, SupervisorNode: {},
	SandboxNode: {}, JoinNode: {}, NoopNode: {},
}

// ValidateGraphNode verifies the canonical node discriminator recursively.
func ValidateGraphNode(node *GraphNode) error {
	if node == nil {
		return fmt.Errorf("node is required")
	}
	if node.ID == "" {
		return fmt.Errorf("node id is required")
	}
	if _, ok := nodeTypes[node.Kind]; !ok {
		return fmt.Errorf("node %q has unsupported kind %q", node.ID, node.Kind)
	}
	if node.Kind == SubGraphNode && node.SubGraph == nil {
		return fmt.Errorf("subgraph config is required")
	}
	if err := node.SubGraph.Validate(); err != nil {
		return err
	}
	if node.SubGraph != nil && node.Kind != SubGraphNode {
		return fmt.Errorf("subgraph config requires agentgraph kind")
	}
	if node.JoinMode != "" && node.JoinMode != "and" && node.JoinMode != "or" {
		return fmt.Errorf("invalid join_mode")
	}
	if node.Node != nil {
		return ValidateGraphNode(node.Node)
	}
	return nil
}

// NodeKind is the type of a DAG node.
type NodeKind string

const (
	AgentNode      NodeKind = "agent"
	ToolNode       NodeKind = "tool"
	SkillNode      NodeKind = "skill"
	MapNode        NodeKind = "map"
	SubGraphNode   NodeKind = "subgraph"
	ConditionNode  NodeKind = "condition"
	CommitteeNode  NodeKind = "committee"
	HumanNode      NodeKind = "human"
	SupervisorNode NodeKind = "supervisor"
	SandboxNode    NodeKind = "sandbox"
	JoinNode       NodeKind = "join"
	NoopNode       NodeKind = "noop"
)

// GraphNode represents a single node in the agentgraph DAG. The manifest contract is
// one flat object whose discriminator is kind.
type GraphNode struct {
	GraphNodeID      string                  `json:"graph_node_id,omitempty" yaml:"graph_node_id,omitempty"`
	ID               string                  `json:"id" yaml:"id"`
	Kind             NodeKind                `json:"kind" yaml:"kind"`
	Solution         string                  `json:"solution,omitempty" yaml:"solution,omitempty"`
	Agent            string                  `json:"agent,omitempty" yaml:"agent,omitempty"`
	Skill            string                  `json:"skill,omitempty" yaml:"skill,omitempty"`
	Tool             string                  `json:"tool,omitempty" yaml:"tool,omitempty"`
	Source           string                  `json:"source,omitempty" yaml:"source,omitempty"`
	Expression       string                  `json:"expression,omitempty" yaml:"expression,omitempty"`
	Instruction      string                  `json:"instruction,omitempty" yaml:"instruction,omitempty"`
	Strategy         map[string]any          `json:"strategy,omitempty" yaml:"strategy,omitempty"`
	Retry            *RetryPolicy            `json:"retry,omitempty" yaml:"retry,omitempty"`
	Node             *GraphNode              `json:"node,omitempty" yaml:"node,omitempty"`
	Concurrency      int                     `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	Approval         *HumanApprovalConfig    `json:"approval,omitempty" yaml:"approval,omitempty"`
	SupervisorConfig *SupervisorConfig       `json:"supervisor_config,omitempty" yaml:"supervisor_config,omitempty"`
	SubGraph         *SubGraphConfig         `json:"subgraph,omitempty" yaml:"subgraph,omitempty"`
	JoinMode         string                  `json:"join_mode,omitempty" yaml:"join_mode,omitempty"`
	OutputSchema     map[string]any          `json:"output_schema,omitempty" yaml:"output_schema,omitempty"`
	Runtime          string                  `json:"runtime,omitempty" yaml:"runtime,omitempty"`
	Script           string                  `json:"script,omitempty" yaml:"script,omitempty"`
	Timeout          string                  `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Resources        *model.SandboxResources `json:"resources,omitempty" yaml:"resources,omitempty"`
	NetworkPolicy    *model.NetworkPolicy    `json:"network_policy,omitempty" yaml:"network_policy,omitempty"`
	Workspace        string                  `json:"workspace,omitempty" yaml:"workspace,omitempty"`
	Args             map[string]any          `json:"args,omitempty" yaml:"args,omitempty"`
}

// GraphEdge represents a directed edge in the DAG.
type GraphEdge struct {
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
