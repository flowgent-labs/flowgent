package entities

import (
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"gopkg.in/yaml.v3"
)

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

// Node represents a single node in the agentflow DAG.
// Both old flat format and new kind+spec nested format are supported via custom UnmarshalYAML.
type Node struct {
	ID               string                  `json:"id" yaml:"id"`
	Kind             NodeType                `json:"kind,omitempty" yaml:"kind,omitempty"`
	Type             NodeType                `json:"type" yaml:"type,omitempty"`
	Solution         string                  `json:"solution,omitempty" yaml:"solution,omitempty"`
	Agent            string                  `json:"agent,omitempty" yaml:"agent,omitempty"`
	Skill            string                  `json:"skill,omitempty" yaml:"skill,omitempty"`
	Tool             string                  `json:"tool,omitempty" yaml:"tool,omitempty"`
	Source           string                  `json:"source,omitempty" yaml:"source,omitempty"`
	Expression       string                  `json:"expression,omitempty" yaml:"expression,omitempty"`
	Instruction      string                  `json:"instruction,omitempty" yaml:"instruction,omitempty"`
	Strategy         map[string]any          `json:"strategy,omitempty" yaml:"strategy,omitempty"`
	Input            map[string]any          `json:"input,omitempty" yaml:"input,omitempty"`
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

// nodeSpecNewFormat is the intermediate struct for parsing the new kind+spec node format.
type nodeSpecNewFormat struct {
	Tool             string                  `yaml:"tool"`
	Agent            string                  `yaml:"agent"`
	Skill            string                  `yaml:"skill"`
	Source           string                  `yaml:"source"`
	Expression       string                  `yaml:"expression"`
	Args             map[string]any          `yaml:"args"`
	Strategy         map[string]any          `yaml:"strategy"`
	Retry            *RetryPolicy            `yaml:"retry"`
	Concurrency      int                     `yaml:"concurrency"`
	Approval         *HumanApprovalConfig    `yaml:"approval"`
	SupervisorConfig *SupervisorConfig       `yaml:"supervisor_config"`
	AgentFlowID      string                  `yaml:"agentflow"`
	Runtime          string                  `yaml:"runtime"`
	Script           string                  `yaml:"script"`
	Timeout          string                  `yaml:"timeout"`
	Resources        *model.SandboxResources `yaml:"resources"`
	NetworkPolicy    *model.NetworkPolicy    `yaml:"network_policy"`
	Workspace        string                  `yaml:"workspace"`
	OutputSchema     map[string]any          `yaml:"output_schema"`
}

// nodeNewFormat is the top-level structure for the new kind+spec node format.
type nodeNewFormat struct {
	ID          string            `yaml:"id"`
	Kind        string            `yaml:"kind"`
	Instruction string            `yaml:"instruction"`
	Solution    string            `yaml:"solution"`
	Spec        nodeSpecNewFormat `yaml:"spec"`
}

// UnmarshalYAML implements yaml.Unmarshaler for backward-compatible node parsing.
// Old flat format: {id, type, tool, input, instruction, ...}
// New kind+spec format: {id, kind, spec: {tool, args, ...}, instruction, solution}
func (n *Node) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.MappingNode {
		for i := 0; i < len(value.Content)-1; i += 2 {
			if value.Content[i].Value == "spec" {
				return n.unmarshalNewFormat(value)
			}
		}
	}
	return n.unmarshalOldFormat(value)
}

func (n *Node) unmarshalNewFormat(value *yaml.Node) error {
	var nf nodeNewFormat
	if err := value.Decode(&nf); err != nil {
		return err
	}
	n.ID = nf.ID
	n.Kind = NodeType(nf.Kind)
	n.Type = NodeType(nf.Kind)
	n.Instruction = nf.Instruction
	n.Solution = nf.Solution

	s := nf.Spec
	n.Tool = s.Tool
	n.Agent = s.Agent
	n.Skill = s.Skill
	n.Source = s.Source
	n.Expression = s.Expression
	n.Input = s.Args
	n.Args = s.Args
	n.Strategy = s.Strategy
	n.Retry = s.Retry
	n.Concurrency = s.Concurrency
	n.Approval = s.Approval
	n.SupervisorConfig = s.SupervisorConfig
	n.AgentFlowID = s.AgentFlowID
	n.Runtime = s.Runtime
	n.Script = s.Script
	n.Timeout = s.Timeout
	n.Resources = s.Resources
	n.NetworkPolicy = s.NetworkPolicy
	n.Workspace = s.Workspace
	n.OutputSchema = s.OutputSchema
	return nil
}

func (n *Node) unmarshalOldFormat(value *yaml.Node) error {
	type plain Node
	var p plain
	if err := value.Decode(&p); err != nil {
		return err
	}
	*n = Node(p)
	if n.Kind == "" {
		n.Kind = n.Type
	}
	return nil
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
