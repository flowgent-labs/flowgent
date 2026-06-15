package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ─── Supervisor Executor ───────────────────────────────

type SupervisorExecutor struct {
	llmClient engine.LLMClient
	client    *client.FlowgentClient
	tenant    string
}

func NewSupervisorExecutor(llm engine.LLMClient, apiClient *client.FlowgentClient, tenant string) *SupervisorExecutor {
	return &SupervisorExecutor{llmClient: llm, client: apiClient, tenant: tenant}
}

func (e *SupervisorExecutor) TaskType() entities.TaskType { return entities.TaskSupervisor }

func (e *SupervisorExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.TaskResult, error) {
	agent, err := e.client.GetAgent(ctx, e.tenant, plan.NodeSpec.Agent)
	if err != nil {
		return nil, fmt.Errorf("supervisor agent not found %q: %w", plan.NodeSpec.Agent, err)
	}
	if agent == nil {
		return nil, fmt.Errorf("supervisor agent not found: %s", plan.NodeSpec.Agent)
	}

	resp, err := e.llmClient.Generate(ctx, agent.Soul, formatPlanInput(plan), agent.Model, 0.2)
	if err != nil {
		return nil, fmt.Errorf("supervisor LLM call failed: %w", err)
	}

	// Extract JSON from LLM response (may have preamble text like "Based on analysis...")
	jsonStr := extractJSON(resp)
	var decision map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &decision); err != nil {
		return nil, fmt.Errorf("supervisor output invalid JSON: %w (raw: %s)", err, resp[:min(len(resp), 200)])
	}

action, _ := decision["action"].(string)
	// Default to "continue" if action is missing or empty (defensive)
	if action == "" {
		action = "continue"
		decision["action"] = "continue"
	}
	if plan.NodeSpec.SupervisorConfig != nil && len(plan.NodeSpec.SupervisorConfig.AllowedActions) > 0 {
		allowed := plan.NodeSpec.SupervisorConfig.AllowedActions
		found := false
		for _, a := range allowed {
			if a == action {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("supervisor action %q not in allowed_actions: %v", action, allowed)
		}
	}

	return &entities.TaskResult{Output: decision}, nil
}

// extractJSON finds the first balanced JSON object in text, handling LLM preamble.
func extractJSON(s string) string {
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start == -1 || end == -1 || end <= start {
		return s
	}
	return s[start : end+1]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
