package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/util"
)

// ─── In-Memory Store for Testing ──────────────────────────

type mockStore struct {
	mu     sync.Mutex
	runs   map[string]*model.AgentFlowRun
	tasks  map[string]*model.TaskRun
	humans map[string]*model.HumanApproval
}

func newMockStore() *mockStore {
	return &mockStore{
		runs:   make(map[string]*model.AgentFlowRun),
		tasks:  make(map[string]*model.TaskRun),
		humans: make(map[string]*model.HumanApproval),
	}
}

func (s *mockStore) CreateAgentFlowRun(ctx context.Context, run *model.AgentFlowRun) error {
	s.mu.Lock(); defer s.mu.Unlock()
	s.runs[run.ID] = run
	return nil
}
func (s *mockStore) UpdateAgentFlowRun(ctx context.Context, run *model.AgentFlowRun) error {
	s.mu.Lock(); defer s.mu.Unlock()
	s.runs[run.ID] = run
	return nil
}
func (s *mockStore) GetAgentFlowRun(ctx context.Context, id string) (*model.AgentFlowRun, error) {
	s.mu.Lock(); defer s.mu.Unlock()
	return s.runs[id], nil
}
func (s *mockStore) ListAgentFlowRuns(ctx context.Context, fid string, limit int) ([]model.AgentFlowRun, error) {
	s.mu.Lock(); defer s.mu.Unlock()
	var out []model.AgentFlowRun
	for _, r := range s.runs {
		if fid == "" || r.AgentFlowID == fid {
			out = append(out, *r)
		}
	}
	return out, nil
}
func (s *mockStore) ListActiveRuns(ctx context.Context) ([]model.AgentFlowRun, error) {
	return nil, nil
}
func (s *mockStore) CreateTaskRun(ctx context.Context, task *model.TaskRun) error {
	s.mu.Lock(); defer s.mu.Unlock()
	task.ID = task.ExecID
	s.tasks[task.ID] = task
	return nil
}
func (s *mockStore) UpdateTaskRun(ctx context.Context, task *model.TaskRun) error {
	s.mu.Lock(); defer s.mu.Unlock()
	s.tasks[task.ID] = task
	return nil
}
func (s *mockStore) GetTaskRun(ctx context.Context, id string) (*model.TaskRun, error) {
	s.mu.Lock(); defer s.mu.Unlock()
	return s.tasks[id], nil
}
func (s *mockStore) GetTaskRunsByAgentFlowRun(ctx context.Context, runID string) ([]model.TaskRun, error) {
	s.mu.Lock(); defer s.mu.Unlock()
	var out []model.TaskRun
	for _, t := range s.tasks {
		if t.AgentFlowRunID == runID {
			out = append(out, *t)
		}
	}
	return out, nil
}
func (s *mockStore) CreateHumanApproval(ctx context.Context, a *model.HumanApproval) error {
	s.mu.Lock(); defer s.mu.Unlock()
	s.humans[a.TaskRunID] = a
	return nil
}
func (s *mockStore) GetHumanApproval(ctx context.Context, token string) (*model.HumanApproval, error) {
	s.mu.Lock(); defer s.mu.Unlock()
	for _, a := range s.humans {
		if a.Token == token {
			return a, nil
		}
	}
	return nil, nil
}
func (s *mockStore) UpdateHumanApproval(ctx context.Context, a *model.HumanApproval) error {
	s.mu.Lock(); defer s.mu.Unlock()
	s.humans[a.TaskRunID] = a
	return nil
}
func (s *mockStore) LogSupervisorDecision(ctx context.Context, arID, trID string, input, decision map[string]any) error {
	return nil
}
func (s *mockStore) CheckIdempotency(ctx context.Context, key string) (bool, error) { return false, nil }
func (s *mockStore) AcquireIdempotency(ctx context.Context, key, trID, execID string) error {
	return nil
}
func (s *mockStore) GetTaskRunByExecID(ctx context.Context, execID string) (*model.TaskRun, error) {
	s.mu.Lock(); defer s.mu.Unlock()
	return s.tasks[execID], nil
}

// Required by store.Store — satisfy the remaining interface methods
func (s *mockStore) SaveAgentFlowDefinition(ctx context.Context, d *model.AgentFlowVersion) error { return nil }
func (s *mockStore) GetLatestAgentFlowDefinition(ctx context.Context, id string) (*model.AgentFlowVersion, error) { return nil, nil }
func (s *mockStore) GetAgentFlowDefinition(ctx context.Context, id string, v int64) (*model.AgentFlowVersion, error) { return nil, nil }
func (s *mockStore) ListAgentFlowDefinitions(ctx context.Context) ([]model.AgentFlowVersion, error) { return nil, nil }
func (s *mockStore) GetPendingApprovals(ctx context.Context) ([]model.HumanApproval, error) { return nil, nil }
func (s *mockStore) DB() interface{ Close() error } { return nil }

// ─── Mock LLM Client ────────────────────────────────────

type mockLLMClient struct{}

func (m *mockLLMClient) Generate(ctx context.Context, systemPrompt, userPrompt, model string, temp float64) (string, error) {
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

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// ─── E2E Tests ──────────────────────────────────────────

func TestE2E_BasicAgentFlow(t *testing.T) {
	store := newMockStore()
	logger := util.NewLogger("JSON", "DEBUG")
	agents := []*model.AgentDef{
		{Name: "issue-detector", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Security expert."},
		{Name: "fixer-agent", Model: "bailian-codeplan/qwen3.5-coder", Soul: "Fixer."},
		{Name: "security-reviewer", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Reviewer."},
		{Name: "supervisor", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Supervisor."},
	}
	llm := &mockLLMClient{}
	exec := NewExecutor(store, nil, agents, llm, logger)
	rt := NewAgentFlowRuntime(store, logger)
	rt.SetExecutor(exec)

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

	if err := rt.Execute(ctx, run, spec); err != nil {
		t.Fatalf("execute: %v", err)
	}

	finalRun := store.runs["test-run-001"]
	if finalRun.Status != model.RunCompleted {
		t.Errorf("expected COMPLETED, got %s", finalRun.Status)
	}
	t.Logf("Basic agentflow: %d tasks, status=%s", len(store.tasks), finalRun.Status)
}

func TestE2E_SupervisorAllowedActions(t *testing.T) {
	store := newMockStore()
	logger := util.NewLogger("JSON", "DEBUG")
	llm := &mockLLMClient{}
	agents := []*model.AgentDef{
		{Name: "supervisor", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Supervisor."},
	}
	exec := NewExecutor(store, nil, agents, llm, logger)
	rt := NewAgentFlowRuntime(store, logger)
	rt.SetExecutor(exec)

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

	err := rt.Execute(ctx, run, spec)
	if err != nil {
		t.Logf("supervisor validation: error returned: %v", err)
	} else {
		finalRun := store.runs["test-run-aa"]
		if finalRun.Status != model.RunFailed {
			t.Fatalf("expected FAILED, got %s", finalRun.Status)
		}
	}
}

func TestE2E_MapNodeExecution(t *testing.T) {
	store := newMockStore()
	logger := util.NewLogger("JSON", "DEBUG")
	llm := &mockLLMClient{}
	agents := []*model.AgentDef{
		{Name: "issue-detector", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Security expert."},
	}
	exec := NewExecutor(store, nil, agents, llm, logger)
	rt := NewAgentFlowRuntime(store, logger)
	rt.SetExecutor(exec)

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

	if err := rt.Execute(ctx, run, spec); err != nil {
		t.Fatalf("map workflow: %v", err)
	}

	t.Logf("Map node: %d tasks", len(store.tasks))
}

func TestE2E_NodeRetry(t *testing.T) {
	store := newMockStore()
	logger := util.NewLogger("JSON", "DEBUG")
	failingLLM := &failingLLMClient{failCount: 1}
	agents := []*model.AgentDef{
		{Name: "issue-detector", Model: "bailian-codeplan/qwen3.6-plus", Soul: "Security expert."},
	}
	exec := NewExecutor(store, nil, agents, failingLLM, logger)
	rt := NewAgentFlowRuntime(store, logger)
	rt.SetExecutor(exec)

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

	if err := rt.Execute(ctx, run, spec); err != nil {
		t.Fatalf("retry workflow: %v", err)
	}
	t.Logf("Node retry: %d tasks", len(store.tasks))
}

type failingLLMClient struct {
	mu        sync.Mutex
	failCount int
	callCount int
}

func (m *failingLLMClient) Generate(ctx context.Context, sp, up, model string, t float64) (string, error) {
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

func TestE2E_DAGScheduler(t *testing.T) {
	s := NewDAGScheduler([]string{"A", "B", "C"}, [][2]string{{"A", "B"}, {"B", "C"}})

	ready := s.Ready()
	if len(ready) != 1 || ready[0] != "A" {
		t.Fatalf("expected A ready, got %v", ready)
	}

	s.Done("A")
	ready = s.Ready()
	if len(ready) != 1 || ready[0] != "B" {
		t.Fatalf("expected B ready, got %v", ready)
	}

	s.Done("B")
	ready = s.Ready()
	if len(ready) != 1 || ready[0] != "C" {
		t.Fatalf("expected C ready, got %v", ready)
	}

	s.Done("C")
	if !s.IsComplete() {
		t.Fatal("expected complete")
	}
}

func TestE2E_DAGInject(t *testing.T) {
	s := NewDAGScheduler([]string{"A", "B", "C"}, [][2]string{{"A", "B"}, {"B", "C"}})
	s.Done("A")

	ready := s.Ready()
	if len(ready) != 1 || ready[0] != "B" {
		t.Fatalf("expected B ready, got %v", ready)
	}

	s.Inject("D", []string{"A"})
	ready = s.Ready()
	if len(ready) != 2 {
		t.Fatalf("expected B and D ready, got %v", ready)
	}

	s.Done("B")
	s.Done("D")
	ready = s.Ready()
	if len(ready) != 1 || ready[0] != "C" {
		t.Fatalf("expected C ready, got %v", ready)
	}
	s.Done("C")
	if !s.IsComplete() {
		t.Fatal("expected complete")
	}
}

func TestE2E_ConditionSkip(t *testing.T) {
	s := NewDAGScheduler([]string{"A", "cond", "B_true", "B_false", "end"},
		[][2]string{{"A", "cond"}, {"cond", "B_true"}, {"cond", "B_false"}, {"B_true", "end"}, {"B_false", "end"}})
	s.Done("A")

	ready := s.Ready()
	if len(ready) != 1 || ready[0] != "cond" {
		t.Fatalf("expected cond ready, got %v", ready)
	}

	s.SetConditionResult("cond", true)
	s.Done("cond")
	s.Skip("B_false")

	ready = s.Ready()
	if len(ready) != 1 || ready[0] != "B_true" {
		t.Fatalf("expected B_true ready, got %v", ready)
	}

	s.Done("B_true")
	ready = s.Ready()
	if len(ready) != 1 || ready[0] != "end" {
		t.Fatalf("expected end ready, got %v", ready)
	}

	s.Done("end")
	if !s.IsComplete() {
		t.Fatal("expected complete")
	}
}

func TestE2E_HumanApprovalOutput(t *testing.T) {
	store := newMockStore()
	logger := util.NewLogger("JSON", "DEBUG")
	exec := NewExecutor(store, nil, nil, nil, logger)

	task := &model.TaskRun{ID: "task-001", AgentFlowRunID: "run-001", NodeID: "human-1", Status: model.TaskPending}
	node := &model.Node{ID: "human-1", Type: model.HumanNode, Approval: &model.HumanApprovalConfig{
		Timeout: 2 * time.Hour, OnApprove: "continue", OnReject: "abort"}}

	if err := exec.executeHuman(context.Background(), task, node); err != nil {
		t.Fatalf("executeHuman: %v", err)
	}
	if task.Status != model.WaitingHuman {
		t.Errorf("expected WAITING_HUMAN, got %s", task.Status)
	}
	if task.Output["on_approve"] != "continue" {
		t.Errorf("expected on_approve=continue, got %v", task.Output["on_approve"])
	}
	if task.Output["on_reject"] != "abort" {
		t.Errorf("expected on_reject=abort, got %v", task.Output["on_reject"])
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
		matched := matchTriggerIDs(flows, tt.provider, tt.event)
		if len(matched) != tt.expectLen {
			t.Errorf("%s/%s: expected %d, got %d: %v", tt.provider, tt.event, tt.expectLen, len(matched), matched)
		}
	}
}

func matchTriggerIDs(flows []model.AgentFlowSpec, provider, eventType string) []string {
	var matched []string
	for _, wf := range flows {
		for _, t := range wf.Triggers {
			if t.Type != "webhook" { continue }
			if t.Provider != "" && t.Provider != provider { continue }
			if len(t.Events) > 0 && !contains(t.Events, eventType) { continue }
			matched = append(matched, wf.ID)
			break
		}
	}
	return matched
}

func contains(ss []string, s string) bool {
	for _, v := range ss { if v == s { return true } }
	return false
}
