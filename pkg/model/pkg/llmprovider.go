package model

import "time"

// ── Persisted LLM provider (DB model) ──────────────────────────

// LlmProvider is a persisted LLM provider definition, manageable via REST API
// when orchestration.llm.providers.standard.enabled=true.
type LlmProvider struct {
	ID          string         `json:"id" yaml:"id"`
	Type        string         `json:"type" yaml:"type"`
	Enabled     bool           `json:"enabled" yaml:"enabled"`
	Timeout     string         `json:"timeout" yaml:"timeout"`
	Endpoint    string         `json:"endpoint" yaml:"endpoint"`
	Credentials map[string]any `json:"credentials" yaml:"credentials"`
	Proxy       string         `json:"proxy,omitempty" yaml:"proxy,omitempty"`
	RateLimit   int            `json:"rate_limit" yaml:"rate_limit"`
	Models      []LlmModelDef  `json:"models" yaml:"models"`
	TenantID    string         `json:"tenant_id,omitempty" yaml:"tenant_id,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`

	// ApiKey is resolved from Credentials at load time (not persisted, not in JSON).
	ApiKey string `json:"-" yaml:"-"`
}

// LlmModelDef is the model-level definition within a persisted LLM provider.
type LlmModelDef struct {
	Name        string            `json:"name" yaml:"name"`
	Temperature float64           `json:"temperature" yaml:"temperature"`
	TopK        int               `json:"topk" yaml:"topk"`
	Modalities  *ModalitiesConfig `json:"modalities,omitempty" yaml:"modalities,omitempty"`
	Thinking    *ThinkingConfig   `json:"thinking,omitempty" yaml:"thinking,omitempty"`
}

// ── Model modality & thinking config ────────────────────────────

// ModalitiesConfig supports the nested YAML/JSON format:
//
//	modalities:
//	  input: [text]
//	  output: [text]
type ModalitiesConfig struct {
	Input  []string `json:"input" yaml:"input"`
	Output []string `json:"output" yaml:"output"`
}

// ThinkingConfig configures extended thinking (e.g. Anthropic extended reasoning).
type ThinkingConfig struct {
	Type         string `json:"type" yaml:"type"`
	BudgetTokens int    `json:"budget_tokens" yaml:"budget_tokens"`
}
