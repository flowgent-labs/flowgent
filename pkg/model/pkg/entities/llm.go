package entities

import (
	"fmt"
	"time"
)

// LlmProviderInfo is a persisted LLM provider definition.
// DB column mapping uses db tags to align with the llm_providers table schema.
type LlmProviderInfo struct {
	BaseEntity

	Name          string            `json:"name" yaml:"name"`
	Type          string            `json:"type" yaml:"type"`
	Provider      string            `json:"-" yaml:"-" db:"-"` // deprecated runtime alias
	Enabled       bool              `json:"enabled" yaml:"enabled" db:"-"`
	Status        string            `json:"status" yaml:"status"`
	Timeout       string            `json:"timeout" yaml:"timeout" db:"-"`
	TimeoutMs     int               `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	BaseURI       string            `json:"base_uri" yaml:"base_uri" db:"base_uri"`
	Endpoint      string            `json:"-" yaml:"-" db:"-"` // deprecated runtime alias
	Proxy         string            `json:"proxy,omitempty" yaml:"proxy,omitempty" db:"-"`
	RateLimit     int               `json:"rate_limit" yaml:"rate_limit" db:"-"`
	DefaultModel  string            `json:"default_model" yaml:"default_model" db:"default_model"`
	Models        []LlmModelInfo    `json:"models" yaml:"models"`
	CredentialRef string            `json:"credential_ref,omitempty" yaml:"credential_ref,omitempty"`
	ApiKey        string            `json:"-" yaml:"-" db:"-"`
	ApiKeyEnv     string            `json:"api_key_env,omitempty" yaml:"api_key_env,omitempty" db:"-"`
	KeyConfigured bool              `json:"key_configured" yaml:"-" db:"-"`
	Env           map[string]string `json:"-" yaml:"-" db:"env"`
	EnvRefs       map[string]string `json:"env_refs,omitempty" yaml:"env_refs,omitempty" db:"-"`
	Labels        map[string]string `json:"labels,omitempty" yaml:"labels,omitempty" db:"-"`
}

func (p *LlmProviderInfo) NormalizeAliases() {
	if p.Name == "" {
		p.Name = p.Provider
	}
	if p.Provider == "" {
		p.Provider = p.Name
	}
	if p.BaseURI == "" {
		p.BaseURI = p.Endpoint
	}
	if p.Endpoint == "" {
		p.Endpoint = p.BaseURI
	}
	if p.TimeoutMs <= 0 {
		if duration, err := time.ParseDuration(p.Timeout); err == nil && duration > 0 {
			p.TimeoutMs = int(duration / time.Millisecond)
		}
		if p.TimeoutMs <= 0 {
			p.TimeoutMs = 30_000
		}
	}
	if p.Timeout == "" {
		p.Timeout = fmt.Sprintf("%dms", p.TimeoutMs)
	}
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
