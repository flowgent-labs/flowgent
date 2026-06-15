package llm

import (
	"context"
	"fmt"
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
				if !dbp.Enabled || dbp.ID == "" {
					continue
				}
				m.registerDB(*dbp)
			}
		}
	}

	return m
}

func (m *LlmProviderManager) registerDB(dbP entities.LlmProviderInfo) {
	// Resolve ApiKey from Credentials map (DB stores credentials as a map, runtime needs the string).
	if v, ok := dbP.Credentials["apikey"]; ok {
		if vs, ok := v.(string); ok {
			dbP.ApiKey = vs
		}
	}
	pc := newProvider(&dbP)
	if pc != nil {
		m.providers[dbP.ID] = pc
	}
}

func newProvider(p *entities.LlmProviderInfo) ILlmProvider {
	switch p.Type {
	case "anthropic":
		return newAnthropicProvider(p)
	case "gemini":
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
