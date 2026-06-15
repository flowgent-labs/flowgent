package entities

import "time"

// SkillInfo is a reusable skill definition backed by the llm_skill table.
type SkillInfo struct {
	Name        string         `json:"name" yaml:"name"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
	Instruction string         `json:"instruction" yaml:"instruction"`
	Model       string         `json:"model,omitempty" yaml:"model,omitempty"`
	Temperature *float64       `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	MaxTokens   int            `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
	Tools       []string       `json:"tools,omitempty" yaml:"tools,omitempty"`
	TenantID    string         `json:"tenant_id,omitempty" yaml:"tenant_id,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}
