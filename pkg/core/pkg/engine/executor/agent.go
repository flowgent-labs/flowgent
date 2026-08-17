package executor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ─── Agent Executor ────────────────────────────────────

// NodeMemoryStore is the subset of store.NodeMemoryStore needed by executors.
type NodeMemoryStore = interface {
	GetMemory(ctx context.Context, flowID, nodeID string) (*entities.MemoryInfo, error)
	UpsertMemory(ctx context.Context, mem *entities.MemoryInfo) error
	SearchMemory(ctx context.Context, flowID string, embedding []float32, topK int) ([]entities.MemoryInfo, error)
}

// KnowledgeRetriever provides cross-workflow persistent knowledge for RAG injection.
// Engine components use this via a REST-client adapter — never a direct store import.
type KnowledgeRetriever interface {
	SearchKnowledge(ctx context.Context, namespace string, query string, topK int, tags []string) ([]*entities.KnowledgeEntry, error)
}

type AgentExecutor struct {
	llmClient  engine.LLMClient
	client     *client.FlowgentClient
	namespace  string
	memStore   NodeMemoryStore
	knowledge  KnowledgeRetriever
	maxRetries int
}

func NewAgentExecutor(llm engine.LLMClient, apiClient *client.FlowgentClient, namespace string) *AgentExecutor {
	return &AgentExecutor{llmClient: llm, client: apiClient, namespace: namespace, maxRetries: 3}
}

func (e *AgentExecutor) SetMemoryStore(s NodeMemoryStore)           { e.memStore = s }
func (e *AgentExecutor) SetKnowledgeRetriever(k KnowledgeRetriever) { e.knowledge = k }
func (e *AgentExecutor) TaskType() entities.TaskType                { return entities.TaskAgent }

func (e *AgentExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.TaskResult, error) {
	agent, err := e.client.GetAgent(ctx, e.namespace, plan.NodeSpec.Agent)
	if err != nil {
		return nil, fmt.Errorf("agent not found %q: %w", plan.NodeSpec.Agent, err)
	}
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

	// Inject cross-workflow knowledge into the system prompt (RAG).
	soul := agent.Soul
	if e.knowledge != nil {
		query := truncate(instruction+" "+userPrompt, 512)
		if entries, err := e.knowledge.SearchKnowledge(ctx, e.namespace, query, 5, nil); err == nil && len(entries) > 0 {
			soul = formatKnowledgeContext(entries) + "\n\n" + soul
		}
	}

	outputSchema := agent.OutputSchema
	if plan.NodeSpec.OutputSchema != nil {
		outputSchema = plan.NodeSpec.OutputSchema
	}
	if outputSchema != nil {
		schemaJSON, _ := json.MarshalIndent(outputSchema, "", "  ")
		userPrompt += "\n\nYou MUST output a valid JSON object matching this schema:\n```json\n" + string(schemaJSON) + "\n```\nOutput ONLY the JSON, no other text."
	}

	temperature := 0.3
	if agent.Temperature != nil {
		temperature = *agent.Temperature
	}

	var lastErr error
	for attempt := 0; attempt <= e.maxRetries; attempt++ {
		attemptCtx, attemptSpan := tracing.Tracer("flowgent/executor/agent").Start(ctx, "agent.generate",
			trace.WithAttributes(
				attribute.String("run.id", plan.AgentFlowRunID),
				attribute.String("agentflow.id", plan.AgentFlowDefinitionID),
				attribute.String("flowgent.node_id", plan.NodeID),
				attribute.String("flowgent.task_id", plan.TaskID),
				attribute.Int("flowgent.generation_attempt", attempt+1),
				attribute.Int("flowgent.generation_max_retries", e.maxRetries),
				attribute.String("gen_ai.request.model", agent.Model),
			),
		)
		resp, err := e.llmClient.Generate(attemptCtx, soul, userPrompt, agent.Model, temperature, agent.MaxTokens)
		if err != nil {
			lastErr = fmt.Errorf("LLM call failed: %w", err)
			e.upsertMemory(ctx, flowDefID, plan.NodeID, userPrompt, "", attempt, lastErr.Error())
			attemptSpan.RecordError(err)
			attemptSpan.SetStatus(codes.Error, "LLM call failed")
			attemptSpan.End()
			continue
		}

		jsonStr := extractJSON(resp)
		var out map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
			lastErr = fmt.Errorf("agent output JSON: %w", err)
			e.upsertMemory(ctx, flowDefID, plan.NodeID, userPrompt, resp, attempt, lastErr.Error())
			attemptSpan.RecordError(err)
			attemptSpan.SetStatus(codes.Error, "invalid JSON output")
			attemptSpan.End()
			if attempt < e.maxRetries {
				userPrompt = fmt.Sprintf("%s\n\nYour previous output was not valid JSON. Output ONLY a valid JSON object. Error: %v", userPrompt, err)
			}
			continue
		}

		if outputSchema != nil {
			if err := utils.ValidateJSONSchema(outputSchema, out); err != nil {
				lastErr = fmt.Errorf("schema validation failed: %w", err)
				e.upsertMemory(ctx, flowDefID, plan.NodeID, userPrompt, resp, attempt, lastErr.Error())
				attemptSpan.RecordError(err)
				attemptSpan.SetStatus(codes.Error, "schema validation failed")
				attemptSpan.End()
				if attempt < e.maxRetries {
					userPrompt = fmt.Sprintf("%s\n\nOutput did not match schema. Fix it. Schema: %v\nError: %v", userPrompt, outputSchema, err)
				}
				continue
			}
		}

		e.upsertMemory(ctx, flowDefID, plan.NodeID, userPrompt, resp, attempt, "")
		attemptSpan.SetStatus(codes.Ok, "done")
		attemptSpan.End()
		return &entities.TaskResult{Output: out}, nil
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

	_ = e.memStore.UpsertMemory(ctx, &entities.MemoryInfo{
		FlowID:   flowID,
		NodeID:   nodeID,
		Content:  content,
		Metadata: map[string]any{"retry_count": attempt, "last_error": errMsg},
	})
}

// formatKnowledgeContext formats knowledge entries as a system-prompt context block.
func formatKnowledgeContext(entries []*entities.KnowledgeEntry) string {
	var b []byte
	b = append(b, "[Relevant cross-workflow knowledge:]\n"...)
	for i, e := range entries {
		if i > 0 {
			b = append(b, '\n')
		}
		b = append(b, fmt.Sprintf("- %s: %s", e.Title, truncate(e.Content, 300))...)
	}
	return string(b)
}
