package entities

import "time"

// AgentInfo is the definition of an AI agent persona.
type AgentInfo struct {
	Name         string         `json:"name" yaml:"name"`
	Model        string         `json:"model" yaml:"model"`
	Soul         string         `json:"soul" yaml:"soul"`
	Instruction  string         `json:"instruction" yaml:"instruction"`
	OutputSchema map[string]any `json:"output_schema,omitempty" yaml:"output_schema,omitempty"`
	Temperature  *float64       `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	MaxTokens    int            `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
	TenantID     string         `json:"tenant_id,omitempty" yaml:"tenant_id,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}
