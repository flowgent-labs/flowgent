package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/pkg/config"
	"github.com/flowgent-labs/flowgent/pkg/engine"
	"github.com/flowgent-labs/flowgent/pkg/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/pkg/model"
	"github.com/flowgent-labs/flowgent/pkg/queue"
	"github.com/flowgent-labs/flowgent/pkg/common/utils"
	"github.com/flowgent-labs/flowgent/tests/testutil"
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
	llm := &secFixLLM{}
	mcp := &secFixMCP{}
	mcpMap := map[string]engine.MCPClient{
		"github": mcp, "sonarqube": mcp, "sonatype-iq": mcp, "sonatype-nexus3": mcp,
	}

	agents := []*config.AgentInfo{
		{Name: "supervisor", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Supervisor.", Instruction: "Output action/target/reason JSON."},
		{Name: "issue-detector", Model: "bailian-codeplan/qwen3.6-plus", Soul: "DevSecOps expert.", Instruction: "Parse scan results."},
		{Name: "fixer-agent", Model: "bailian-codeplan/qwen3.5-coder", Soul: "Secure coding expert.", Instruction: "Generate secure patches."},
		{Name: "security-reviewer", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Security reviewer.", Instruction: "Review patches."},
		{Name: "quality-reviewer", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Code quality reviewer.", Instruction: "Review quality."},
		{Name: "arch-reviewer", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Architecture reviewer.", Instruction: "Review architecture."},
		{Name: "git-agent", Model: "bailian-codeplan/qwen3.5-coder", Soul: "Git operations.", Instruction: "Handle git."},
	}

	store := testutil.NewMockStore()
	rm, err := resourcemanager.NewLocalResourceManager(&resourcemanager.ResourceManagerConfig{
		Provider: engine.ProviderLocal, PoolSize: 10,
		Store: store, Agents: agents, MCPClients: mcpMap, LLMClient: llm,
		Logger: utils.NewLogger("JSON", "DEBUG"),
	})
	cfg := &config.ServiceConfig{
		Orchestration: config.OrchestrationConfig{
			FlowExecutionTimeout: "60s",
			MaxNodeRetries:       3,
			MaxConcurrentFlows:   10,
		},
	}
	jm, err := jobmanager.NewJobManager(store, rm, utils.NewLogger("JSON", "DEBUG"), cfg)
	if err != nil {
		t.Fatalf("create JM: %v", err)
	}

	spec := &entities.AgentFlowInfo{
		ID:          "security-autonomy-fixer",
		Description: "E2E security fix pipeline",
		Priority:    entities.PriorityMedium,
		TenantID:    "default",
		Vars:        map[string]any{"repos": []any{"org/repo1"}},
		Nodes: []entities.Node{
			{ID: "scan-sonarqube", Type: entities.ToolNode, Tool: "sonarqube", Input: map[string]any{"action": "get_issues"}},
			{ID: "aggregate-issues", Type: entities.AgentNode, Agent: "issue-detector"},
			{ID: "generate-fixes", Type: entities.AgentNode, Agent: "fixer-agent"},
			{ID: "review-sec", Type: entities.AgentNode, Agent: "security-reviewer"},
			{ID: "review-quality", Type: entities.AgentNode, Agent: "quality-reviewer"},
			{ID: "review-arch", Type: entities.AgentNode, Agent: "arch-reviewer"},
			{ID: "tribunal", Type: entities.TribunalNode, Strategy: map[string]any{"type": "majority"}},
			{ID: "supervisor-check", Type: entities.SupervisorNode, Agent: "supervisor",
				SupervisorConfig: &entities.SupervisorConfig{AllowedActions: []string{"continue", "retry", "inject", "abort"}, MaxInjections: 3, MaxRetries: 3, MaxNodes: 50}},
			{ID: "approved", Type: entities.ConditionNode, Expression: "${tribunal.decision == true}"},
			{ID: "create-pr", Type: entities.ToolNode, Tool: "github", Input: map[string]any{"action": "create_pull_request"}},
			{ID: "summary-report", Type: entities.AgentNode, Agent: "issue-detector"},
			{ID: "notify-pr", Type: entities.ToolNode, Tool: "github", Input: map[string]any{"action": "create_issue_comment"}},
			{ID: "end", Type: entities.NoopNode},
		},
		Edges: []entities.Edge{
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
			{From: "approved", To: "create-pr", Condition: testutil.BoolPtr(true)},
			{From: "approved", To: "end", Condition: testutil.BoolPtr(false)},
			{From: "create-pr", To: "summary-report"},
			{From: "summary-report", To: "notify-pr"},
			{From: "notify-pr", To: "end"},
		},
	}

	run := &entities.FlowRunInfo{
		ID: "e2e-secfix-local", AgentFlowID: "security-autonomy-fixer", Version: 1,
		Status: entities.RunPending, Trigger: entities.TriggerInfo{Type: "manual", Source: "e2e"},
		Vars:    map[string]any{"repos": []any{"org/repo1"}},
		Priority: entities.PriorityMedium, TenantID: "default",
	}
	store.CreateAgentFlowRun(context.Background(), run)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err = jm.Submit(ctx, run, spec)
	if err != nil {
		t.Logf("pipeline result: %v", err)
	}

	finalRun, _ := store.GetAgentFlowRun(context.Background(), "e2e-secfix-local")
	tasks, _ := store.GetTaskRunsByAgentFlowRun(context.Background(), "e2e-secfix-local")
	for _, tk := range tasks {
		t.Logf("  task: node=%s status=%s error=%s", tk.NodeID, tk.Status, tk.Error)
	}
	if finalRun != nil {
		t.Logf("E2E Local: %d tasks, status=%s", len(tasks), finalRun.Status)
	}
}

// ─── E2E: MQTT Queue Integration ─────────────────────

func TestE2E_MQTTQueue_Local(t *testing.T) {
	q, err := queue.NewMQTTQueue(&queue.MQTTConfig{
		Broker:   "tcp://127.0.0.1:1883",
		ClientID: "e2e-test",
		Topic:    "flowgent/e2e",
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
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
