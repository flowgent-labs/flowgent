package entities

// SkillInfo is a reusable skill definition backed by the llm_skill table.
type SkillInfo struct {
	BaseEntity

	Name        string      `json:"name" yaml:"name"`
	Instruction string      `json:"instruction" yaml:"instruction"`
	Model       string      `json:"model,omitempty" yaml:"model,omitempty"`
	Temperature *float64    `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	MaxTokens   int         `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
	Tools       []string    `json:"tools,omitempty" yaml:"tools,omitempty"`
	Version     int64       `json:"version" yaml:"version"`
	Assets      []SkillFile `json:"assets" db:"-"`
	Scripts     []SkillFile `json:"scripts" db:"-"`
}

// SkillFile is metadata for a workspace file belonging to a SkillInfo. File
// content never enters the metadata database or a list response.
type SkillFile struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}
