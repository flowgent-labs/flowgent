package entities

import (
	"github.com/flowgent-labs/flowgent/model/pkg"
	"gopkg.in/yaml.v3"
)

// FlowInfo contains the full specification of a flow (nodes, edges, triggers, etc.).
type FlowInfo struct {
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

func (s *FlowInfo) EffectiveMode() ExecutionMode {
	return ModeApplication
}

// TriggerDef defines a trigger for a flow (schedule or webhook).
type TriggerDef struct {
	Type     string   `json:"type" yaml:"type"`
	Cron     string   `json:"cron,omitempty" yaml:"cron,omitempty"`
	Provider string   `json:"provider,omitempty" yaml:"provider,omitempty"`
	Events   []string `json:"events,omitempty" yaml:"events,omitempty"`
}

// FlowDefinition is a top-level YAML wrapper supporting flat and K8s-style (name/kind/spec) formats.
type FlowDefinition struct {
	Flow *FlowInfo `json:"flow" yaml:"-"`
}

func (d *FlowDefinition) UnmarshalYAML(value *yaml.Node) error {
	var raw map[string]any
	if err := value.Decode(&raw); err != nil {
		return err
	}
	// K8s-style: {name, kind, spec: {...}}
	if _, ok := raw["spec"]; ok {
		var nested struct {
			Name string   `yaml:"name"`
			Kind string   `yaml:"kind"`
			Spec FlowInfo `yaml:"spec"`
		}
		if err := value.Decode(&nested); err != nil {
			return err
		}
		nested.Spec.Kind = nested.Kind
		if nested.Spec.ID == "" {
			nested.Spec.ID = nested.Name
		}
		d.Flow = &nested.Spec
		return nil
	}
	// Legacy nested: {agentflow: {...}} or {flow: {...}}
	if _, ok := raw["agentflow"]; ok {
		var nested struct {
			AgentFlow FlowInfo `yaml:"agentflow"`
		}
		if err := value.Decode(&nested); err != nil {
			return err
		}
		d.Flow = &nested.AgentFlow
		return nil
	}
	if _, ok := raw["flow"]; ok {
		var nested struct {
			Flow FlowInfo `yaml:"flow"`
		}
		if err := value.Decode(&nested); err != nil {
			return err
		}
		d.Flow = &nested.Flow
		return nil
	}
	// Flat format
	var spec FlowInfo
	if err := value.Decode(&spec); err != nil {
		return err
	}
	d.Flow = &spec
	return nil
}

// FlowVersionInfo represents a versioned flow stored in the database.
type FlowVersionInfo struct {
	BaseEntity

	FlowID     string `json:"flow_id" db:"agentflow_id"`
	Version    int64  `json:"version"`
	Definition []byte `json:"definition"`
	Comment    string `json:"comment,omitempty"`
}
