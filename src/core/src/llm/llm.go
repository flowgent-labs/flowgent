package llm

import (
	"context"
	"fmt"
	"sync"

	"github.com/flowgent-labs/flowgent/config/src"
	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
)

// ILlmProvider is the interface each LLM provider implementation must satisfy.
type ILlmProvider interface {
	// Generate sends a chat completion and returns the response text.
	Generate(ctx context.Context, systemPrompt, userPrompt, modelName string, temperature float64) (string, error)
}

// LlmProviderManager loads and manages LLM provider instances from static
// config and DB (standard mode), and routes Generate calls to the correct provider.
type LlmProviderManager struct {
	mu        sync.Mutex
	providers map[string]ILlmProvider // providerID → instance
}

// NewLlmProviderManager creates a manager and loads all configured providers.
func NewLlmProviderManager(cfg *config.LLMConfig, store store.Store) *LlmProviderManager {
	m := &LlmProviderManager{providers: make(map[string]ILlmProvider)}
	if cfg == nil {
		return m
	}

	// Load static providers from YAML config
	for _, p := range cfg.Providers.Static {
		if !p.Enabled || p.ID == "" {
			continue
		}
		m.registerStatic(p)
	}

	// Load DB-backed providers (standard mode)
	if cfg.Providers.Standard.Enabled && store != nil {
		dbProviders, err := store.ListLlmProviders(context.Background(), "")
		if err == nil {
			for _, dbp := range dbProviders {
				if !dbp.Enabled || dbp.ID == "" {
					continue
				}
				m.registerDB(dbp)
			}
		}
	}

	return m
}

func (m *LlmProviderManager) registerStatic(p config.LLMProviderDef) {
	pc := newProvider(p)
	if pc != nil {
		m.providers[p.ID] = pc
	}
}

func (m *LlmProviderManager) registerDB(dbP model.LlmProvider) {
	pc := newProviderFromModel(dbP)
	if pc != nil {
		m.providers[dbP.ID] = pc
	}
}

// newProvider creates the correct ILlmProvider from a static config definition.
func newProvider(p config.LLMProviderDef) ILlmProvider {
	switch p.Type {
	case "anthropic":
		return newAnthropicProvider(p)
	case "gemini":
		return newGeminiProvider(p)
	default: // "openai", "dashscope", "deepseek", "" → OpenAI-compatible
		return newOpenAIProvider(p)
	}
}

// newProviderFromModel creates the correct ILlmProvider from a DB model.
func newProviderFromModel(p model.LlmProvider) ILlmProvider {
	def := config.LLMProviderDef{
		ID:          p.ID,
		Type:        p.Type,
		Enabled:     p.Enabled,
		Timeout:     p.Timeout,
		Endpoint:    p.Endpoint,
		RateLimit:   p.RateLimit,
		Proxy:       p.Proxy,
		Models:      nil,
		Credentials: make(map[string]string),
	}
	for k, v := range p.Credentials {
		if vs, ok := v.(string); ok {
			def.Credentials[k] = vs
		}
	}
	return newProvider(def)
}

// Generate routes the request to the appropriate provider instance.
func (m *LlmProviderManager) Generate(ctx context.Context, systemPrompt, userPrompt, providerModel string, temperature float64) (string, error) {
	providerID, modelName := resolveProvider(providerModel, m.providers)
	pc, ok := m.providers[providerID]
	if !ok {
		return "", fmt.Errorf("llm provider not found: %s", providerID)
	}
	return pc.Generate(ctx, systemPrompt, userPrompt, modelName, temperature)
}

func resolveProvider(providerModel string, providers map[string]ILlmProvider) (providerID, modelName string) {
	providerID, modelName = "default", providerModel
	for id := range providers {
		if len(providerModel) > len(id) && providerModel[:len(id)] == id {
			providerID = id
			if len(providerModel) > len(id)+1 && providerModel[len(id)] == '/' {
				modelName = providerModel[len(id)+1:]
			}
			break
		}
	}
	return
}
