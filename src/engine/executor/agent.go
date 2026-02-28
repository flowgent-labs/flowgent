package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/flowgent-labs/flowgent/src/config"
	"github.com/flowgent-labs/flowgent/src/engine"
	"github.com/flowgent-labs/flowgent/src/model"
)

// ─── Agent Executor ────────────────────────────────────

type AgentExecutor struct {
	llmClient engine.LLMClient
	agents    map[string]*config.AgentDef
}

func NewAgentExecutor(llm engine.LLMClient, agents []*config.AgentDef) *AgentExecutor {
	m := make(map[string]*config.AgentDef)
	for _, a := range agents {
		m[a.Name] = a
	}
	return &AgentExecutor{llmClient: llm, agents: m}
}

func (e *AgentExecutor) TaskType() model.TaskType { return model.TaskAgent }

func (e *AgentExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	agent := e.agents[plan.NodeSpec.Agent]
	if agent == nil {
		return nil, fmt.Errorf("agent not found: %s", plan.NodeSpec.Agent)
	}

	userPrompt := formatPlanInput(plan)
	instruction := plan.NodeSpec.Instruction
	if instruction == "" {
		instruction = agent.Instruction
	}
	if instruction != "" {
		userPrompt = instruction + "\n\n" + userPrompt
	}

	resp, err := e.llmClient.Generate(ctx, agent.Soul, userPrompt, agent.Model, 0.3)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		return nil, fmt.Errorf("agent output is not valid JSON: %w", err)
	}
	return &model.TaskResult{Output: out}, nil
}

