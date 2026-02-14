package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/src/api"
	"github.com/flowgent-labs/flowgent/src/config"
	"github.com/flowgent-labs/flowgent/src/engine"
	"github.com/flowgent-labs/flowgent/src/model"
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
		return "", fmt.Errorf("transient LLM failure")
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
	store, jm := engine.NewTestJobManager(nil, agents, &e2eLLM{})

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
			{ID: "tribunal", Type: model.TribunalNode, Strategy: map[string]any{"type": "majority"}},
			{ID: "approved", Type: model.ConditionNode, Expression: "${tribunal.decision == true}"},
			{ID: "end", Type: model.NoopNode},
		},
		Edges: []model.Edge{
			{From: "detect", To: "fix"},
			{From: "fix", To: "review"},
			{From: "review", To: "tribunal"},
			{From: "tribunal", To: "approved"},
			{From: "approved", To: "end"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := jm.StartJob(ctx, run, spec); err != nil {
		t.Fatalf("execute: %v", err)
	}

	finalRun := store.Runs["test-run-001"]
	if finalRun.Status != model.RunCompleted {
		t.Errorf("expected COMPLETED, got %s", finalRun.Status)
	}
	t.Logf("Basic agentflow: %d tasks, status=%s", len(store.Tasks), finalRun.Status)
}

func TestE2E_SupervisorAllowedActions(t *testing.T) {
	agents := []*config.AgentDef{
		{Name: "supervisor", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Supervisor."},
	}
	store, jm := engine.NewTestJobManager(nil, agents, &e2eLLM{})

	run := &model.AgentFlowRun{
		ID: "test-run-aa", AgentFlowID: "test-aa", Version: 1,
		Status: model.RunPending, Trigger: model.TriggerInfo{Type: "manual", Source: "test"},
	}
	store.CreateAgentFlowRun(context.Background(), run)

	spec := &model.AgentFlowSpec{
		ID: "test-aa",
		Nodes: []model.Node{
			{ID: "work", Type: model.NoopNode},
			{ID: "sup", Type: model.SupervisorNode, Agent: "supervisor",
				SupervisorConfig: &model.SupervisorConfig{AllowedActions: []string{"continue"}}},
			{ID: "end", Type: model.NoopNode},
		},
		Edges: []model.Edge{{From: "work", To: "sup"}, {From: "sup", To: "end"}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := jm.StartJob(ctx, run, spec)
	if err != nil {
		t.Logf("supervisor validation: error returned: %v", err)
	} else {
		finalRun := store.Runs["test-run-aa"]
		if finalRun.Status != model.RunFailed {
			t.Fatalf("expected FAILED, got %s", finalRun.Status)
		}
	}
}

func TestE2E_MapNodeExecution(t *testing.T) {
	agents := []*config.AgentDef{
		{Name: "issue-detector", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Security expert."},
	}
	store, jm := engine.NewTestJobManager(nil, agents, &e2eLLM{})

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

	if err := jm.StartJob(ctx, run, spec); err != nil {
		t.Fatalf("map workflow: %v", err)
	}
	t.Logf("Map node: %d tasks", len(store.Tasks))
}

func TestE2E_NodeRetry(t *testing.T) {
	agents := []*config.AgentDef{
		{Name: "issue-detector", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Security expert."},
	}
	store, jm := engine.NewTestJobManager(nil, agents, &e2eFailingLLM{failCount: 1})

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

	if err := jm.StartJob(ctx, run, spec); err != nil {
		t.Fatalf("retry workflow: %v", err)
	}
	t.Logf("Node retry: %d tasks", len(store.Tasks))
}

func TestE2E_DAGExecutor(t *testing.T) {
	jm := &engine.JobManager{}
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
	jm := &engine.JobManager{}
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

	jm.Done("B")
	jm.Done("D")
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
	jm := &engine.JobManager{}
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

func TestE2E_WebhookTriggerMatch(t *testing.T) {
	flows := []model.AgentFlowSpec{
		{
			ID: "sec-fixer",
			Triggers: []model.TriggerDef{
				{Type: "webhook", Provider: "github", Events: []string{"push", "pull_request"}},
			},
		},
	}
	tests := []struct {
		provider, event string
		expectLen       int
	}{
		{"github", "push", 1},
		{"github", "pull_request", 1},
		{"github", "issues", 0},
		{"gitlab", "push", 0},
	}
	for _, tt := range tests {
		matched := api.MatchWebhookTrigger(flows, tt.provider, tt.event)
		if len(matched) != tt.expectLen {
			t.Errorf("%s/%s: expected %d, got %d: %v", tt.provider, tt.event, tt.expectLen, len(matched), matched)
		}
	}
}
