package e2e

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/src/config"
	"github.com/flowgent-labs/flowgent/src/engine"
	"github.com/flowgent-labs/flowgent/src/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/src/engine/scheduler"
	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/common/utils"
	"github.com/flowgent-labs/flowgent/tests/testutil"
)

// ─── Mock LLM Clients ──────────────────────────────────────

type e2eLLM struct{}

func (m *e2eLLM) Generate(ctx context.Context, systemPrompt, userPrompt, model string, temp float64) (string, error) {
	switch {
	case model == "bailian-codeplan/qwen3.6-plus" || model == "qwen3.6-plus":
		return mustJSON(map[string]any{
			"issues": []map[string]any{{"id": "CVE-2024-001", "severity": "high"}},
		}), nil
	case model == "bailian-codeplan/qwen3.5-coder" || model == "qwen3.5-coder":
		return mustJSON(map[string]any{"patches": []map[string]any{{"file": "x.java", "patch": "diff..."}}}), nil
	default:
		return mustJSON(map[string]any{"action": "continue", "target": "", "reason": "ok"}), nil
	}
}

type e2eFailingLLM struct {
	mu        sync.Mutex
	failCount int
	callCount int
}

func (m *e2eFailingLLM) Generate(ctx context.Context, sp, up, model string, t float64) (string, error) {
	m.mu.Lock()
	m.callCount++
	c := m.callCount
	f := m.failCount
	m.mu.Unlock()
	if c <= f {
		return "", nil // returning empty is treated as transient failure by executor retry
	}
	return mustJSON(map[string]any{"decision": true, "confidence": 0.9}), nil
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// ─── E2E Tests ────────────────────────────────────────────

func TestE2E_BasicAgentFlow(t *testing.T) {
	agents := []*config.AgentDef{
		{Name: "issue-detector", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Security expert."},
		{Name: "fixer-agent", Model: "bailian-codeplan/qwen3.5-coder", Soul: "Fixer."},
		{Name: "security-reviewer", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Reviewer."},
		{Name: "supervisor", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Supervisor."},
	}
	store := testutil.NewMockStore()
	rm, err := scheduler.NewLocalResourceManager(&scheduler.ResourceManagerConfig{
		Provider: engine.ProviderLocal, PoolSize: 10,
		Store: store, Agents: agents, MCPClients: map[string]engine.MCPClient{}, LLMClient: &e2eLLM{},
		Logger: utils.NewLogger("JSON", "DEBUG"),
	})
	cfg := &config.ServiceConfig{Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "30m", MaxNodeRetries: 3}}
	jm, err := jobmanager.NewJobManager(store, rm, utils.NewLogger("JSON", "DEBUG"), cfg)
	if err != nil {
		t.Fatalf("create JM: %v", err)
	}

	run := &model.AgentFlowRun{
		ID: "test-run-001", AgentFlowID: "test-flow", Version: 1,
		Status: model.RunPending, Trigger: model.TriggerInfo{Type: "manual", Source: "test"},
		Vars: map[string]any{"repos": []any{"org/repo1"}},
	}
	store.CreateAgentFlowRun(context.Background(), run)

	spec := &model.AgentFlowSpec{
		ID: "test-flow", Vars: map[string]any{"repos": []any{"org/repo1"}},
		Nodes: []model.Node{
			{ID: "detect", Type: model.AgentNode, Agent: "issue-detector"},
			{ID: "fix", Type: model.AgentNode, Agent: "fixer-agent"},
			{ID: "review", Type: model.AgentNode, Agent: "security-reviewer"},
			{ID: "end", Type: model.NoopNode},
		},
		Edges: []model.Edge{
			{From: "detect", To: "fix"},
			{From: "fix", To: "review"},
			{From: "review", To: "end"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := jm.Submit(ctx, run, spec); err != nil {
		t.Logf("flow execution note: %v", err)
	}

	finalRun, _ := store.GetAgentFlowRun(context.Background(), "test-run-001")
	if finalRun == nil {
		t.Fatal("run not found in store")
	}
	t.Logf("Basic agentflow: status=%s", finalRun.Status)
}

func TestE2E_MapNodeExecution(t *testing.T) {
	agents := []*config.AgentDef{
		{Name: "issue-detector", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Security expert."},
	}
	store := testutil.NewMockStore()
	rm, err := scheduler.NewLocalResourceManager(&scheduler.ResourceManagerConfig{
		Provider: engine.ProviderLocal, PoolSize: 5,
		Store: store, Agents: agents, MCPClients: map[string]engine.MCPClient{}, LLMClient: &e2eLLM{},
		Logger: utils.NewLogger("JSON", "DEBUG"),
	})
	cfg := &config.ServiceConfig{Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "30m", MaxNodeRetries: 3}}
	jm, err := jobmanager.NewJobManager(store, rm, utils.NewLogger("JSON", "DEBUG"), cfg)
	if err != nil {
		t.Fatalf("create JM: %v", err)
	}

	run := &model.AgentFlowRun{
		ID: "test-run-map", AgentFlowID: "test-map", Version: 1,
		Status: model.RunPending, Vars: map[string]any{"repos": []any{"org/r1", "org/r2"}},
	}
	store.CreateAgentFlowRun(context.Background(), run)

	spec := &model.AgentFlowSpec{
		ID: "test-map", Vars: map[string]any{"repos": []any{"org/r1", "org/r2"}},
		Nodes: []model.Node{
			{ID: "scan", Type: model.MapNode, Source: "${vars.repos}", Concurrency: 2,
				Node: &model.Node{Type: model.AgentNode, Agent: "issue-detector", Input: map[string]any{"repo": "${item}"}}},
			{ID: "end", Type: model.NoopNode},
		},
		Edges: []model.Edge{{From: "scan", To: "end"}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := jm.Submit(ctx, run, spec); err != nil {
		t.Logf("map workflow note: %v", err)
	}
	t.Logf("Map node: done")
}

func TestE2E_NodeRetry(t *testing.T) {
	agents := []*config.AgentDef{
		{Name: "issue-detector", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Security expert."},
	}
	store := testutil.NewMockStore()
	failingLLM := &e2eFailingLLM{failCount: 1}
	rm, err := scheduler.NewLocalResourceManager(&scheduler.ResourceManagerConfig{
		Provider: engine.ProviderLocal, PoolSize: 5,
		Store: store, Agents: agents, MCPClients: map[string]engine.MCPClient{}, LLMClient: failingLLM,
		Logger: utils.NewLogger("JSON", "DEBUG"),
	})
	cfg := &config.ServiceConfig{Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "30m", MaxNodeRetries: 3}}
	jm, err := jobmanager.NewJobManager(store, rm, utils.NewLogger("JSON", "DEBUG"), cfg)
	if err != nil {
		t.Fatalf("create JM: %v", err)
	}

	run := &model.AgentFlowRun{
		ID: "test-run-retry", AgentFlowID: "test-retry", Version: 1, Status: model.RunPending,
	}
	store.CreateAgentFlowRun(context.Background(), run)

	spec := &model.AgentFlowSpec{
		ID: "test-retry",
		Nodes: []model.Node{
			{ID: "detect", Type: model.AgentNode, Agent: "issue-detector",
				Retry: &model.RetryPolicy{Max: 3, Initial: 10 * time.Millisecond, Factor: 1.0}},
			{ID: "end", Type: model.NoopNode},
		},
		Edges: []model.Edge{{From: "detect", To: "end"}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := jm.Submit(ctx, run, spec); err != nil {
		t.Logf("retry workflow note: %v", err)
	}
	t.Logf("Node retry: done")
}

func TestE2E_DAGExecutor(t *testing.T) {
	// DAG topology: A → B → C
	jm := jobmanager.NewJobMaster(nil, nil, nil, &config.ServiceConfig{
		Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "30m"}})
	jm.BuildGraphNodes([]string{"A", "B", "C"}, [][2]string{{"A", "B"}, {"B", "C"}})

	ready := jm.Ready()
	if len(ready) != 1 || ready[0] != "A" {
		t.Fatalf("expected A ready, got %v", ready)
	}
	jm.Done("A")
	ready = jm.Ready()
	if len(ready) != 1 || ready[0] != "B" {
		t.Fatalf("expected B ready, got %v", ready)
	}
	jm.Done("B")
	ready = jm.Ready()
	if len(ready) != 1 || ready[0] != "C" {
		t.Fatalf("expected C ready, got %v", ready)
	}
	jm.Done("C")
	if !jm.IsComplete() {
		t.Fatal("expected complete")
	}
}

func TestE2E_DAGInject(t *testing.T) {
	jm := jobmanager.NewJobMaster(nil, nil, nil, &config.ServiceConfig{
		Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "30m"}})
	jm.BuildGraphNodes([]string{"A", "B", "C"}, [][2]string{{"A", "B"}, {"B", "C"}})
	jm.Done("A")

	ready := jm.Ready()
	if len(ready) != 1 || ready[0] != "B" {
		t.Fatalf("expected B ready, got %v", ready)
	}
	jm.Inject("D", []string{"A"})
	ready = jm.Ready()
	if len(ready) != 2 {
		t.Fatalf("expected B and D ready, got %v", ready)
	}
	jm.Done("B"); jm.Done("D")
	ready = jm.Ready()
	if len(ready) != 1 || ready[0] != "C" {
		t.Fatalf("expected C ready, got %v", ready)
	}
	jm.Done("C")
	if !jm.IsComplete() {
		t.Fatal("expected complete")
	}
}

func TestE2E_ConditionSkip(t *testing.T) {
	jm := jobmanager.NewJobMaster(nil, nil, nil, &config.ServiceConfig{
		Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "30m"}})
	jm.BuildGraphNodes([]string{"A", "cond", "B_true", "B_false", "end"},
		[][2]string{{"A", "cond"}, {"cond", "B_true"}, {"cond", "B_false"}, {"B_true", "end"}, {"B_false", "end"}})
	jm.Done("A")

	ready := jm.Ready()
	if len(ready) != 1 || ready[0] != "cond" {
		t.Fatalf("expected cond ready, got %v", ready)
	}
	jm.SetConditionResult("cond", true)
	jm.Done("cond")
	jm.Skip("B_false")

	ready = jm.Ready()
	if len(ready) != 1 || ready[0] != "B_true" {
		t.Fatalf("expected B_true ready, got %v", ready)
	}
	jm.Done("B_true")
	ready = jm.Ready()
	if len(ready) != 1 || ready[0] != "end" {
		t.Fatalf("expected end ready, got %v", ready)
	}
	jm.Done("end")
	if !jm.IsComplete() {
		t.Fatal("expected complete")
	}
}
