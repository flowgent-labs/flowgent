package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/src/common/utils"
	"github.com/flowgent-labs/flowgent/src/config"
	"github.com/flowgent-labs/flowgent/src/engine"
	"github.com/flowgent-labs/flowgent/src/model"
)

// ─── Agent Executor ────────────────────────────────────

// MemoryStore is the subset of store.MemoryStore needed by executors.
type MemoryStore = interface {
	SaveMemory(ctx context.Context, m *model.Memory) error
	SearchMemory(ctx context.Context, agentID, nodeID string, embedding []float32, topK int) ([]model.Memory, error)
}

type AgentExecutor struct {
	llmClient   engine.LLMClient
	agents      map[string]*config.AgentDef
	memStore    MemoryStore // optional: enables memory persistence + recall
	maxRetries  int
}

func NewAgentExecutor(llm engine.LLMClient, agents []*config.AgentDef) *AgentExecutor {
	m := make(map[string]*config.AgentDef)
	for _, a := range agents {
		m[a.Name] = a
	}
	return &AgentExecutor{llmClient: llm, agents: m, maxRetries: 3}
}

// SetMemoryStore enables episodic memory persistence/recall for this executor.
func (e *AgentExecutor) SetMemoryStore(s MemoryStore) { e.memStore = s }

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

	// Enrich prompt with relevant past memories (RAG-style recall)
	if e.memStore != nil {
		if similar, _ := e.memStore.SearchMemory(ctx, agent.Name, plan.NodeID, nil, 3); len(similar) > 0 {
			userPrompt += "\n\n[Relevant past execution memories for context:]\n"
			for _, m := range similar {
				userPrompt += fmt.Sprintf("- Run %s, attempt %d (%s): %s\n", m.AgentFlowRunID, m.RetryCount, m.Status, truncate(m.Content, 300))
			}
		}
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
			e.saveMemory(ctx, agent.Name, plan, userPrompt, "", attempt, "failed", lastErr.Error())
			continue
		}

		jsonStr := extractJSON(resp)
		var out map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
			lastErr = fmt.Errorf("agent output JSON: %w (len=%d raw: %s)", err, len(resp), resp[:min(len(resp), 500)])
			e.saveMemory(ctx, agent.Name, plan, userPrompt, resp, attempt, "failed", lastErr.Error())
			if attempt < e.maxRetries {
				userPrompt = fmt.Sprintf("%s\n\nYour previous output was not valid JSON. Output ONLY a valid JSON object, no other text. Error: %v", userPrompt, err)
			}
			continue
		}

		// Validate against output schema if defined
		if outputSchema != nil {
			if err := utils.ValidateJSONSchema(outputSchema, out); err != nil {
				lastErr = fmt.Errorf("schema validation failed: %w", err)
				e.saveMemory(ctx, agent.Name, plan, userPrompt, resp, attempt, "failed", lastErr.Error())
				if attempt < e.maxRetries {
					userPrompt = fmt.Sprintf("%s\n\nYour output did not match the required schema. Fix it. Schema: %v\nError: %v", userPrompt, outputSchema, err)
				}
				continue
			}
		}

		e.saveMemory(ctx, agent.Name, plan, userPrompt, resp, attempt, "success", "")
		return &model.TaskResult{Output: out}, nil
	}

	return nil, lastErr
}

// saveMemory persists an episodic memory after each attempt (success or failure).
func (e *AgentExecutor) saveMemory(ctx context.Context, agentID string, plan *model.ExecutionPlan, prompt, response string, attempt int, status, errMsg string) {
	if e.memStore == nil {
		return
	}
	content := fmt.Sprintf("Prompt: %s\nResponse: %s", truncate(prompt, 1000), truncate(response, 1000))
	if errMsg != "" {
		content += fmt.Sprintf("\nError: %s", errMsg)
	}
	mem := &model.Memory{
		AgentID:        agentID,
		AgentFlowRunID: plan.AgentFlowRunID,
		NodeID:         plan.NodeID,
		Type:           model.MemoryEpisodic,
		Content:        content,
		RetryCount:     attempt,
		Status:         status,
		CreatedAt:      time.Now(),
	}
	_ = e.memStore.SaveMemory(ctx, mem)
}
