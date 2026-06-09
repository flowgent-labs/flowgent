package llm

import (
	"context"
	"fmt"
	"sync"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/model/pkg"
)

// ILlmProvider is the interface each LLM provider implementation must satisfy.
type ILlmProvider interface {
	Generate(ctx context.Context, systemPrompt, userPrompt, modelName string, temperature float64) (string, error)
}

// LlmProviderLoader loads DB-backed LLM provider definitions via apiserver.
type LlmProviderLoader interface {
	ListProviders(ctx context.Context) ([]*model.LlmProvider, error)
}

// LlmProviderManager loads and manages LLM provider instances from static
// config and a ProviderLoader (standard mode), and routes Generate calls.
type LlmProviderManager struct {
	mu        sync.Mutex
	providers map[string]ILlmProvider
}

// NewLlmProviderManager creates a manager and loads all configured providers.
func NewLlmProviderManager(cfg *config.LLMConfig, loader LlmProviderLoader) *LlmProviderManager {
	m := &LlmProviderManager{providers: make(map[string]ILlmProvider)}
	if cfg == nil {
		return m
	}

	for _, p := range cfg.Providers.Static {
		if !p.Enabled || p.ID == "" {
			continue
		}
		m.registerStatic(p)
	}

	if cfg.Providers.Standard.Enabled && loader != nil {
		dbProviders, err := loader.ListProviders(context.Background())
		if err == nil {
			for _, dbp := range dbProviders {
				if !dbp.Enabled || dbp.ID == "" {
					continue
				}
				m.registerDB(*dbp)
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

func newProvider(p config.LLMProviderDef) ILlmProvider {
	switch p.Type {
	case "anthropic":
		return newAnthropicProvider(p)
	case "gemini":
		return newGeminiProvider(p)
	default:
		return newOpenAIProvider(p)
	}
}

func newProviderFromModel(p model.LlmProvider) ILlmProvider {
	apiKey := ""
	if v, ok := p.Credentials["apikey"]; ok {
		if vs, ok := v.(string); ok {
			apiKey = vs
		}
	}
	def := config.LLMProviderDef{
		ID:        p.ID,
		Type:      p.Type,
		Enabled:   p.Enabled,
		Timeout:   p.Timeout,
		Endpoint:  p.Endpoint,
		RateLimit: p.RateLimit,
		Proxy:     p.Proxy,
		ApiKey:    apiKey,
		Models:    nil,
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
