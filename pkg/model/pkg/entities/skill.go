package entities

// SkillInfo is a reusable skill definition backed by the llm_skill table.
type SkillInfo struct {
	BaseEntity

	Name        string   `json:"name" yaml:"name"`
	Instruction string   `json:"instruction" yaml:"instruction"`
	Model       string   `json:"model,omitempty" yaml:"model,omitempty"`
	Temperature *float64 `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	MaxTokens   int      `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
	Tools       []string `json:"tools,omitempty" yaml:"tools,omitempty"`
}
