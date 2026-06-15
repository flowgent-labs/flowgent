package executor

import (
	"context"

	"github.com/flowgent-labs/flowgent/core/pkg/mcp"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ─── Tool Executor ─────────────────────────────────────

type ToolExecutor struct {
	mcpMgr *mcp.McpManager
}

func NewToolExecutor(mcpMgr *mcp.McpManager) *ToolExecutor {
	return &ToolExecutor{mcpMgr: mcpMgr}
}

func (e *ToolExecutor) TaskType() entities.TaskType { return entities.TaskTool }

func (e *ToolExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.TaskResult, error) {
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

	out, err := e.mcpMgr.CallTool(ctx, plan.NodeSpec.Tool, toolName, inputCopy)
	if err != nil {
		return nil, err
	}
	return &entities.TaskResult{Output: out}, nil
}
