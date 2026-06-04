package executor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flowgent-labs/flowgent/common/src/utils"
	"github.com/flowgent-labs/flowgent/config/src/config"
	"github.com/flowgent-labs/flowgent/core/src/engine"
	"github.com/flowgent-labs/flowgent/model/src"
)

// ─── Agent Executor ────────────────────────────────────

// NodeMemoryStore is the subset of store.NodeMemoryStore needed by executors.
type NodeMemoryStore = interface {
	GetMemory(ctx context.Context, flowID, nodeID string) (*model.NodeMemory, error)
	UpsertMemory(ctx context.Context, mem *model.NodeMemory) error
	SearchMemory(ctx context.Context, flowID string, embedding []float32, topK int) ([]model.NodeMemory, error)
}

type AgentExecutor struct {
	llmClient  engine.LLMClient
	agents     map[string]*config.AgentDef
	memStore   NodeMemoryStore // optional: flow-scoped memory persistence
	maxRetries int
}

func NewAgentExecutor(llm engine.LLMClient, agents []*config.AgentDef) *AgentExecutor {
	m := make(map[string]*config.AgentDef)
	for _, a := range agents {
		m[a.Name] = a
	}
	return &AgentExecutor{llmClient: llm, agents: m, maxRetries: 3}
}

func (e *AgentExecutor) SetMemoryStore(s NodeMemoryStore) { e.memStore = s }
func (e *AgentExecutor) TaskType() model.TaskType         { return model.TaskAgent }

func (e *AgentExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	agent := e.agents[plan.NodeSpec.Agent]
	if agent == nil {
		return nil, fmt.Errorf("agent not found: %s", plan.NodeSpec.Agent)
	}

	flowDefID := plan.AgentFlowDefinitionID // agentflow definition ID (NOT run ID)

	// Enrich prompt with prior node memory from past runs
	userPrompt := formatPlanInput(plan)
	if e.memStore != nil {
		if prior, _ := e.memStore.GetMemory(ctx, flowDefID, plan.NodeID); prior != nil && prior.Content != "" {
			userPrompt += fmt.Sprintf("\n\n[Prior executions of this node (flow=%s, node=%s):]\n%s", flowDefID, plan.NodeID, truncate(prior.Content, 500))
		}
	}

	instruction := plan.NodeSpec.Instruction
	if instruction == "" {
		instruction = agent.Instruction
	}
	if instruction != "" {
		userPrompt = instruction + "\n\n" + userPrompt
	}

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
			e.upsertMemory(ctx, flowDefID, plan.NodeID, userPrompt, "", attempt, lastErr.Error())
			continue
		}

		jsonStr := extractJSON(resp)
		var out map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
			lastErr = fmt.Errorf("agent output JSON: %w", err)
			e.upsertMemory(ctx, flowDefID, plan.NodeID, userPrompt, resp, attempt, lastErr.Error())
			if attempt < e.maxRetries {
				userPrompt = fmt.Sprintf("%s\n\nYour previous output was not valid JSON. Output ONLY a valid JSON object. Error: %v", userPrompt, err)
			}
			continue
		}

		if outputSchema != nil {
			if err := utils.ValidateJSONSchema(outputSchema, out); err != nil {
				lastErr = fmt.Errorf("schema validation failed: %w", err)
				e.upsertMemory(ctx, flowDefID, plan.NodeID, userPrompt, resp, attempt, lastErr.Error())
				if attempt < e.maxRetries {
					userPrompt = fmt.Sprintf("%s\n\nOutput did not match schema. Fix it. Schema: %v\nError: %v", userPrompt, outputSchema, err)
				}
				continue
			}
		}

		e.upsertMemory(ctx, flowDefID, plan.NodeID, userPrompt, resp, attempt, "")
		return &model.TaskResult{Output: out}, nil
	}

	return nil, lastErr
}

// upsertMemory accumulates execution context into the (flowID, nodeID) memory slot.
// Each call appends to the existing content rather than replacing — building a
// rich execution history that persists across runs.
func (e *AgentExecutor) upsertMemory(ctx context.Context, flowID, nodeID, prompt, response string, attempt int, errMsg string) {
	if e.memStore == nil {
		return
	}

	existing, _ := e.memStore.GetMemory(ctx, flowID, nodeID)
	entry := truncate(fmt.Sprintf("attempt=%d prompt=%s response=%s", attempt, truncate(prompt, 300), truncate(response, 300)), 2000)
	if errMsg != "" {
		entry += fmt.Sprintf(" error=%s", errMsg)
	}

	content := entry
	if existing != nil {
		content = existing.Content + "\n" + entry // accumulate, don't replace
	}

	_ = e.memStore.UpsertMemory(ctx, &model.NodeMemory{
		FlowID:   flowID,
		NodeID:   nodeID,
		Content:  content,
		Metadata: map[string]any{"retry_count": attempt, "last_error": errMsg},
	})
}
