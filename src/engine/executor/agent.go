package executor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flowgent-labs/flowgent/src/common/utils"
	"github.com/flowgent-labs/flowgent/src/config"
	"github.com/flowgent-labs/flowgent/src/engine"
	"github.com/flowgent-labs/flowgent/src/model"
)

// ─── Agent Executor ────────────────────────────────────

type AgentExecutor struct {
	llmClient  engine.LLMClient
	agents     map[string]*config.AgentDef
	maxRetries int
}

func NewAgentExecutor(llm engine.LLMClient, agents []*config.AgentDef) *AgentExecutor {
	m := make(map[string]*config.AgentDef)
	for _, a := range agents {
		m[a.Name] = a
	}
	return &AgentExecutor{llmClient: llm, agents: m, maxRetries: 3}
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

	// Determine output schema: node-level overrides agent-level
	outputSchema := agent.OutputSchema
	if plan.NodeSpec.OutputSchema != nil {
		outputSchema = plan.NodeSpec.OutputSchema
	}

	temperature := 0.3
	if agent.Temperature != nil {
		temperature = *agent.Temperature
	}

	var lastErr error
	for attempt := 0; attempt <= e.maxRetries; attempt++ {
		resp, err := e.llmClient.Generate(ctx, agent.Soul, userPrompt, agent.Model, temperature)
		if err != nil {
			lastErr = fmt.Errorf("LLM call failed: %w", err)
			continue
		}

		jsonStr := extractJSON(resp)
		var out map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
			lastErr = fmt.Errorf("agent output JSON: %w (len=%d raw: %s)", err, len(resp), resp[:min(len(resp), 500)])
			if attempt < e.maxRetries {
				userPrompt = fmt.Sprintf("%s\n\nYour previous output was not valid JSON. Output ONLY a valid JSON object, no other text. Error: %v", userPrompt, err)
			}
			continue
		}

		// Validate against output schema if defined
		if outputSchema != nil {
			if err := utils.ValidateJSONSchema(outputSchema, out); err != nil {
				lastErr = fmt.Errorf("schema validation failed: %w", err)
				if attempt < e.maxRetries {
					userPrompt = fmt.Sprintf("%s\n\nYour output did not match the required schema. Fix it. Schema: %v\nError: %v", userPrompt, outputSchema, err)
				}
				continue
			}
		}

		return &model.TaskResult{Output: out}, nil
	}

	return nil, lastErr
}
