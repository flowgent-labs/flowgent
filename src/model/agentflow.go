package model

import (
	"time"

	"gopkg.in/yaml.v3"
)

// Node represents a single node in the agentflow DAG.
type Node struct {
	ID               string              `json:"id" yaml:"id"`
	Type             NodeType            `json:"type" yaml:"type"`
	Agent            string              `json:"agent,omitempty" yaml:"agent,omitempty"`
	Skill            string              `json:"skill,omitempty" yaml:"skill,omitempty"`
	Tool             string              `json:"tool,omitempty" yaml:"tool,omitempty"`
	Source           string              `json:"source,omitempty" yaml:"source,omitempty"`
	Expression       string              `json:"expression,omitempty" yaml:"expression,omitempty"`
	Instruction      string              `json:"instruction,omitempty" yaml:"instruction,omitempty"`
	Strategy         map[string]any      `json:"strategy,omitempty" yaml:"strategy,omitempty"`
	Input            map[string]any      `json:"input,omitempty" yaml:"input,omitempty"`
	Retry            *RetryPolicy        `json:"retry,omitempty" yaml:"retry,omitempty"`
	Node             *Node               `json:"node,omitempty" yaml:"node,omitempty"`
	Concurrency      int                 `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	Approval         *HumanApprovalConfig `json:"approval,omitempty" yaml:"approval,omitempty"`
	SupervisorConfig *SupervisorConfig   `json:"supervisor_config,omitempty" yaml:"supervisor_config,omitempty"`
	AgentFlowID      string              `json:"agentflow,omitempty" yaml:"agentflow,omitempty"`
	// Sandbox fields
	Runtime   string            `json:"runtime,omitempty" yaml:"runtime,omitempty"`     // python3 | bash | node
	Script    string            `json:"script,omitempty" yaml:"script,omitempty"`       // inline script
	Timeout   string            `json:"timeout,omitempty" yaml:"timeout,omitempty"`     // e.g. "120s"
	Resources *SandboxResources `json:"resources,omitempty" yaml:"resources,omitempty"` // cpu/memory limits
}

// SandboxResources defines resource limits for a sandbox node.
type SandboxResources struct {
	CPU    string `json:"cpu,omitempty" yaml:"cpu,omitempty"`       // e.g. "500m"
	Memory string `json:"memory,omitempty" yaml:"memory,omitempty"` // e.g. "256Mi"
}

// RetryPolicy defines the retry behavior for a node.
type RetryPolicy struct {
	Max      int           `json:"max" yaml:"max"`
	Initial  time.Duration `json:"initial" yaml:"initial"`
	MaxDelay time.Duration `json:"max_delay" yaml:"max_delay"`
	Factor   float64       `json:"factor" yaml:"factor"`
}

// HumanApprovalConfig defines the approval gate configuration for human nodes.
type HumanApprovalConfig struct {
	Timeout   time.Duration `json:"timeout" yaml:"timeout"`
	OnApprove string        `json:"on_approve" yaml:"on_approve"`
	OnReject  string        `json:"on_reject" yaml:"on_reject"`
}

// SupervisorConfig defines the constraints for supervisor nodes.
type SupervisorConfig struct {
	MaxRetries     int      `json:"max_retries" yaml:"max_retries"`
	MaxNodes       int      `json:"max_nodes" yaml:"max_nodes"`
	MaxInjections  int      `json:"max_injections" yaml:"max_injections"`
	AllowedActions []string `json:"allowed_actions" yaml:"allowed_actions"`
}

// Edge represents a directed edge in the DAG, with an optional condition for branching.
type Edge struct {
	From      string `json:"from" yaml:"from"`
	To        string `json:"to" yaml:"to"`
	Condition *bool  `json:"condition,omitempty" yaml:"condition,omitempty"`
}

// AgentFlowSpec contains the full specification of an agentflow (nodes, edges, triggers, etc.).
type AgentFlowSpec struct {
	ID           string         `json:"id" yaml:"id"`
	Kind         string         `json:"kind,omitempty" yaml:"kind,omitempty"` // "skill" | "" (regular flow)
	Description  string         `json:"description,omitempty" yaml:"description,omitempty"`
	Summary      string         `json:"summary,omitempty" yaml:"summary,omitempty"`       // one-liner for A2A card
	InputSchema  map[string]any `json:"input_schema,omitempty" yaml:"input_schema,omitempty"`   // optional JSON Schema
	OutputSchema map[string]any `json:"output_schema,omitempty" yaml:"output_schema,omitempty"` // optional JSON Schema
	Vars         map[string]any `json:"vars,omitempty" yaml:"vars,omitempty"`
	Nodes        []Node         `json:"nodes" yaml:"nodes"`
	Edges        []Edge         `json:"edges" yaml:"edges"`
	Triggers     []TriggerDef   `json:"triggers,omitempty" yaml:"triggers,omitempty"`
}

// TriggerDef defines a trigger for an agentflow (schedule or webhook).
type TriggerDef struct {
	Type     string   `json:"type" yaml:"type"`
	Cron     string   `json:"cron,omitempty" yaml:"cron,omitempty"`
	Provider string   `json:"provider,omitempty" yaml:"provider,omitempty"`
	Events   []string `json:"events,omitempty" yaml:"events,omitempty"`
}

// AgentFlowDefinition is a top-level wrapper that supports both flat and nested YAML formats.
type AgentFlowDefinition struct {
	AgentFlow *AgentFlowSpec `json:"agentflow" yaml:"-"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface to support both flat
// and nested ("agentflow:" key) YAML formats.
func (d *AgentFlowDefinition) UnmarshalYAML(value *yaml.Node) error {
	var raw map[string]any
	if err := value.Decode(&raw); err != nil {
		return err
	}

	if _, ok := raw["agentflow"]; ok {
		var nested struct {
			AgentFlow AgentFlowSpec `yaml:"agentflow"`
		}
		if err := value.Decode(&nested); err != nil {
			return err
		}
		d.AgentFlow = &nested.AgentFlow
		return nil
	}

	var spec AgentFlowSpec
	if err := value.Decode(&spec); err != nil {
		return err
	}
	d.AgentFlow = &spec
	return nil
}

// AgentFlowVersion represents a versioned agentflow stored in the database.
type AgentFlowVersion struct {
	AgentFlowID string    `json:"agentflow_id"`
	Version     int64     `json:"version"`
	Definition  []byte    `json:"definition"`
	CreatedBy   string    `json:"created_by,omitempty"`
	Comment     string    `json:"comment,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}
