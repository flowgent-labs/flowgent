// Package model defines the shared domain types for the Flowgent engine.
//
// File: agentflow.go — Flow spec and scheduling metadata consumed by API Server, Controller.
//
//	AgentFlowSpec, TriggerDef, AgentFlowDefinition (YAML), AgentFlowVersion,
//	ExecutionMode, Priority.
package model

import (
	"time"

	"gopkg.in/yaml.v3"
)

// ─── Scheduling metadata ─────────────────────────────────────

// ExecutionMode defines whether a flow runs on a shared cluster or a dedicated one.
type ExecutionMode string

const (
	ModeSession     ExecutionMode = "session"
	ModeApplication ExecutionMode = "application"
)

// Priority defines the scheduling precedence of an agentflow.
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
	PriorityGrade  Priority = "grade" // application mode — dedicated JM+TM cluster
)

// IsApplication returns true if the priority demands a dedicated cluster.
func (p Priority) IsApplication() bool { return p == PriorityGrade }

// ─── AgentFlow spec ──────────────────────────────────────────

// AgentFlowSpec contains the full specification of an agentflow (nodes, edges, triggers, etc.).
type AgentFlowSpec struct {
	ID           string         `json:"id" yaml:"id"`
	Kind         string         `json:"kind,omitempty" yaml:"kind,omitempty"` // "skill" | "" (regular flow)
	Description  string         `json:"description,omitempty" yaml:"description,omitempty"`
	Summary      string         `json:"summary,omitempty" yaml:"summary,omitempty"` // one-liner for A2A card
	InputSchema  map[string]any `json:"input_schema,omitempty" yaml:"input_schema,omitempty"`
	OutputSchema map[string]any `json:"output_schema,omitempty" yaml:"output_schema,omitempty"`
	Vars         map[string]any `json:"vars,omitempty" yaml:"vars,omitempty"`
	Nodes        []Node         `json:"nodes" yaml:"nodes"`
	Edges        []Edge         `json:"edges" yaml:"edges"`
	Triggers     []TriggerDef   `json:"triggers,omitempty" yaml:"triggers,omitempty"`

	// Per-flow sandbox policy override (nil fields inherit from global config).
	SandboxPolicy *SandboxPolicyOverride `json:"sandbox_policy,omitempty" yaml:"sandbox_policy,omitempty"`

	// Multi-tenant & scheduling metadata
	Priority  Priority          `json:"priority,omitempty" yaml:"priority,omitempty"`
	TenantID  string            `json:"tenant_id,omitempty" yaml:"tenant_id,omitempty"`
	Namespace string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Mode      ExecutionMode     `json:"mode,omitempty" yaml:"mode,omitempty"`
	Labels    map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// EffectiveMode returns the execution mode, deriving from priority if not explicitly set.
func (s *AgentFlowSpec) EffectiveMode() ExecutionMode {
	if s.Mode != "" {
		return s.Mode
	}
	if s.Priority.IsApplication() {
		return ModeApplication
	}
	return ModeSession
}

// TriggerDef defines a trigger for an agentflow (schedule or webhook).
type TriggerDef struct {
	Type     string   `json:"type" yaml:"type"`
	Cron     string   `json:"cron,omitempty" yaml:"cron,omitempty"`
	Provider string   `json:"provider,omitempty" yaml:"provider,omitempty"`
	Events   []string `json:"events,omitempty" yaml:"events,omitempty"`
}

// ─── YAML loading ────────────────────────────────────────────

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

// ─── Versioning ──────────────────────────────────────────────

// AgentFlowVersion represents a versioned agentflow stored in the database.
type AgentFlowVersion struct {
	AgentFlowID string    `json:"agentflow_id"`
	Version     int64     `json:"version"`
	Definition  []byte    `json:"definition"`
	CreatedBy   string    `json:"created_by,omitempty"`
	Comment     string    `json:"comment,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}
