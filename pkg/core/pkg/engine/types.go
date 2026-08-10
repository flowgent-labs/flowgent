package engine

import (
	"context"
)

// Provider identifies the resource management backend.
type Provider string

const (
	ProviderStandalone Provider = "standalone"
	ProviderKubernetes Provider = "kubernetes"
)

// MCPClient abstracts an MCP server connection.
type MCPClient interface {
	CallTool(ctx context.Context, toolName string, args map[string]any) (map[string]any, error)
}

// LLMClient abstracts an LLM provider.
type LLMClient interface {
	Generate(ctx context.Context, systemPrompt, userPrompt, model string, temperature float64, maxTokens int) (string, error)
}

// Note: pkg/core does not import pkg/store directly — only the apiserver connects
// to the database (see docs/01-L1-Engine-Architecture.md §1). Non-apiserver
// components (JM, TM, controller, sandbox, notifier) persist state exclusively
// via FlowgentClient (REST) or MQTT.
