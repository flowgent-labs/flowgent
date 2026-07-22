package llm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ILlmProvider is the interface each LLM provider implementation must satisfy.
type ILlmProvider interface {
	Generate(ctx context.Context, systemPrompt, userPrompt, modelName string, temperature float64) (string, error)
}

// LlmProviderLoader loads DB-backed LLM provider definitions via apiserver.
type LlmProviderLoader interface {
	ListProviders(ctx context.Context) ([]*entities.LlmProviderInfo, error)
}

// LlmProviderManager loads and manages LLM provider instances from the
// database via LlmProviderLoader (standard mode), and routes Generate calls.
type LlmProviderManager struct {
	mu        sync.Mutex
	providers map[string]ILlmProvider
}

// NewLlmProviderManager creates a manager and loads providers from DB via the
// standard (apiserver-backed) loader.
func NewLlmProviderManager(loader LlmProviderLoader) *LlmProviderManager {
	m := &LlmProviderManager{providers: make(map[string]ILlmProvider)}

	if loader != nil {
		dbProviders, err := loader.ListProviders(context.Background())
		if err == nil {
			for _, dbp := range dbProviders {
				slog.Debug("llm loaded provider", "id", dbp.ID, "provider", dbp.Provider, "status", dbp.Status, "apiKeyLen", len(dbp.ApiKey))
				if dbp.Status != "ACTIVE" || dbp.ID == "" {
					slog.Debug("llm skip provider", "id", dbp.ID, "status", dbp.Status)
					continue
				}
				m.registerDB(*dbp)
			}
			slog.Debug("llm registered providers in map", "count", len(m.providers))
		} else {
			slog.Warn("llm ListProviders failed", "err", err)
		}
	}

	return m
}

func (m *LlmProviderManager) registerDB(dbP entities.LlmProviderInfo) {
	// DB stores apikey in the apikey column directly.
	// Also check Credentials map for backward compatibility.
	if dbP.ApiKey == "" {
		if v, ok := dbP.Credentials["apikey"]; ok {
			if vs, ok := v.(string); ok {
				dbP.ApiKey = vs
			}
		}
	}
	// Convert DB timeout_ms (int) to Timeout duration string for provider constructors.
	if dbP.TimeoutMs > 0 && dbP.Timeout == "" {
		dbP.Timeout = fmt.Sprintf("%dms", dbP.TimeoutMs)
	}
	pc := newProvider(&dbP)
	if pc != nil {
		m.providers[dbP.ID] = pc
		if dbP.Provider != "" && dbP.Provider != dbP.ID {
			m.providers[dbP.Provider] = pc
		}
	}
}

func newProvider(p *entities.LlmProviderInfo) ILlmProvider {
	switch {
	case strings.EqualFold(p.Provider, "anthropic"):
		return newAnthropicProvider(p)
	case strings.EqualFold(p.Provider, "gemini"):
		return newGeminiProvider(p)
	default:
		return newOpenAIProvider(p)
	}
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
