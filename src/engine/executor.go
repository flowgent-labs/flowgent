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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Executor handles execution of individual nodes.
type Executor struct {
	store         Store
	mcpClients    map[string]MCPClient
	agents        map[string]*config.AgentDef
	llmClient     LLMClient
	logger        *util.Logger
	mu            sync.Mutex
	agentFlowDesc string
}

// Store is the interface for the persistence layer used by the engine.
type Store interface {
	CreateTaskRun(ctx context.Context, task *model.TaskRun) error
	UpdateTaskRun(ctx context.Context, task *model.TaskRun) error
	GetTaskRun(ctx context.Context, id string) (*model.TaskRun, error)
	CreateAgentFlowRun(ctx context.Context, run *model.AgentFlowRun) error
	UpdateAgentFlowRun(ctx context.Context, run *model.AgentFlowRun) error
	GetAgentFlowRun(ctx context.Context, id string) (*model.AgentFlowRun, error)
	ListAgentFlowRuns(ctx context.Context, agentFlowID string, limit int) ([]model.AgentFlowRun, error)
	ListActiveRuns(ctx context.Context) ([]model.AgentFlowRun, error)
	GetTaskRunsByAgentFlowRun(ctx context.Context, agentFlowRunID string) ([]model.TaskRun, error)
	CreateHumanApproval(ctx context.Context, approval *model.HumanApproval) error
	GetHumanApproval(ctx context.Context, token string) (*model.HumanApproval, error)
	UpdateHumanApproval(ctx context.Context, approval *model.HumanApproval) error
	LogSupervisorDecision(ctx context.Context, agentFlowRunID, taskRunID string, input, decision map[string]any) error
	GetTaskRunByExecID(ctx context.Context, execID string) (*model.TaskRun, error)
}

type MCPClient interface {
	CallTool(ctx context.Context, toolName string, args map[string]any) (map[string]any, error)
}

type LLMClient interface {
	Generate(ctx context.Context, systemPrompt, userPrompt, model string, temperature float64) (string, error)
}

func NewExecutor(store Store, mcp map[string]MCPClient, agents []*config.AgentDef, llm LLMClient, logger *util.Logger) *Executor {
	agentMap := make(map[string]*config.AgentDef)
	for _, a := range agents {
		agentMap[a.Name] = a
	}
	return &Executor{
		store:      store,
		mcpClients: mcp,
		agents:     agentMap,
		llmClient:  llm,
		logger:     logger,
	}
}

func (e *Executor) SetAgentFlowContext(desc string) {
	e.agentFlowDesc = desc
}

func (e *Executor) executeNode(ctx context.Context, task *model.TaskRun, node *model.Node, scope map[string]map[string]any) error {
	ctx, span := otel.Tracer("flowgent/executor").Start(ctx, "executor.node",
		trace.WithAttributes(
			attribute.String("node.id", node.ID),
			attribute.String("node.type", string(node.Type)),
			attribute.String("node.agent", node.Agent),
			attribute.String("node.tool", node.Tool),
			attribute.String("task.id", task.ID),
		),
	)
	defer span.End()

	resolvedInput := resolveInput(node.Input, scope)
	task.Input = resolvedInput

	span.AddEvent("node.input", trace.WithAttributes(
		attribute.String("input.json", util.TruncateJSON(resolvedInput, 2000)),
	))

	now := time.Now()
	task.StartedAt = &now
	task.Status = model.Running
	if err := e.store.UpdateTaskRun(ctx, task); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	var out map[string]any
	var err error

	switch node.Type {
	case model.AgentNode:
		out, err = e.executeAgent(ctx, node, resolvedInput, scope)
	case model.ToolNode:
		out, err = e.executeTool(ctx, node, resolvedInput)
	case model.MapNode:
		// Map nodes are handled by the runtime at a higher level
		out = resolvedInput
	case model.AgentFlowNode:
		// Sub-agentflow nodes are dispatched by the runtime via spec lookup
		out = resolvedInput
	case model.ConditionNode:
		out, err = e.executeCondition(node, resolvedInput, scope)
	case model.TribunalNode:
		out, err = e.executeTribunal(node, resolvedInput)
	case model.HumanNode:
		return e.executeHuman(ctx, task, node)
	case model.SupervisorNode:
		return e.executeSupervisor(ctx, task, node, resolvedInput, scope)
	case model.NoopNode:
		out = nil
	default:
		err = fmt.Errorf("unknown node type: %s", node.Type)
	}

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return e.finishTask(ctx, task, nil, err)
	}

	if out != nil {
		span.AddEvent("node.output", trace.WithAttributes(
			attribute.String("output.json", util.TruncateJSON(out, 4000)),
		))
	}
	return e.finishTask(ctx, task, out, nil)
}

func (e *Executor) executeAgent(ctx context.Context, node *model.Node, input map[string]any, scope map[string]map[string]any) (map[string]any, error) {
	agent := e.agents[node.Agent]
	if agent == nil {
		return nil, fmt.Errorf("agent not found: %s", node.Agent)
	}

	userPrompt := formatInput(input)
	// Node-level instruction overrides agent-level instruction
	instruction := node.Instruction
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
	if err := parseJSON(resp, &out); err != nil {
		return nil, fmt.Errorf("agent output is not valid JSON: %w", err)
	}
	return out, nil
}

func (e *Executor) executeTool(ctx context.Context, node *model.Node, input map[string]any) (map[string]any, error) {
	client, ok := e.mcpClients[node.Tool]
	if !ok {
		return nil, fmt.Errorf("MCP client not found: %s", node.Tool)
	}

	toolName := "call"
	if action, ok := input["action"].(string); ok {
		toolName = action
		delete(input, "action")
	}

	return client.CallTool(ctx, toolName, input)
}

func (e *Executor) executeCondition(node *model.Node, input map[string]any, scope map[string]map[string]any) (map[string]any, error) {
	expr := node.Expression
	if expr == "" {
		expr = "${input.result == true}"
	}
	result := util.EvalCondition(expr, scope)
	return map[string]any{"result": result}, nil
}

func (e *Executor) executeTribunal(node *model.Node, input map[string]any) (map[string]any, error) {
	strategy := node.Strategy
	decisionType := "majority"
	if strategy != nil {
		if t, ok := strategy["type"].(string); ok {
			decisionType = t
		}
	}

	votes, ok := input["votes"].([]map[string]any)
	if !ok {
		reviews := extractReviews(input)
		votes = make([]map[string]any, len(reviews))
		for i, r := range reviews {
			if dec, ok := r["decision"].(bool); ok {
				votes[i] = map[string]any{"decision": dec}
			} else {
				votes[i] = map[string]any{"decision": false}
			}
		}
	}

	approveCount := 0
	totalCount := len(votes)
	for _, v := range votes {
		if dec, ok := v["decision"].(bool); ok && dec {
			approveCount++
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

	return map[string]any{
		"decision":   approved,
		"confidence": confidence,
		"approve":    approveCount,
		"total":      totalCount,
		"strategy":   decisionType,
	}, nil
}

func (e *Executor) executeHuman(ctx context.Context, task *model.TaskRun, node *model.Node) error {
	timeout := 24 * time.Hour
	if node.Approval != nil && node.Approval.Timeout > 0 {
		timeout = node.Approval.Timeout
	}

	task.Status = model.WaitingHuman

	approval := &model.HumanApproval{
		TaskRunID: task.ID,
		Timeout:   timeout,
		Status:    "PENDING",
	}

	if err := e.store.CreateHumanApproval(ctx, approval); err != nil {
		return fmt.Errorf("create human approval: %w", err)
	}

	task.Output = map[string]any{
		"approval_token": approval.Token,
		"timeout":        timeout.String(),
		"status":         "WAITING_HUMAN",
		"on_approve":     resolveApprovalAction(node.Approval.OnApprove),
		"on_reject":      resolveApprovalAction(node.Approval.OnReject),
	}

	return e.store.UpdateTaskRun(ctx, task)
}

func resolveApprovalAction(action string) string {
	switch action {
	case "continue", "abort", "skip":
		return action
	default:
		return "continue"
	}
}

func (e *Executor) executeSupervisor(ctx context.Context, task *model.TaskRun, node *model.Node, input map[string]any, scope map[string]map[string]any) error {
	ctx, span := otel.Tracer("flowgent/executor").Start(ctx, "executor.supervisor",
		trace.WithAttributes(
			attribute.String("supervisor.agent", node.Agent),
			attribute.String("agentflow.description", e.agentFlowDesc),
			attribute.String("supervisor.allowed_actions", fmt.Sprintf("%v", func() []string {
				if node.SupervisorConfig != nil {
					return node.SupervisorConfig.AllowedActions
				}
				return nil
			}())),
		),
	)
	defer span.End()

	agent := e.agents[node.Agent]
	if agent == nil {
		return fmt.Errorf("supervisor agent not found: %s", node.Agent)
	}

	systemPrompt := agent.Soul
	if e.agentFlowDesc != "" {
		systemPrompt = fmt.Sprintf("%s\n\n## AgentFlow Context\n%s", agent.Soul, e.agentFlowDesc)
	}

	userPrompt := formatInput(input)
	resp, err := e.llmClient.Generate(ctx, systemPrompt, userPrompt, agent.Model, 0.2)
	if err != nil {
		return fmt.Errorf("supervisor LLM call failed: %w", err)
	}

	var decision map[string]any
	if err := parseJSON(resp, &decision); err != nil {
		return fmt.Errorf("supervisor output invalid JSON: %w", err)
	}

	_ = e.store.LogSupervisorDecision(ctx, task.AgentFlowRunID, task.ID, input, decision)

	span.AddEvent("supervisor.decision", trace.WithAttributes(
		attribute.String("decision.action", fmt.Sprintf("%v", decision["action"])),
		attribute.String("decision.target", fmt.Sprintf("%v", decision["target"])),
		attribute.String("decision.reason", fmt.Sprintf("%v", decision["reason"])),
	))

	action, _ := decision["action"].(string)
	target, _ := decision["target"].(string)

	if node.SupervisorConfig != nil && len(node.SupervisorConfig.AllowedActions) > 0 {
		if !containsAction(node.SupervisorConfig.AllowedActions, action) {
			return fmt.Errorf("supervisor action %q not in allowed_actions: %v", action, node.SupervisorConfig.AllowedActions)
		}
	}

	task.Output = decision

	switch action {
	case "continue":
		return e.finishTask(ctx, task, decision, nil)
	case "retry":
		return e.retryFromSupervisor(ctx, task, target, decision)
	case "redirect":
		task.Output = decision
		if err := e.finishTask(ctx, task, decision, nil); err != nil {
			return err
		}
		return nil
	case "inject":
		return e.injectFromSupervisor(ctx, task, target, decision)
	case "abort":
		task.Output = decision
		if err := e.finishTask(ctx, task, decision, nil); err != nil {
			return err
		}
		return ErrSupervisorAbort
	default:
		return e.finishTask(ctx, task, decision, nil)
	}
}

func (e *Executor) retryFromSupervisor(ctx context.Context, task *model.TaskRun, target string, decision map[string]any) error {
	task.Output = decision
	if err := e.finishTask(ctx, task, decision, nil); err != nil {
		return err
	}
	return &SupervisorActionError{
		Action: "retry",
		Target: target,
		Reason: getString(decision, "reason"),
	}
}

func (e *Executor) injectFromSupervisor(ctx context.Context, task *model.TaskRun, target string, decision map[string]any) error {
	task.Output = decision
	if err := e.finishTask(ctx, task, decision, nil); err != nil {
		return err
	}

	injectedNodes, ok := decision["injected_nodes"].([]map[string]any)
	if !ok || len(injectedNodes) == 0 {
		return nil
	}

	var toInject []InjectedNode
	for _, n := range injectedNodes {
		id, _ := n["id"].(string)
		nodeType, _ := n["type"].(string)
		dependsOn, _ := n["depends_on"].([]string)
		toInject = append(toInject, InjectedNode{
			ID:        id,
			NodeType:  nodeType,
			DependsOn: dependsOn,
		})
	}

	return &SupervisorActionError{
		Action:        "inject",
		Target:        target,
		Reason:        getString(decision, "reason"),
		InjectedNodes: toInject,
	}
}

func (e *Executor) finishTask(ctx context.Context, task *model.TaskRun, out map[string]any, err error) error {
	now := time.Now()
	task.FinishedAt = &now
	if err != nil {
		task.Status = model.Failed
		task.Error = err.Error()
	} else {
		task.Status = model.Success
		task.Output = out
	}
	task.UpdatedAt = now
	return e.store.UpdateTaskRun(ctx, task)
}

func resolveInput(input map[string]any, scope map[string]map[string]any) map[string]any {
	if input == nil {
		return make(map[string]any)
	}
	resolved := make(map[string]any)
	for k, v := range input {
		resolved[k] = util.Resolve(v, scope)
	}
	return resolved
}

func formatInput(input map[string]any) string {
	b, _ := json.Marshal(input)
	return string(b)
}

func extractReviews(input map[string]any) []map[string]any {
	var reviews []map[string]any
	for _, v := range input {
		if m, ok := v.(map[string]any); ok {
			reviews = append(reviews, m)
		}
	}
	return reviews
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func parseJSON(data string, v any) error {
	return json.Unmarshal([]byte(data), v)
}

func containsAction(actions []string, action string) bool {
	for _, a := range actions {
		if a == action {
			return true
		}
	}
	return false
}
