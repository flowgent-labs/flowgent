package entities

import (
	"github.com/flowgent-labs/flowgent/model/pkg"
	"gopkg.in/yaml.v3"
)

// AgentFlowInfo contains the full specification of an agentflow (nodes, edges, triggers, etc.).
type AgentFlowInfo struct {
	BaseEntity

	Kind          string                 `json:"kind,omitempty" yaml:"kind,omitempty"`
	Summary       string                 `json:"summary,omitempty" yaml:"summary,omitempty"`
	InputSchema   map[string]any         `json:"input_schema,omitempty" yaml:"input_schema,omitempty"`
	OutputSchema  map[string]any         `json:"output_schema,omitempty" yaml:"output_schema,omitempty"`
	Vars          map[string]any         `json:"vars,omitempty" yaml:"vars,omitempty"`
	Nodes         []Node                 `json:"nodes" yaml:"nodes"`
	Edges         []Edge                 `json:"edges" yaml:"edges"`
	Triggers      []TriggerDef           `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	SandboxPolicy *model.SandboxPolicyOverride `json:"sandbox_policy,omitempty" yaml:"sandbox_policy,omitempty"`

	Priority    Priority          `json:"priority,omitempty" yaml:"priority,omitempty"`
	Namespace   string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Mode        ExecutionMode     `json:"mode,omitempty" yaml:"mode,omitempty"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Credentials map[string]string `json:"credentials,omitempty" yaml:"credentials,omitempty"`
}

func (s *AgentFlowInfo) EffectiveMode() ExecutionMode {
	return ModeApplication
}

// TriggerDef defines a trigger for an agentflow (schedule or webhook).
type TriggerDef struct {
	Type     string   `json:"type" yaml:"type"`
	Cron     string   `json:"cron,omitempty" yaml:"cron,omitempty"`
	Provider string   `json:"provider,omitempty" yaml:"provider,omitempty"`
	Events   []string `json:"events,omitempty" yaml:"events,omitempty"`
}

// AgentFlowDefinition is a top-level YAML wrapper supporting flat and nested formats.
type AgentFlowDefinition struct {
	AgentFlow *AgentFlowInfo `json:"agentflow" yaml:"-"`
}

func (d *AgentFlowDefinition) UnmarshalYAML(value *yaml.Node) error {
	var raw map[string]any
	if err := value.Decode(&raw); err != nil {
		return err
	}
	if _, ok := raw["agentflow"]; ok {
		var nested struct {
			AgentFlow AgentFlowInfo `yaml:"agentflow"`
		}
		if err := value.Decode(&nested); err != nil {
			return err
		}
		d.AgentFlow = &nested.AgentFlow
		return nil
	}
	var spec AgentFlowInfo
	if err := value.Decode(&spec); err != nil {
		return err
	}
	d.AgentFlow = &spec
	return nil
}

// AgentFlowVersionInfo represents a versioned agentflow stored in the database.
type AgentFlowVersionInfo struct {
	BaseEntity

	AgentFlowID string `json:"agentflow_id"`
	Version     int64  `json:"version"`
	Definition  []byte `json:"definition"`
	Comment     string `json:"comment,omitempty"`
}
