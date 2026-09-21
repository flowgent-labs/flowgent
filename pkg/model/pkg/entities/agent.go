package entities

// AgentInfo is the definition of an AI agent persona.
type AgentInfo struct {
	BaseEntity

	Name         string            `json:"name" yaml:"name"`
	Model        string            `json:"model" yaml:"model"`
	Soul         string            `json:"soul" yaml:"soul"`
	Instruction  string            `json:"instruction" yaml:"instruction"`
	InputSchema  map[string]any    `json:"input_schema,omitempty" yaml:"input_schema,omitempty"`
	OutputSchema map[string]any    `json:"output_schema,omitempty" yaml:"output_schema,omitempty"`
	Version      int64             `json:"version" yaml:"version"`
	Temperature  *float64          `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	MaxTokens    int               `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
	Labels       map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}
