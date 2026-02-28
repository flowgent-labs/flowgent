package engine

import (
	"context"

	"github.com/flowgent-labs/flowgent/src/store"
)

// Provider identifies the resource management backend.
type Provider string

const (
	ProviderLocal      Provider = "local"
	ProviderKubernetes Provider = "kubernetes"
)

// MCPClient abstracts an MCP server connection.
type MCPClient interface {
	CallTool(ctx context.Context, toolName string, args map[string]any) (map[string]any, error)
}

// LLMClient abstracts an LLM provider.
type LLMClient interface {
	Generate(ctx context.Context, systemPrompt, userPrompt, model string, temperature float64) (string, error)
}

// Store is the persistence layer interface used by all engine components.
// This is an alias of store.Store — the canonical definition lives in src/store/.
type Store = store.Store
