package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/src/config"
	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/util"
)

// TaskExecutor executes a single ExecutionPlan. Each task type has its
// own implementation, registered in the TaskExecutorRouter.
type TaskExecutor interface {
	TaskType() model.TaskType
	Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error)
}

// ─── Router ────────────────────────────────────────────

// TaskExecutorRouter dispatches an ExecutionPlan to the correct TaskExecutor
// by plan.TaskType.
type TaskExecutorRouter struct {
	executors map[model.TaskType]TaskExecutor
}

func NewTaskExecutorRouter() *TaskExecutorRouter {
	return &TaskExecutorRouter{executors: make(map[model.TaskType]TaskExecutor)}
}

func (r *TaskExecutorRouter) Register(exec TaskExecutor) {
	r.executors[exec.TaskType()] = exec
}

func (r *TaskExecutorRouter) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	exec, ok := r.executors[plan.TaskType]
	if !ok {
		return nil, fmt.Errorf("no executor registered for task type %q", plan.TaskType)
	}
	return exec.Execute(ctx, plan, scope)
}

// ─── Agent Executor ────────────────────────────────────

type AgentExecutor struct {
	llmClient LLMClient
	agents    map[string]*config.AgentDef
}

func NewAgentExecutor(llm LLMClient, agents []*config.AgentDef) *AgentExecutor {
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

// ─── Condition Executor ────────────────────────────────

type ConditionExecutor struct{}

func (e *ConditionExecutor) TaskType() model.TaskType { return model.TaskCondition }

func (e *ConditionExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	expr := plan.NodeSpec.Expression
	if expr == "" {
		expr = "${input.result == true}"
	}
	result := util.EvalCondition(expr, scope)
	return &model.TaskResult{Output: map[string]any{"result": result}}, nil
}

// ─── Tool Executor ─────────────────────────────────────

type ToolExecutor struct {
	mcpClients map[string]MCPClient
}

func NewToolExecutor(mcp map[string]MCPClient) *ToolExecutor {
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

// ─── Supervisor Executor ───────────────────────────────

type SupervisorExecutor struct {
	llmClient LLMClient
	agents    map[string]*config.AgentDef
	store     Store
}

func NewSupervisorExecutor(llm LLMClient, agents []*config.AgentDef, store Store) *SupervisorExecutor {
	m := make(map[string]*config.AgentDef)
	for _, a := range agents {
		m[a.Name] = a
	}
	return &SupervisorExecutor{llmClient: llm, agents: m, store: store}
}

func (e *SupervisorExecutor) TaskType() model.TaskType { return model.TaskSupervisor }

func (e *SupervisorExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	agent := e.agents[plan.NodeSpec.Agent]
	if agent == nil {
		return nil, fmt.Errorf("supervisor agent not found: %s", plan.NodeSpec.Agent)
	}

	resp, err := e.llmClient.Generate(ctx, agent.Soul, formatPlanInput(plan), agent.Model, 0.2)
	if err != nil {
		return nil, fmt.Errorf("supervisor LLM call failed: %w", err)
	}

	var decision map[string]any
	if err := json.Unmarshal([]byte(resp), &decision); err != nil {
		return nil, fmt.Errorf("supervisor output invalid JSON: %w", err)
	}

	_ = e.store.LogSupervisorDecision(ctx, plan.AgentFlowRunID, plan.TaskID, plan.Input, decision)

	action, _ := decision["action"].(string)
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

	return &model.TaskResult{Output: decision}, nil
}

// ─── Tribunal Executor ─────────────────────────────────

type TribunalExecutor struct{}

func (e *TribunalExecutor) TaskType() model.TaskType { return model.TaskTribunal }

func (e *TribunalExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	strategy := plan.NodeSpec.Strategy
	decisionType := "majority"
	if strategy != nil {
		if t, ok := strategy["type"].(string); ok {
			decisionType = t
		}
	}

	votes, _ := plan.Input["votes"].([]any)
	approveCount := 0
	totalCount := len(votes)
	for _, v := range votes {
		if m, ok := v.(map[string]any); ok {
			if dec, ok := m["decision"].(bool); ok && dec {
				approveCount++
			}
		}
	}

	var approved bool
	switch decisionType {
	case "majority":
		approved = approveCount > totalCount/2
	case "unanimous":
		approved = approveCount == totalCount
	case "any":
		approved = approveCount >= 1
	case "majority_strict":
		approved = approveCount >= (totalCount*2)/3
	default:
		approved = approveCount > totalCount/2
	}

	confidence := 0.0
	if totalCount > 0 {
		confidence = float64(approveCount) / float64(totalCount)
	}

	return &model.TaskResult{Output: map[string]any{
		"decision":   approved,
		"confidence": confidence,
		"approve":    approveCount,
		"total":      totalCount,
		"strategy":   decisionType,
	}}, nil
}

// ─── Map Executor ──────────────────────────────────────

type MapExecutor struct{}

func (e *MapExecutor) TaskType() model.TaskType { return model.TaskMap }

func (e *MapExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	// Map execution: the JM creates child ExecutionPlans for each item
	// and dispatches them. The MapExecutor just marks the map plan as
	// complete; the actual fan-out is handled by the JM.
	return &model.TaskResult{Output: map[string]any{"status": "dispatched"}}, nil
}

// ─── Join Executor ─────────────────────────────────────

type JoinExecutor struct{}

func (e *JoinExecutor) TaskType() model.TaskType { return model.TaskJoin }

func (e *JoinExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	// Join waits for all map children to complete and aggregates results.
	// The JM handles the aggregation; this marks the join as done.
	results, _ := plan.Input["results"].([]any)
	return &model.TaskResult{Output: map[string]any{
		"results": results,
		"count":   len(results),
	}}, nil
}

// ─── Subflow Executor ──────────────────────────────────

type SubflowExecutor struct{}

func (e *SubflowExecutor) TaskType() model.TaskType { return model.TaskSubflow }

func (e *SubflowExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	// Subflow execution: the JM creates child ExecutionPlans from the
	// sub-agentflow spec and dispatches them.
	return &model.TaskResult{Output: map[string]any{"status": "dispatched"}}, nil
}

// ─── Human Executor ────────────────────────────────────

type HumanExecutor struct {
	store Store
}

func NewHumanExecutor(store Store) *HumanExecutor {
	return &HumanExecutor{store: store}
}

func (e *HumanExecutor) TaskType() model.TaskType { return model.TaskHuman }

func (e *HumanExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	timeout := 24 * time.Hour
	if plan.NodeSpec.Approval != nil && plan.NodeSpec.Approval.Timeout > 0 {
		timeout = plan.NodeSpec.Approval.Timeout
	}

	approval := &model.HumanApproval{
		TaskRunID: plan.TaskID,
		Timeout:   timeout,
		Status:    "PENDING",
	}
	if err := e.store.CreateHumanApproval(ctx, approval); err != nil {
		return nil, fmt.Errorf("create human approval: %w", err)
	}

	onApprove := "continue"
	onReject := "continue"
	if plan.NodeSpec.Approval != nil {
		onApprove = plan.NodeSpec.Approval.OnApprove
		onReject = plan.NodeSpec.Approval.OnReject
	}

	return &model.TaskResult{Output: map[string]any{
		"approval_token": approval.Token,
		"timeout":        timeout.String(),
		"status":         "WAITING_HUMAN",
		"on_approve":     resolveAction(onApprove),
		"on_reject":      resolveAction(onReject),
	}}, nil
}

func resolveAction(action string) string {
	switch action {
	case "continue", "abort", "skip":
		return action
	default:
		return "continue"
	}
}

// ─── Noop Executor ─────────────────────────────────────

type NoopExecutor struct{}

func (e *NoopExecutor) TaskType() model.TaskType { return model.TaskNoop }

func (e *NoopExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	return &model.TaskResult{Output: nil}, nil
}

// ─── Helpers ───────────────────────────────────────────

func formatPlanInput(plan *model.ExecutionPlan) string {
	b, _ := json.Marshal(plan.Input)
	return string(b)
}

// ─── MapRunner (kept for inline fan-out within map nodes) ─

type MapRunner struct {
	store  Store
	logger *util.Logger
	mu     sync.Mutex
}

func newMapRunner(store Store, logger *util.Logger) *MapRunner {
	return &MapRunner{store: store, logger: logger}
}

func (m *MapRunner) runMap(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any, router *TaskExecutorRouter) error {
	source := plan.Input["source"]
	items, ok := source.([]any)
	if !ok {
		return fmt.Errorf("map source is not an array")
	}

	concurrency := plan.NodeSpec.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	results := make([]any, len(items))
	var firstErr error

	for i, item := range items {
		wg.Add(1)
		go func(idx int, it any) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			innerScope := make(map[string]map[string]any)
			for k, v := range scope {
				innerScope[k] = v
			}
			innerScope["item"] = map[string]any{"item": it, "index": idx}

			innerPlan := &model.ExecutionPlan{
				PlanID:         fmt.Sprintf("%s-%d", plan.PlanID, idx),
				AgentFlowRunID: plan.AgentFlowRunID,
				TaskType:       model.TaskAgent,
				NodeID:         fmt.Sprintf("%s[%d]", plan.NodeID, idx),
				Input:          map[string]any{"item": it, "index": idx},
				NodeSpec:       plan.NodeSpec.ChildNode,
				CreatedAt:      time.Now(),
			}

			result, err := router.Execute(ctx, innerPlan, innerScope)
			if err != nil && firstErr == nil {
				firstErr = err
			}
			if result != nil {
				results[idx] = result.Output
			}
		}(i, item)
	}

	wg.Wait()
	plan.Result = &model.TaskResult{Output: map[string]any{"results": results}}
	if firstErr != nil {
		return firstErr
	}
	return nil
}
