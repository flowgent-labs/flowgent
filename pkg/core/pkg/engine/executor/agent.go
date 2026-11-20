package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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

// KnowledgeRetriever provides cross-workflow persistent knowledge for RAG injection.
// Engine components use this via a REST-client adapter — never a direct store import.
type KnowledgeRetriever interface {
	SearchKnowledge(ctx context.Context, namespace string, query string, topK int, tags []string) ([]*entities.KnowledgeEntry, error)
}

type AgentExecutor struct {
	llmClient  engine.LLMClient
	client     *client.FlowgentClient
	namespace  string
	knowledge  KnowledgeRetriever
	maxRetries int
}

func NewAgentExecutor(llm engine.LLMClient, apiClient *client.FlowgentClient, namespace string) *AgentExecutor {
	return &AgentExecutor{llmClient: llm, client: apiClient, namespace: namespace, maxRetries: 3}
}

func (e *AgentExecutor) SetKnowledgeRetriever(k KnowledgeRetriever) { e.knowledge = k }
func (e *AgentExecutor) NodeKind() entities.NodeKind                { return entities.AgentNode }

func (e *AgentExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.NodeResult, error) {
	agent, err := e.client.GetRunAgent(ctx, e.namespace, plan.GraphRunID, plan.Node.Agent)
	if err != nil {
		return nil, fmt.Errorf("agent not found %q: %w", plan.Node.Agent, err)
	}
	if agent == nil {
		return nil, fmt.Errorf("agent not found: %s", plan.Node.Agent)
	}

	userPrompt := formatPlanInput(plan)
	// A node resumes only the checkpoint carried by this run/attempt. Context
	// from older runs is knowledge and must pass the scoped retrieval path.
	if plan.Checkpoint != nil {
		if plan.Checkpoint.Scratchpad != "" {
			userPrompt += "\n\n[Current attempt checkpoint:]\n" + truncate(plan.Checkpoint.Scratchpad, 1000)
		}
		for _, message := range plan.Checkpoint.Messages {
			userPrompt += fmt.Sprintf("\n%s: %s", message.Role, truncate(message.Content, 500))
		}
	}

	instruction := plan.Node.Instruction
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
	if plan.Node.OutputSchema != nil {
		outputSchema = plan.Node.OutputSchema
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
				attribute.String("graph_run.id", plan.GraphRunID),
				attribute.String("agent_graph.name", plan.AgentGraphName),
				attribute.String("flowgent.node_instance_id", plan.NodeInstanceID),
				attribute.String("flowgent.node_run_id", plan.NodeRunID),
				attribute.Int("flowgent.generation_attempt", attempt+1),
				attribute.Int("flowgent.generation_max_retries", e.maxRetries),
				attribute.String("gen_ai.request.model", agent.Model),
			),
		)
		resp, err := e.llmClient.Generate(attemptCtx, soul, userPrompt, agent.Model, temperature, agent.MaxTokens)
		if err != nil {
			lastErr = fmt.Errorf("LLM call failed: %w", err)
			attemptSpan.RecordError(err)
			attemptSpan.SetStatus(codes.Error, "LLM call failed")
			attemptSpan.End()
			continue
		}

		jsonStr := extractJSON(resp)
		var out map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
			lastErr = fmt.Errorf("agent output JSON: %w", err)
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
				attemptSpan.RecordError(err)
				attemptSpan.SetStatus(codes.Error, "schema validation failed")
				attemptSpan.End()
				if attempt < e.maxRetries {
					userPrompt = fmt.Sprintf("%s\n\nOutput did not match schema. Fix it. Schema: %v\nError: %v", userPrompt, outputSchema, err)
				}
				continue
			}
		}

		attemptSpan.SetStatus(codes.Ok, "done")
		attemptSpan.End()
		plan.Checkpoint = &entities.NodeCheckpoint{Messages: []entities.MessageEntry{{Role: "system", Content: soul}, {Role: "user", Content: userPrompt}, {Role: "assistant", Content: resp}}, LastStep: 1, CheckpointedAt: time.Now().UTC()}
		return &entities.NodeResult{Output: out}, nil
	}

	return nil, lastErr
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
