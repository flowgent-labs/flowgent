package engine

import "context"

// MCPClient abstracts an MCP server connection.
type MCPClient interface {
	CallTool(ctx context.Context, toolName string, args map[string]any) (map[string]any, error)
}

// LLMClient abstracts an LLM provider.
type LLMClient interface {
	Generate(ctx context.Context, systemPrompt, userPrompt, model string, temperature float64) (string, error)
}
