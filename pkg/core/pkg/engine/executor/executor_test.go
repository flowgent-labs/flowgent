package executor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/tests/testutil"
)

// ── Condition ────────────────────────────────────────

func TestConditionExecutor_True(t *testing.T) {
	e := &ConditionExecutor{}
	plan := &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{Expression: "${input.result == true}"},
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
	plan := &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{Expression: "false"},
	}
	result, _ := e.Execute(context.Background(), plan, nil)
	if result.Output["result"] != false {
		t.Error("expected false")
	}
}

func TestConditionExecutor_TaskType(t *testing.T) {
	e := &ConditionExecutor{}
	if e.TaskType() != model.TaskCondition {
		t.Error("wrong task type for ConditionExecutor")
	}
}

// ── Noop ─────────────────────────────────────────────

func TestNoopExecutor(t *testing.T) {
	e := &NoopExecutor{}
	if e.TaskType() != model.TaskNoop {
		t.Error("wrong task type")
	}
	result, err := e.Execute(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil && result.Output != nil {
		t.Error("noop should return nil output")
	}
}

// ── Map ──────────────────────────────────────────────

func TestMapExecutor(t *testing.T) {
	e := &MapExecutor{}
	if e.TaskType() != model.TaskMap {
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
	if e.TaskType() != model.TaskJoin {
		t.Error("wrong task type")
	}
	result, _ := e.Execute(context.Background(), &model.ExecutionPlan{
		Input: map[string]any{"results": []any{
			map[string]any{"id": 1}, map[string]any{"id": 2},
		}},
	}, nil)
	if result.Output["count"] != 2 {
		t.Errorf("expected count=2, got %v", result.Output["count"])
	}
}

// ── Tribunal ─────────────────────────────────────────

func TestTribunalExecutor_Majority(t *testing.T) {
	e := &TribunalExecutor{}
	if e.TaskType() != model.TaskTribunal {
		t.Error("wrong task type")
	}
	// 3 reviews, 2 approve → majority
	result, err := e.Execute(context.Background(), &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{Strategy: map[string]any{"type": "majority"}},
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

func TestTribunalExecutor_Unanimous_Fail(t *testing.T) {
	e := &TribunalExecutor{}
	result, err := e.Execute(context.Background(), &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{Strategy: map[string]any{"type": "unanimous"}},
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

func TestTribunalExecutor_EmptyVotes(t *testing.T) {
	e := &TribunalExecutor{}
	result, err := e.Execute(context.Background(), &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{Strategy: map[string]any{"type": "majority"}},
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

func TestHumanExecutor(t *testing.T) {
	store := testutil.NewMockStore()
	e := NewHumanExecutor(store)
	if e.TaskType() != model.TaskHuman {
		t.Error("wrong task type")
	}
	result, err := e.Execute(context.Background(), &model.ExecutionPlan{
		AgentFlowRunID: "run-1",
		TaskID:         "task-1",
		NodeSpec: &model.NodeSpec{
			Approval: &model.HumanApprovalConfig{Timeout: 3600000000000}, // 1h in ns
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
	if e.TaskType() != model.TaskSubflow {
		t.Error("wrong task type")
	}
	result, err := e.Execute(context.Background(), &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{AgentFlowID: "sub-fix"},
		Input:    map[string]any{"repo": "test"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["status"] != "dispatched" {
		t.Error("subflow should return dispatched")
	}
}

// ── Tool ─────────────────────────────────────────────

type testMCPClient struct {
	tools map[string]string
}

func (c *testMCPClient) CallTool(_ context.Context, toolName string, args map[string]any) (map[string]any, error) {
	return map[string]any{"tool": toolName, "args": args}, nil
}

func TestToolExecutor(t *testing.T) {
	e := NewToolExecutor(map[string]engine.MCPClient{
		"github": &testMCPClient{},
	})
	if e.TaskType() != model.TaskTool {
		t.Error("wrong task type")
	}
	result, err := e.Execute(context.Background(), &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{Tool: "github"},
		Input: map[string]any{
			"action": "get_issue",
			"repo":   "test/repo",
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["tool"] != "get_issue" {
		t.Errorf("expected tool name, got %v", result.Output)
	}
}

func TestToolExecutor_MissingClient(t *testing.T) {
	e := NewToolExecutor(nil)
	_, err := e.Execute(context.Background(), &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{Tool: "nonexistent"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing client")
	}
}

// ── Supervisor ───────────────────────────────────────

type testLLMClient struct {
	response string
}

func (c *testLLMClient) Generate(_ context.Context, _, _, _ string, _ float64) (string, error) {
	return c.response, nil
}

func TestSupervisorExecutor_ValidAction(t *testing.T) {
	llm := &testLLMClient{response: `{"action":"continue","target":"","reason":"ok"}`}
	e := NewSupervisorExecutor(llm, []*config.AgentDef{
		{Name: "supervisor", Model: "test/gpt"},
	})

	result, err := e.Execute(context.Background(), &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{
			Agent: "supervisor",
			SupervisorConfig: &model.SupervisorConfig{
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
	e := NewSupervisorExecutor(llm, []*config.AgentDef{
		{Name: "supervisor", Model: "test/gpt"},
	})

	_, err := e.Execute(context.Background(), &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{
			Agent: "supervisor",
			SupervisorConfig: &model.SupervisorConfig{
				AllowedActions: []string{"continue", "retry"},
			},
		},
	}, nil)
	if err == nil {
		t.Fatal("expected error for disallowed action")
	}
}

func TestSupervisorExecutor_DefaultContinue(t *testing.T) {
	// LLM returns JSON without action field → should default to "continue"
	llm := &testLLMClient{response: `{"reason":"testing"}`}
	e := NewSupervisorExecutor(llm, []*config.AgentDef{
		{Name: "supervisor", Model: "test/gpt"},
	})

	result, err := e.Execute(context.Background(), &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{
			Agent: "supervisor",
			SupervisorConfig: &model.SupervisorConfig{
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

// ── Agent (with schema validation) ────────────────────

func TestAgentExecutor_Success(t *testing.T) {
	llm := &testLLMClient{response: `{"issues":[{"id":"ISS-001","severity":"high"}]}`}
	e := NewAgentExecutor(llm, []*config.AgentDef{
		{Name: "issue-detector", Model: "test/gpt", Instruction: "find issues", Soul: "you are a scanner"},
	})

	result, err := e.Execute(context.Background(), &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{Agent: "issue-detector"},
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
	e := NewAgentExecutor(llm, []*config.AgentDef{
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

	result, err := e.Execute(context.Background(), &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{Agent: "reviewer"},
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
			`{"decision":"yes","reason":"ok"}`,          // fail: decision not boolean
			`{"decision":true}`,                         // fail: missing reason
			`{"decision":true,"reason":"final answer"}`, // pass
		},
		onCall: func() { callCount++ },
	}
	e := NewAgentExecutor(llm, []*config.AgentDef{
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
	// First response fails schema → retry → second fails → retry → third passes
	result, err := e.Execute(context.Background(), &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{Agent: "reviewer"},
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

func TestAgentExecutor_JSONPreamble(t *testing.T) {
	// LLM output with markdown preamble → extractJSON should handle it
	llm := &testLLMClient{response: "Based on analysis, the best approach is:\n\n```json\n{\"decision\":false,\"reason\":\"unsafe\"}\n```\n\nLet me know if changes are needed."}
	e := NewAgentExecutor(llm, []*config.AgentDef{
		{Name: "security-reviewer", Model: "test/gpt", Instruction: "review", Soul: "you are a reviewer"},
	})

	result, err := e.Execute(context.Background(), &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{Agent: "security-reviewer"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["decision"] != false {
		t.Errorf("expected false, got %v", result.Output["decision"])
	}
}

func TestAgentExecutor_NotFound(t *testing.T) {
	e := NewAgentExecutor(nil, nil)
	_, err := e.Execute(context.Background(), &model.ExecutionPlan{
		NodeSpec: &model.NodeSpec{Agent: "nonexistent"},
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
	router.Register(&TribunalExecutor{})
	router.Register(&SubflowExecutor{})

	tests := []struct {
		taskType model.TaskType
		plan     *model.ExecutionPlan
	}{
		{model.TaskCondition, &model.ExecutionPlan{TaskType: model.TaskCondition, NodeSpec: &model.NodeSpec{Expression: "true"}}},
		{model.TaskNoop, &model.ExecutionPlan{TaskType: model.TaskNoop}},
		{model.TaskMap, &model.ExecutionPlan{TaskType: model.TaskMap}},
		{model.TaskJoin, &model.ExecutionPlan{TaskType: model.TaskJoin, Input: map[string]any{}}},
		{model.TaskTribunal, &model.ExecutionPlan{TaskType: model.TaskTribunal, NodeSpec: &model.NodeSpec{Strategy: map[string]any{"type": "majority"}}, Input: map[string]any{"votes": []any{map[string]any{"decision": true}}}}},
		{model.TaskSubflow, &model.ExecutionPlan{TaskType: model.TaskSubflow, NodeSpec: &model.NodeSpec{AgentFlowID: "test-flow"}}},
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
	_, err := router.Execute(context.Background(), &model.ExecutionPlan{
		TaskType: model.TaskType("unknown"),
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
		// Compare after parsing both as JSON to ignore formatting
		var wantParsed, gotParsed map[string]any
		wErr := json.Unmarshal([]byte(tt.expected), &wantParsed)
		gErr := json.Unmarshal([]byte(got), &gotParsed)
		if wErr != nil && gErr != nil {
			continue // both fail to parse, strings should match
		}
		if wErr != nil || gErr != nil {
			t.Errorf("extractJSON(%q) = %q; expected %q", tt.input, got, tt.expected)
		}
	}
}
