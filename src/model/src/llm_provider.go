package model

import "time"

// LlmProvider is a persisted LLM provider definition, manageable via REST API
// when orchestration.llm.providers.standard.enabled=true.
type LlmProvider struct {
	ID          string         `json:"id" yaml:"id"`
	Type        string         `json:"type" yaml:"type"`
	Enabled     bool           `json:"enabled" yaml:"enabled"`
	Endpoint    string         `json:"endpoint" yaml:"endpoint"`
	Credentials map[string]any `json:"credentials" yaml:"credentials"`
	Proxy       string         `json:"proxy,omitempty" yaml:"proxy,omitempty"`
	RateLimit   int            `json:"rate_limit" yaml:"rate_limit"`
	Models      []LlmModelDef  `json:"models" yaml:"models"`
	TenantID    string         `json:"tenant_id,omitempty" yaml:"tenant_id,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// LlmModelDef is the model-level definition within a persisted LLM provider.
type LlmModelDef struct {
	Name        string         `json:"name" yaml:"name"`
	Temperature float64        `json:"temperature" yaml:"temperature"`
	TopK        int            `json:"topk" yaml:"topk"`
	Modalities  map[string]any `json:"modalities,omitempty" yaml:"modalities,omitempty"`
	Thinking    map[string]any `json:"thinking,omitempty" yaml:"thinking,omitempty"`
}
