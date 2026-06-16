package entities

// LlmProviderInfo is a persisted LLM provider definition.
type LlmProviderInfo struct {
	BaseEntity

	Type        string         `json:"type" yaml:"type"`
	Enabled     bool           `json:"enabled" yaml:"enabled"`
	Timeout     string         `json:"timeout" yaml:"timeout"`
	Endpoint    string         `json:"endpoint" yaml:"endpoint"`
	Credentials map[string]any `json:"credentials" yaml:"credentials"`
	Proxy       string         `json:"proxy,omitempty" yaml:"proxy,omitempty"`
	RateLimit   int            `json:"rate_limit" yaml:"rate_limit"`
	Models      []LlmModelInfo `json:"models" yaml:"models"`
	ApiKey      string         `json:"-" yaml:"-"`
}

// LlmModelInfo is the model-level definition within a persisted LLM provider.
type LlmModelInfo struct {
	Name        string            `json:"name" yaml:"name"`
	Temperature float64           `json:"temperature" yaml:"temperature"`
	TopK        int               `json:"topk" yaml:"topk"`
	Modalities  *ModalitiesConfig `json:"modalities,omitempty" yaml:"modalities,omitempty"`
	Thinking    *ThinkingConfig   `json:"thinking,omitempty" yaml:"thinking,omitempty"`
}

// ModalitiesConfig supports the nested YAML/JSON format.
type ModalitiesConfig struct {
	Input  []string `json:"input" yaml:"input"`
	Output []string `json:"output" yaml:"output"`
}

// ThinkingConfig configures extended thinking.
type ThinkingConfig struct {
	Type         string `json:"type" yaml:"type"`
	BudgetTokens int    `json:"budget_tokens" yaml:"budget_tokens"`
}
