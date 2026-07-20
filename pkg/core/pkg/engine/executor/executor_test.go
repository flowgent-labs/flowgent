package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/mcp"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

const testTenant = "test-tenant"

// ── Condition ────────────────────────────────────────

func TestConditionExecutor_True(t *testing.T) {
	e := &ConditionExecutor{}
	plan := &entities.ExecutionPlan{
		NodeSpec: &entities.NodeSpec{Expression: "${input.result == true}"},
		Input:    map[string]any{"result": true},
	}
	result, err := e.Execute(context.Background(), plan, map[string]map[string]any{"input": plan.Input})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["result"] != true {
		t.Errorf("expected true, got %v", result.Output["result"])
	}
}

func TestConditionExecutor_False(t *testing.T) {
	e := &ConditionExecutor{}
	plan := &entities.ExecutionPlan{
		NodeSpec: &entities.NodeSpec{Expression: "false"},
	}
	result, _ := e.Execute(context.Background(), plan, nil)
	if result.Output["result"] != false {
		t.Error("expected false")
	}
}

func TestConditionExecutor_TaskType(t *testing.T) {
	e := &ConditionExecutor{}
	if e.TaskType() != entities.TaskCondition {
		t.Error("wrong task type for ConditionExecutor")
	}
}

// ── Noop ─────────────────────────────────────────────

func TestNoopExecutor(t *testing.T) {
	e := &NoopExecutor{}
	if e.TaskType() != entities.TaskNoop {
		t.Error("wrong task type")
	}
	result, err := e.Execute(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Noop still reports a non-nil output ({"result": true}) so downstream
	// nodes/conditions can observe that this passthrough node ran — see
	// NoopExecutor.Execute.
	if got, ok := result.Output["result"]; !ok || got != true {
		t.Errorf("expected Output[\"result\"]=true, got %v", result.Output)
	}
}

// ── Map ──────────────────────────────────────────────

func TestMapExecutor(t *testing.T) {
	e := &MapExecutor{}
	if e.TaskType() != entities.TaskMap {
		t.Error("wrong task type")
	}
	result, _ := e.Execute(context.Background(), nil, nil)
	if result.Output["status"] != "dispatched" {
		t.Error("map should return dispatched")
	}
}

// ── Join ─────────────────────────────────────────────

func TestJoinExecutor(t *testing.T) {
	e := &JoinExecutor{}
	if e.TaskType() != entities.TaskJoin {
		t.Error("wrong task type")
	}
	result, _ := e.Execute(context.Background(), &entities.ExecutionPlan{
		Input: map[string]any{"results": []any{
			map[string]any{"id": 1}, map[string]any{"id": 2},
		}},
	}, nil)
	if result.Output["count"] != 2 {
		t.Errorf("expected count=2, got %v", result.Output["count"])
	}
}

// ── Committee ─────────────────────────────────────────

func TestCommitteeExecutor_Majority(t *testing.T) {
	e := &CommitteeExecutor{}
	if e.TaskType() != entities.TaskCommittee {
		t.Error("wrong task type")
	}
	result, err := e.Execute(context.Background(), &entities.ExecutionPlan{
		NodeSpec: &entities.NodeSpec{Strategy: map[string]any{"type": "majority"}},
		Input: map[string]any{"votes": []any{
			map[string]any{"decision": true},
			map[string]any{"decision": true},
			map[string]any{"decision": false},
		}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["decision"] != true {
		t.Errorf("majority yes expected, got %v", result.Output["decision"])
	}
}

func TestCommitteeExecutor_Unanimous_Fail(t *testing.T) {
	e := &CommitteeExecutor{}
	result, err := e.Execute(context.Background(), &entities.ExecutionPlan{
		NodeSpec: &entities.NodeSpec{Strategy: map[string]any{"type": "unanimous"}},
		Input: map[string]any{"votes": []any{
			map[string]any{"decision": true},
			map[string]any{"decision": true},
			map[string]any{"decision": false},
		}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["decision"] != false {
		t.Error("unanimous should fail with one false")
	}
}

func TestCommitteeExecutor_EmptyVotes(t *testing.T) {
	e := &CommitteeExecutor{}
	result, err := e.Execute(context.Background(), &entities.ExecutionPlan{
		NodeSpec: &entities.NodeSpec{Strategy: map[string]any{"type": "majority"}},
		Input:    map[string]any{},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["decision"] != false {
		t.Error("empty votes should reject")
	}
}

// ── Human ────────────────────────────────────────────

type mockHumanStore struct{}

func (m *mockHumanStore) CreateApproval(_ context.Context, a *entities.ApprovalInfo) error {
	a.Token = "test-token"
	return nil
}

func TestHumanExecutor(t *testing.T) {
	store := &mockHumanStore{}
	e := NewHumanExecutor(store)
	if e.TaskType() != entities.TaskHuman {
		t.Error("wrong task type")
	}
	result, err := e.Execute(context.Background(), &entities.ExecutionPlan{
		AgentFlowRunID: "run-1",
		TaskID:         "task-1",
		NodeSpec: &entities.NodeSpec{
			Approval: &entities.HumanApprovalConfig{},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["approval_token"] == "" {
		t.Errorf("expected non-empty token, got %v", result.Output)
	}
	if result.Output["status"] != "WAITING_HUMAN" {
		t.Errorf("expected WAITING_HUMAN, got %v", result.Output["status"])
	}
}

// ── Subflow ──────────────────────────────────────────

func TestSubflowExecutor(t *testing.T) {
	e := &SubflowExecutor{}
	if e.TaskType() != entities.TaskSubflow {
		t.Error("wrong task type")
	}
	result, err := e.Execute(context.Background(), &entities.ExecutionPlan{
		NodeSpec: &entities.NodeSpec{AgentFlowID: "sub-fix"},
		Input:    map[string]any{"repo": "test"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["status"] != "dispatched" {
		t.Error("subflow should return dispatched")
	}
}

// ── Test helpers ─────────────────────────────────────

type testLLMClient struct {
	response string
}

func (c *testLLMClient) Generate(_ context.Context, _, _, _ string, _ float64) (string, error) {
	return c.response, nil
}

type retryLLM struct {
	responses []string
	onCall    func()
	idx       int
}

func (c *retryLLM) Generate(_ context.Context, _, _, _ string, _ float64) (string, error) {
	if c.onCall != nil {
		c.onCall()
	}
	resp := c.responses[c.idx]
	c.idx++
	return resp, nil
}

// agentServer creates an httptest server that serves agent definitions.
func agentServer(agents []*entities.AgentInfo) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, a := range agents {
			if r.URL.Path == "/api/v1/"+testTenant+"/agents/"+a.Name {
				json.NewEncoder(w).Encode(a)
				return
			}
		}
		w.WriteHeader(404)
	}))
}

// ── Tool ─────────────────────────────────────────────

func TestToolExecutor_TaskType(t *testing.T) {
	e := NewToolExecutor(mcp.NewMcpManager(client.NewGenericHttpClient(0)), client.NewGenericHttpClient(0))
	if e.TaskType() != entities.TaskTool {
		t.Error("wrong task type")
	}
}

func TestToolExecutor_MissingTool(t *testing.T) {
	e := NewToolExecutor(mcp.NewMcpManager(client.NewGenericHttpClient(0)), client.NewGenericHttpClient(0))
	_, err := e.Execute(context.Background(), &entities.ExecutionPlan{
		NodeSpec: &entities.NodeSpec{Tool: "nonexistent"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing tool")
	}
}

// ── Supervisor ───────────────────────────────────────

func TestSupervisorExecutor_ValidAction(t *testing.T) {
	llm := &testLLMClient{response: `{"action":"continue","target":"","reason":"ok"}`}
	srv := agentServer([]*entities.AgentInfo{
		{Name: "supervisor", Model: "test/gpt"},
	})
	defer srv.Close()
	apiClient := client.NewFlowgentClient(srv.URL)

	e := NewSupervisorExecutor(llm, apiClient, testTenant)

	result, err := e.Execute(context.Background(), &entities.ExecutionPlan{
		NodeSpec: &entities.NodeSpec{
			Agent: "supervisor",
			SupervisorConfig: &entities.SupervisorConfig{
				AllowedActions: []string{"continue", "retry", "abort"},
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["action"] != "continue" {
		t.Errorf("expected continue, got %v", result.Output["action"])
	}
}

func TestSupervisorExecutor_DisallowedAction(t *testing.T) {
	llm := &testLLMClient{response: `{"action":"redirect","target":"x","reason":"test"}`}
	srv := agentServer([]*entities.AgentInfo{
		{Name: "supervisor", Model: "test/gpt"},
	})
	defer srv.Close()
	apiClient := client.NewFlowgentClient(srv.URL)

	e := NewSupervisorExecutor(llm, apiClient, testTenant)

	_, err := e.Execute(context.Background(), &entities.ExecutionPlan{
		NodeSpec: &entities.NodeSpec{
			Agent: "supervisor",
			SupervisorConfig: &entities.SupervisorConfig{
				AllowedActions: []string{"continue", "retry"},
			},
		},
	}, nil)
	if err == nil {
		t.Fatal("expected error for disallowed action")
	}
}

func TestSupervisorExecutor_DefaultContinue(t *testing.T) {
	llm := &testLLMClient{response: `{"reason":"testing"}`}
	srv := agentServer([]*entities.AgentInfo{
		{Name: "supervisor", Model: "test/gpt"},
	})
	defer srv.Close()
	apiClient := client.NewFlowgentClient(srv.URL)

	e := NewSupervisorExecutor(llm, apiClient, testTenant)

	result, err := e.Execute(context.Background(), &entities.ExecutionPlan{
		NodeSpec: &entities.NodeSpec{
			Agent: "supervisor",
			SupervisorConfig: &entities.SupervisorConfig{
				AllowedActions: []string{"continue", "retry"},
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["action"] != "continue" {
		t.Errorf("expected default continue, got %v", result.Output["action"])
	}
}

// ── Agent ────────────────────────────────────────────

func TestAgentExecutor_Success(t *testing.T) {
	llm := &testLLMClient{response: `{"issues":[{"id":"ISS-001","severity":"high"}]}`}
	srv := agentServer([]*entities.AgentInfo{
		{Name: "issue-detector", Model: "test/gpt", Instruction: "find issues", Soul: "you are a scanner"},
	})
	defer srv.Close()
	apiClient := client.NewFlowgentClient(srv.URL)

	e := NewAgentExecutor(llm, apiClient, testTenant)

	result, err := e.Execute(context.Background(), &entities.ExecutionPlan{
		NodeSpec: &entities.NodeSpec{Agent: "issue-detector"},
		Input:    map[string]any{"repo": "test"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output == nil {
		t.Fatal("expected output")
	}
}

func TestAgentExecutor_SchemaValidation_Pass(t *testing.T) {
	llm := &testLLMClient{response: `{"decision":true,"reason":"ok"}`}
	srv := agentServer([]*entities.AgentInfo{
		{Name: "reviewer", Model: "test/gpt", Instruction: "review", Soul: "you are a reviewer",
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"decision": map[string]any{"type": "boolean"},
					"reason":   map[string]any{"type": "string"},
				},
				"required": []any{"decision", "reason"},
			},
		},
	})
	defer srv.Close()
	apiClient := client.NewFlowgentClient(srv.URL)

	e := NewAgentExecutor(llm, apiClient, testTenant)

	result, err := e.Execute(context.Background(), &entities.ExecutionPlan{
		NodeSpec: &entities.NodeSpec{Agent: "reviewer"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output == nil {
		t.Fatal("expected output")
	}
}

func TestAgentExecutor_SchemaValidation_Retry(t *testing.T) {
	callCount := 0
	llm := &retryLLM{
		responses: []string{
			`{"decision":"yes","reason":"ok"}`,
			`{"decision":true}`,
			`{"decision":true,"reason":"final answer"}`,
		},
		onCall: func() { callCount++ },
	}
	srv := agentServer([]*entities.AgentInfo{
		{Name: "reviewer", Model: "test/gpt", Instruction: "review", Soul: "you are a reviewer",
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"decision": map[string]any{"type": "boolean"},
					"reason":   map[string]any{"type": "string"},
				},
				"required": []any{"decision", "reason"},
			},
		},
	})
	defer srv.Close()
	apiClient := client.NewFlowgentClient(srv.URL)

	e := NewAgentExecutor(llm, apiClient, testTenant)

	result, err := e.Execute(context.Background(), &entities.ExecutionPlan{
		NodeSpec: &entities.NodeSpec{Agent: "reviewer"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if callCount < 3 {
		t.Errorf("expected 3 retries, got %d", callCount)
	}
	if result.Output["decision"] != true {
		t.Errorf("expected true, got %v", result.Output["decision"])
	}
}

func TestAgentExecutor_JSONPreamble(t *testing.T) {
	llm := &testLLMClient{response: "Based on analysis, the best approach is:\n\n```json\n{\"decision\":false,\"reason\":\"unsafe\"}\n```\n\nLet me know if changes are needed."}
	srv := agentServer([]*entities.AgentInfo{
		{Name: "security-reviewer", Model: "test/gpt", Instruction: "review", Soul: "you are a reviewer"},
	})
	defer srv.Close()
	apiClient := client.NewFlowgentClient(srv.URL)

	e := NewAgentExecutor(llm, apiClient, testTenant)

	result, err := e.Execute(context.Background(), &entities.ExecutionPlan{
		NodeSpec: &entities.NodeSpec{Agent: "security-reviewer"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["decision"] != false {
		t.Errorf("expected false, got %v", result.Output["decision"])
	}
}

func TestAgentExecutor_NotFound(t *testing.T) {
	srv := agentServer(nil)
	defer srv.Close()
	apiClient := client.NewFlowgentClient(srv.URL)

	e := NewAgentExecutor(nil, apiClient, testTenant)
	_, err := e.Execute(context.Background(), &entities.ExecutionPlan{
		NodeSpec: &entities.NodeSpec{Agent: "nonexistent"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing agent")
	}
}

// ── Router ───────────────────────────────────────────

func TestTaskExecutorRouter(t *testing.T) {
	router := NewTaskExecutorRouter()
	router.Register(&ConditionExecutor{})
	router.Register(&NoopExecutor{})
	router.Register(&MapExecutor{})
	router.Register(&JoinExecutor{})
	router.Register(&CommitteeExecutor{})
	router.Register(&SubflowExecutor{})

	tests := []struct {
		taskType entities.TaskType
		plan     *entities.ExecutionPlan
	}{
		{entities.TaskCondition, &entities.ExecutionPlan{TaskType: entities.TaskCondition, NodeSpec: &entities.NodeSpec{Expression: "true"}}},
		{entities.TaskNoop, &entities.ExecutionPlan{TaskType: entities.TaskNoop}},
		{entities.TaskMap, &entities.ExecutionPlan{TaskType: entities.TaskMap}},
		{entities.TaskJoin, &entities.ExecutionPlan{TaskType: entities.TaskJoin, Input: map[string]any{}}},
		{entities.TaskCommittee, &entities.ExecutionPlan{TaskType: entities.TaskCommittee, NodeSpec: &entities.NodeSpec{Strategy: map[string]any{"type": "majority"}}, Input: map[string]any{"votes": []any{map[string]any{"decision": true}}}}},
		{entities.TaskSubflow, &entities.ExecutionPlan{TaskType: entities.TaskSubflow, NodeSpec: &entities.NodeSpec{AgentFlowID: "test-flow"}}},
	}

	for _, tt := range tests {
		t.Run(string(tt.taskType), func(t *testing.T) {
			_, err := router.Execute(context.Background(), tt.plan, nil)
			if err != nil {
				t.Errorf("router.Execute for %s: %v", tt.taskType, err)
			}
		})
	}
}

func TestTaskExecutorRouter_UnknownType(t *testing.T) {
	router := NewTaskExecutorRouter()
	_, err := router.Execute(context.Background(), &entities.ExecutionPlan{
		TaskType: entities.TaskType("unknown"),
	}, nil)
	if err == nil {
		t.Fatal("expected error for unknown task type")
	}
}

// ── JSON Extract ─────────────────────────────────────

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{"key":"value"}`, `{"key":"value"}`},
		{"Some text before\n```json\n{\"a\":1}\n```\nAfter", `{"a":1}`},
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{"No JSON here", "No JSON here"},
		{"prefix text {\"b\":2} suffix", `{"b":2}`},
	}
	for _, tt := range tests {
		got := extractJSON(tt.input)
		var wantParsed, gotParsed map[string]any
		wErr := json.Unmarshal([]byte(tt.expected), &wantParsed)
		gErr := json.Unmarshal([]byte(got), &gotParsed)
		if wErr != nil && gErr != nil {
			continue
		}
		if wErr != nil || gErr != nil {
			t.Errorf("extractJSON(%q) = %q; expected %q", tt.input, got, tt.expected)
		}
	}
}
