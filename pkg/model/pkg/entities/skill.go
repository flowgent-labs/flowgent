package entities

import "time"

// SkillInfo is a reusable skill definition backed by the llm_skill table.
type SkillInfo struct {
	BaseEntity

	Name        string      `json:"name" yaml:"name"`
	Instruction string      `json:"instruction" yaml:"instruction"`
	Model       string      `json:"model,omitempty" yaml:"model,omitempty"`
	Temperature *float64    `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	MaxTokens   int         `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
	Tools       []string    `json:"tools,omitempty" yaml:"tools,omitempty"`
	Revision    int64       `json:"revision" yaml:"revision" db:"-"`
	Version     int64       `json:"-" yaml:"-" db:"-"` // deprecated runtime alias
	Assets      []SkillFile `json:"assets" db:"-"`
	Scripts     []SkillFile `json:"scripts" db:"-"`
}

// SkillFile is metadata for a workspace file belonging to a SkillInfo. File
// content never enters the metadata database or a list response.
type SkillFile struct {
	ID           string         `json:"id,omitempty"`
	Kind         string         `json:"kind"`
	RelativePath string         `json:"relative_path"`
	MediaType    string         `json:"media_type"`
	SizeBytes    int64          `json:"size_bytes"`
	ContentHash  string         `json:"content_hash"`
	CreatedAt    time.Time      `json:"created_at"`
	CreatedBy    string         `json:"created_by,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}
