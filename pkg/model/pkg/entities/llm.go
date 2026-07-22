package entities

// LlmProviderInfo is a persisted LLM provider definition.
// DB column mapping uses db tags to align with the llm_providers table schema.
type LlmProviderInfo struct {
	BaseEntity

	Provider    string            `json:"provider" yaml:"provider" db:"provider"`
	Enabled     bool              `json:"enabled" yaml:"enabled" db:"-"`
	Status      string            `json:"status" yaml:"status" db:"status"`
	Timeout     string            `json:"timeout" yaml:"timeout" db:"-"`
	TimeoutMs   int               `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty" db:"timeout_ms"`
	Endpoint    string            `json:"endpoint" yaml:"endpoint" db:"endpoint"`
	Credentials map[string]any    `json:"credentials" yaml:"credentials" db:"-"`
	Proxy       string            `json:"proxy,omitempty" yaml:"proxy,omitempty" db:"-"`
	RateLimit   int               `json:"rate_limit" yaml:"rate_limit" db:"-"`
	DefaultModel string           `json:"defaultModel" yaml:"defaultModel" db:"model"`
	Models      []LlmModelInfo    `json:"models" yaml:"models" db:"models"`
	ApiKey      string            `json:"apikey" yaml:"apikey" db:"apikey"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
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
