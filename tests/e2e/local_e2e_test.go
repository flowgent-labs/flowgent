package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/src/engine"
	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/config"
	"github.com/flowgent-labs/flowgent/src/queue"
)

// ─── Mock LLM ────────────────────────────────────────

type secFixLLM struct{}

func (m *secFixLLM) Generate(ctx context.Context, sp, up, modelStr string, t float64) (string, error) {
	switch {
	case strings.Contains(sp, "DevSecOps") || strings.Contains(sp, "security expert"):
		return toJSON(map[string]any{
			"issues": []map[string]any{{"id": "CVE-001", "severity": "high", "type": "sast"}},
		}), nil
	case strings.Contains(sp, "Secure coding") || strings.Contains(sp, "fixer"):
		return toJSON(map[string]any{
			"patches": []map[string]any{{"file": "x.java", "patch": "diff"}},
		}), nil
	case strings.Contains(sp, "reviewer") || strings.Contains(sp, "Reviewer"):
		return toJSON(map[string]any{"decision": true, "confidence": 0.9, "risk_level": "low", "reason": "ok"}), nil
	default:
		return toJSON(map[string]any{"action": "continue", "target": "", "reason": "proceed"}), nil
	}
}

// ─── Mock MCP ────────────────────────────────────────

type secFixMCP struct{}

func (m *secFixMCP) CallTool(ctx context.Context, toolName string, args map[string]any) (map[string]any, error) {
	return map[string]any{"result": "ok", "tool": toolName}, nil
}

// ─── E2E: Full Security Fix Pipeline (Local Mode) ─────

func TestE2E_SecurityFixPipeline_Local(t *testing.T) {
	store, rt := engine.NewTestRuntime()
	llm := &secFixLLM{}
	mcp := &secFixMCP{}
	mcpMap := map[string]engine.MCPClient{
		"github": mcp, "sonarqube": mcp, "sonatype-iq": mcp, "sonatype-nexus3": mcp,
	}

	agents := []*config.AgentDef{
		{Name: "supervisor", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Supervisor.", Instruction: "Output action/target/reason JSON."},
		{Name: "issue-detector", Model: "bailian-codeplan/qwen3.6-plus", Soul: "DevSecOps expert.", Instruction: "Parse scan results."},
		{Name: "fixer-agent", Model: "bailian-codeplan/qwen3.5-coder", Soul: "Secure coding expert.", Instruction: "Generate secure patches."},
		{Name: "security-reviewer", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Security reviewer.", Instruction: "Review patches."},
		{Name: "quality-reviewer", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Code quality reviewer.", Instruction: "Review quality."},
		{Name: "arch-reviewer", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Architecture reviewer.", Instruction: "Review architecture."},
		{Name: "git-agent", Model: "bailian-codeplan/qwen3.5-coder", Soul: "Git operations.", Instruction: "Handle git."},
	}

	exec := engine.NewTestExecutor(store, mcpMap, agents, llm)
	rt.SetExecutor(exec)
	rt.SetTimeout(60 * time.Second)

	spec := &model.AgentFlowSpec{
		ID:          "security-autonomy-fixer",
		Description: "E2E security fix pipeline",
		Vars:        map[string]any{"repos": []any{"org/repo1"}},
		Nodes: []model.Node{
			{ID: "scan-sonarqube", Type: model.ToolNode, Tool: "sonarqube", Input: map[string]any{"action": "get_issues"}},
			{ID: "aggregate-issues", Type: model.AgentNode, Agent: "issue-detector"},
			{ID: "generate-fixes", Type: model.AgentNode, Agent: "fixer-agent"},
			{ID: "review-sec", Type: model.AgentNode, Agent: "security-reviewer"},
			{ID: "review-quality", Type: model.AgentNode, Agent: "quality-reviewer"},
			{ID: "review-arch", Type: model.AgentNode, Agent: "arch-reviewer"},
			{ID: "tribunal", Type: model.TribunalNode, Strategy: map[string]any{"type": "majority"}},
			{ID: "supervisor-check", Type: model.SupervisorNode, Agent: "supervisor",
				SupervisorConfig: &model.SupervisorConfig{AllowedActions: []string{"continue", "retry", "inject", "abort"}, MaxInjections: 3, MaxRetries: 3, MaxNodes: 50}},
			{ID: "approved", Type: model.ConditionNode, Expression: "${tribunal.decision == true}"},
			{ID: "create-pr", Type: model.ToolNode, Tool: "github", Input: map[string]any{"action": "create_pull_request"}},
			{ID: "summary-report", Type: model.AgentNode, Agent: "issue-detector"},
			{ID: "notify-pr", Type: model.ToolNode, Tool: "github", Input: map[string]any{"action": "create_issue_comment"}},
			{ID: "end", Type: model.NoopNode},
		},
		Edges: []model.Edge{
			{From: "scan-sonarqube", To: "aggregate-issues"},
			{From: "aggregate-issues", To: "generate-fixes"},
			{From: "generate-fixes", To: "review-sec"},
			{From: "generate-fixes", To: "review-quality"},
			{From: "generate-fixes", To: "review-arch"},
			{From: "review-sec", To: "tribunal"},
			{From: "review-quality", To: "tribunal"},
			{From: "review-arch", To: "tribunal"},
			{From: "tribunal", To: "supervisor-check"},
			{From: "supervisor-check", To: "approved"},
			{From: "approved", To: "create-pr"},
			{From: "create-pr", To: "summary-report"},
			{From: "summary-report", To: "notify-pr"},
			{From: "notify-pr", To: "end"},
		},
	}

	run := &model.AgentFlowRun{
		ID: "e2e-secfix-local", AgentFlowID: "security-autonomy-fixer", Version: 1, Status: model.RunPending,
		Trigger: model.TriggerInfo{Type: "manual", Source: "e2e"},
		Vars:    map[string]any{"repos": []any{"org/repo1"}},
	}
	store.CreateAgentFlowRun(context.Background(), run)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := rt.Execute(ctx, run, spec); err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}

	finalRun, _ := store.GetAgentFlowRun(context.Background(), "e2e-secfix-local")
	tasks, _ := store.GetTaskRunsByAgentFlowRun(context.Background(), "e2e-secfix-local")
	for _, tk := range tasks {
		t.Logf("  task: node=%s status=%s error=%s", tk.NodeID, tk.Status, tk.Error)
	}
	if finalRun == nil || finalRun.Status != model.RunCompleted {
		t.Fatalf("expected COMPLETED, got status=%s error=%s (tasks=%d)", finalRun.Status, finalRun.Error, len(tasks))
	}
	t.Logf("E2E Local: %d tasks, status=%s", len(tasks), finalRun.Status)
}

// ─── E2E: MQTT Queue Integration ─────────────────────

func TestE2E_MQTTQueue_Local(t *testing.T) {
	q, err := queue.NewMQTTQueue(&queue.MQTTConfig{
		Broker:   "tcp://127.0.0.1:1883",
		ClientID: "e2e-test",
		Topic:    "flowgent/e2e",
	})
	if err != nil {
		t.Skipf("MQTT not available: %v", err)
		return
	}
	defer q.Close()
	ctx := context.Background()
	msg := &queue.Message{ID: "mqtt-1", TaskRunID: "r1", NodeID: "n1", Payload: []byte("hello")}
	if err := q.Push(ctx, msg); err != nil {
		t.Fatalf("push: %v", err)
	}
	got, err := q.Pop(ctx, 5*time.Second)
	if err != nil || got.ID != "mqtt-1" {
		t.Fatalf("pop failed: err=%v, got=%v", err, got)
	}
	t.Logf("MQTT E2E: OK")
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

type failingLLM struct {
	mu        sync.Mutex
	failCount int
	calls     int
}

func (m *failingLLM) Generate(ctx context.Context, sp, up, modelStr string, t float64) (string, error) {
	m.mu.Lock()
	m.calls++
	c := m.calls
	fc := m.failCount
	m.mu.Unlock()
	if c <= fc {
		return "", fmt.Errorf("transient failure %d", c)
	}
	return toJSON(map[string]any{"issues": []any{}}), nil
}
