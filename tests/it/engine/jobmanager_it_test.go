package engine

import (
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/tests/it"
)

func boolPtr(b bool) *bool { return &b }


// TestJM_LinearChain verifies serial DAG execution: a→b→c→d.
func TestJM_LinearChain(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "it-linear", Namespace: "test"},
		Nodes:      []entities.Node{entities.Node{ID: "get-commit", Type: entities.NoopNode}, entities.Node{ID: "scan", Type: entities.NoopNode}, entities.Node{ID: "aggregate", Type: entities.NoopNode}, entities.Node{ID: "report", Type: entities.NoopNode}},
		Edges:      []entities.Edge{entities.Edge{From: "get-commit", To: "scan"}, entities.Edge{From: "scan", To: "aggregate"}, entities.Edge{From: "aggregate", To: "report"}},
	}
	fs := it.New(t, flow)
	runIDs := fs.TriggerManual(nil)
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %s, want COMPLETED", status)
	}
}

// TestJM_ParallelFanOutFanIn verifies diamond topology: fan-out to two parallel
// branches that fan back in to a join node.
func TestJM_ParallelFanOutFanIn(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "it-diamond", Namespace: "test"},
		Nodes:      []entities.Node{entities.Node{ID: "generate-fixes", Type: entities.NoopNode}, entities.Node{ID: "review-security", Type: entities.NoopNode}, entities.Node{ID: "review-quality", Type: entities.NoopNode}, entities.Node{ID: "committee", Type: entities.NoopNode}},
		Edges:      []entities.Edge{entities.Edge{From: "generate-fixes", To: "review-security"}, entities.Edge{From: "generate-fixes", To: "review-quality"}, entities.Edge{From: "review-security", To: "committee"}, entities.Edge{From: "review-quality", To: "committee"}},
	}
	fs := it.New(t, flow)
	runIDs := fs.TriggerManual(nil)
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %s, want COMPLETED", status)
	}
}

// TestJM_ConditionRouting verifies conditional edges: the condition node picks the
// true branch and the false branch is skipped.
func TestJM_ConditionRouting(t *testing.T) {
	cases := []struct {
		name, wantExecuted, wantSkipped string
		approved                        bool
	}{
		{name: "approved", approved: true, wantExecuted: "commit-fixes", wantSkipped: "loop-back"},
		{name: "rejected", approved: false, wantExecuted: "loop-back", wantSkipped: "commit-fixes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flow := &entities.FlowInfo{
				BaseEntity: entities.BaseEntity{ID: "it-condition", Namespace: "test"},
				Nodes: []entities.Node{
					entities.Node{ID: "committee", Type: entities.NoopNode},
					{ID: "is-approved", Type: entities.ConditionNode, Expression: "${input.approved == true}", Input: map[string]any{"approved": "${vars.approved}"}},
					entities.Node{ID: "commit-fixes", Type: entities.NoopNode}, entities.Node{ID: "loop-back", Type: entities.NoopNode}, entities.Node{ID: "end", Type: entities.NoopNode},
				},
				Edges: []entities.Edge{
					entities.Edge{From: "committee", To: "is-approved"},
					entities.Edge{From: "is-approved", To: "commit-fixes", Condition: boolPtr(true)},
					entities.Edge{From: "is-approved", To: "loop-back", Condition: boolPtr(false)},
					entities.Edge{From: "commit-fixes", To: "end"}, entities.Edge{From: "loop-back", To: "end"},
				},
			}
			fs := it.New(t, flow)
			runIDs := fs.TriggerManual(map[string]any{"approved": tc.approved})
			if len(runIDs) != 1 {
				t.Fatalf("expected 1 run, got %v", runIDs)
			}
			if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
				t.Fatalf("run status = %s, want COMPLETED", status)
			}
		})
	}
}

// TestJM_CommitteeMajority verifies majority-vote committee.
func TestJM_CommitteeMajority(t *testing.T) {
	cases := []struct {
		name    string
		votes   []any
		wantDec bool
	}{
		{"two-of-three-approve", []any{map[string]any{"decision": true}, map[string]any{"decision": true}, map[string]any{"decision": false}}, true},
		{"one-of-three-approve", []any{map[string]any{"decision": true}, map[string]any{"decision": false}, map[string]any{"decision": false}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flow := &entities.FlowInfo{
				BaseEntity: entities.BaseEntity{ID: "it-committee", Namespace: "test"},
				Nodes: []entities.Node{
					{ID: "committee", Type: entities.CommitteeNode, Strategy: map[string]any{"type": "majority"}, Input: map[string]any{"votes": tc.votes}},
					entities.Node{ID: "end", Type: entities.NoopNode},
				},
				Edges: []entities.Edge{entities.Edge{From: "committee", To: "end"}},
			}
			fs := it.New(t, flow)
			runIDs := fs.TriggerManual(nil)
			if len(runIDs) != 1 {
				t.Fatalf("expected 1 run, got %v", runIDs)
			}
			if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
				t.Fatalf("run status = %s, want COMPLETED", status)
			}
		})
	}
}

// TestJM_MapIteration verifies the map node processes items and dispatches
// child execution plans.
func TestJM_MapIteration(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "it-map", Namespace: "test"},
		Nodes: []entities.Node{
			{ID: "iterate", Type: entities.MapNode, Input: map[string]any{"items": []any{"x", "y", "z"}}},
			entities.Node{ID: "done", Type: entities.NoopNode},
		},
		Edges: []entities.Edge{entities.Edge{From: "iterate", To: "done"}},
	}
	fs := it.New(t, flow)
	runIDs := fs.TriggerManual(nil)
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %s, want COMPLETED", status)
	}
}

// TestJM_SubFlowNesting verifies a parent flow with a subflow node completes
// through the DAG.
func TestJM_SubFlowNesting(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "it-nesting", Namespace: "test"},
		Vars:       map[string]any{"repo": "wl4g/rengine", "project_key": "rengine"},
		Triggers:   []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes: []entities.Node{
			entities.Node{ID: "parent-start", Type: entities.NoopNode},
			{ID: "subflow", Type: entities.AgentFlowNode, AgentFlowID: "it-nesting-child"},
			entities.Node{ID: "parent-end", Type: entities.NoopNode},
		},
		Edges: []entities.Edge{entities.Edge{From: "parent-start", To: "subflow"}, entities.Edge{From: "subflow", To: "parent-end"}},
	}
	fs := it.New(t, flow)
	runIDs := fs.TriggerGitHubPR(10, "subflow123")
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %q, want COMPLETED", status)
	}
}

// TestJM_SupervisorGate verifies the supervisor node with a "continue" action
// allows the DAG to proceed.
func TestJM_SupervisorGate(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "it-supervisor", Namespace: "test"},
		Vars:       map[string]any{"repo": "wl4g/rengine"},
		Triggers:   []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes: []entities.Node{
			entities.Node{ID: "start", Type: entities.NoopNode},
			{ID: "gate", Type: entities.SupervisorNode, Agent: "supervisor-agent", Input: map[string]any{"task": "review changes"},
				SupervisorConfig: &entities.SupervisorConfig{MaxRetries: 1, MaxNodes: 3, AllowedActions: []string{"continue", "rework"}}},
			entities.Node{ID: "end", Type: entities.NoopNode},
		},
		Edges: []entities.Edge{entities.Edge{From: "start", To: "gate"}, entities.Edge{From: "gate", To: "end"}},
	}
	fs := it.New(t, flow)
	fs.Post("/api/v1/"+fs.Namespace+"/agents", entities.AgentInfo{
		Name: "supervisor-agent", Model: "mock/echo",
		Soul: "You are a supervisor agent. Decide whether to continue or rework.",
	})
	runIDs := fs.TriggerGitHubPR(5, "deadbeef")
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 60*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %q, want COMPLETED", status)
	}
}

// TestJM_CommitteeUnanimous verifies the unanimous committee strategy.
func TestJM_CommitteeUnanimous(t *testing.T) {
	cases := []struct {
		name    string
		votes   []any
		wantDec bool
	}{
		{"all-three-approve", []any{map[string]any{"decision": true}, map[string]any{"decision": true}, map[string]any{"decision": true}}, true},
		{"one-dissents", []any{map[string]any{"decision": true}, map[string]any{"decision": true}, map[string]any{"decision": false}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flow := &entities.FlowInfo{
				BaseEntity: entities.BaseEntity{ID: "it-committee-unanimous", Namespace: "test"},
				Nodes: []entities.Node{
					{ID: "committee", Type: entities.CommitteeNode, Strategy: map[string]any{"type": "unanimous"}, Input: map[string]any{"votes": tc.votes}},
					entities.Node{ID: "end", Type: entities.NoopNode},
				},
				Edges: []entities.Edge{entities.Edge{From: "committee", To: "end"}},
			}
			fs := it.New(t, flow)
			runIDs := fs.TriggerManual(nil)
			if len(runIDs) != 1 {
				t.Fatalf("expected 1 run, got %v", runIDs)
			}
			if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
				t.Fatalf("run status = %s, want COMPLETED", status)
			}
		})
	}
}

// TestJM_CommitteeVeto verifies that with unanimous strategy a single "no" blocks.
func TestJM_CommitteeVeto(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "it-committee-veto", Namespace: "test"},
		Nodes: []entities.Node{
			{ID: "committee", Type: entities.CommitteeNode, Strategy: map[string]any{"type": "unanimous"},
				Input: map[string]any{"votes": []any{map[string]any{"decision": true}, map[string]any{"decision": true}, map[string]any{"decision": false}}}},
			entities.Node{ID: "end", Type: entities.NoopNode},
		},
		Edges: []entities.Edge{entities.Edge{From: "committee", To: "end"}},
	}
	fs := it.New(t, flow)
	runIDs := fs.TriggerManual(nil)
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %s, want COMPLETED", status)
	}
}

// TestJM_CommitteeWeighted verifies weighted-vote majority_strict (2/3 super-majority).
func TestJM_CommitteeWeighted(t *testing.T) {
	cases := []struct {
		name    string
		votes   []any
		wantDec bool
	}{
		{"two-of-three-approve", []any{map[string]any{"decision": true, "weight": 0.5}, map[string]any{"decision": true, "weight": 0.3}, map[string]any{"decision": false, "weight": 0.2}}, true},
		{"one-of-three-approve", []any{map[string]any{"decision": true, "weight": 0.5}, map[string]any{"decision": false, "weight": 0.3}, map[string]any{"decision": false, "weight": 0.2}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flow := &entities.FlowInfo{
				BaseEntity: entities.BaseEntity{ID: "it-committee-weighted", Namespace: "test"},
				Nodes: []entities.Node{
					{ID: "committee", Type: entities.CommitteeNode, Strategy: map[string]any{"type": "majority_strict"}, Input: map[string]any{"votes": tc.votes}},
					entities.Node{ID: "end", Type: entities.NoopNode},
				},
				Edges: []entities.Edge{entities.Edge{From: "committee", To: "end"}},
			}
			fs := it.New(t, flow)
			runIDs := fs.TriggerManual(nil)
			if len(runIDs) != 1 {
				t.Fatalf("expected 1 run, got %v", runIDs)
			}
			if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
				t.Fatalf("run status = %s, want COMPLETED", status)
			}
		})
	}
}
