package executor

import (
	"context"
	"fmt"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/model/pkg"
)

// ─── Tool Executor ─────────────────────────────────────

type ToolExecutor struct {
	mcpClients map[string]engine.MCPClient
}

func NewToolExecutor(mcp map[string]engine.MCPClient) *ToolExecutor {
	return &ToolExecutor{mcpClients: mcp}
}

func (e *ToolExecutor) TaskType() model.TaskType { return model.TaskTool }

func (e *ToolExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	client, ok := e.mcpClients[plan.NodeSpec.Tool]
	if !ok {
		return nil, fmt.Errorf("MCP client not found: %s", plan.NodeSpec.Tool)
	}
	toolName := "call"
	if action, ok := plan.Input["action"].(string); ok {
		toolName = action
	}

	inputCopy := make(map[string]any)
	for k, v := range plan.Input {
		if k != "action" {
			inputCopy[k] = v
		}
	}
	if inputCopy == nil {
		inputCopy = make(map[string]any)
	}

	out, err := client.CallTool(ctx, toolName, inputCopy)
	if err != nil {
		return nil, err
	}
	return &model.TaskResult{Output: out}, nil
}
