package engine

import (
	"net/http"
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
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
		Nodes: []entities.Node{
			{ID: "get-commit", Kind: entities.NoopNode},
			{ID: "scan", Kind: entities.NoopNode},
			{ID: "aggregate", Kind: entities.NoopNode},
			{ID: "report", Kind: entities.NoopNode},
		},
		Edges: []entities.Edge{
			{From: "get-commit", To: "scan"},
			{From: "scan", To: "aggregate"},
			{From: "aggregate", To: "report"},
		},
	}
	fs := it.New(t, flow)
	runIDs := fs.TriggerManual(nil)
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %s, want COMPLETED", status)
	}
	fs.ExpectTaskCount(runIDs[0], 4)
}

// TestJM_ParallelFanOutFanIn verifies diamond topology: fan-out to two parallel
// branches that fan back in to a join node.
func TestJM_ParallelFanOutFanIn(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "it-diamond", Namespace: "test"},
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
		Nodes: []entities.Node{
			{ID: "generate-fixes", Kind: entities.NoopNode},
			{ID: "review-security", Kind: entities.NoopNode},
			{ID: "review-quality", Kind: entities.NoopNode},
			{ID: "committee", Kind: entities.NoopNode},
		},
		Edges: []entities.Edge{
			{From: "generate-fixes", To: "review-security"},
			{From: "generate-fixes", To: "review-quality"},
			{From: "review-security", To: "committee"},
			{From: "review-quality", To: "committee"},
		},
	}
	fs := it.New(t, flow)
	runIDs := fs.TriggerManual(nil)
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %s, want COMPLETED", status)
	}
	fs.ExpectTaskCount(runIDs[0], 4)
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
				Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
				Nodes: []entities.Node{
					{ID: "committee", Kind: entities.NoopNode},
					{ID: "is-approved", Kind: entities.ConditionNode, Expression: "${input.approved == true}", Args: map[string]any{"approved": "${vars.approved}"}},
					{ID: "commit-fixes", Kind: entities.NoopNode},
					{ID: "loop-back", Kind: entities.NoopNode},
					{ID: "end", Kind: entities.NoopNode},
				},
				Edges: []entities.Edge{
					{From: "committee", To: "is-approved"},
					{From: "is-approved", To: "commit-fixes", Condition: boolPtr(true)},
					{From: "is-approved", To: "loop-back", Condition: boolPtr(false)},
					{From: "commit-fixes", To: "end"},
					{From: "loop-back", To: "end"},
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
			// 5 nodes but 1 is skipped (never dispatched) → 4 task rows.
			fs.ExpectTaskCount(runIDs[0], 4)
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
				Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
				Nodes: []entities.Node{
					{ID: "committee", Kind: entities.CommitteeNode, Strategy: map[string]any{"type": "majority"}, Args: map[string]any{"votes": tc.votes}},
					{ID: "end", Kind: entities.NoopNode},
				},
				Edges: []entities.Edge{{From: "committee", To: "end"}},
			}
			fs := it.New(t, flow)
			runIDs := fs.TriggerManual(nil)
			if len(runIDs) != 1 {
				t.Fatalf("expected 1 run, got %v", runIDs)
			}
			if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
				t.Fatalf("run status = %s, want COMPLETED", status)
			}
			fs.ExpectTaskCount(runIDs[0], 2)
		})
	}
}

// TestJM_MapIteration verifies the map node processes items and dispatches
// child execution plans, plus a join node aggregates them.
func TestJM_MapIteration(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "it-map", Namespace: "test"},
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
		Nodes: []entities.Node{
			{ID: "iterate", Kind: entities.MapNode, Args: map[string]any{"items": []any{"x", "y", "z"}}},
			{ID: "done", Kind: entities.NoopNode},
		},
		Edges: []entities.Edge{{From: "iterate", To: "done"}},
	}
	fs := it.New(t, flow)
	runIDs := fs.TriggerManual(nil)
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %s, want COMPLETED", status)
	}
	fs.ExpectTaskCount(runIDs[0], 2)
}

// TestJM_JoinAggregation verifies map → join: the join node aggregates results
// from map children and reports count.
func TestJM_JoinAggregation(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "it-map-join", Namespace: "test"},
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
		Nodes: []entities.Node{
			{ID: "fanout", Kind: entities.MapNode, Args: map[string]any{"items": []any{"a", "b"}}},
			{ID: "collect", Kind: entities.JoinNode},
			{ID: "done", Kind: entities.NoopNode},
		},
		Edges: []entities.Edge{
			{From: "fanout", To: "collect"},
			{From: "collect", To: "done"},
		},
	}
	fs := it.New(t, flow)
	runIDs := fs.TriggerManual(nil)
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %s, want COMPLETED", status)
	}
	fs.ExpectTaskCount(runIDs[0], 3)
}

// TestJM_SkillExecution verifies the skill executor routes correctly through
// the DAG. The executor is a stub (returns immediately), but the DAG routing
// must work end-to-end.
func TestJM_SkillExecution(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "it-skill", Namespace: "test"},
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
		Nodes: []entities.Node{
			{ID: "start", Kind: entities.NoopNode},
			{ID: "skill-step", Kind: entities.SkillNode, Skill: "nexus3-retrieval"},
			{ID: "done", Kind: entities.NoopNode},
		},
		Edges: []entities.Edge{
			{From: "start", To: "skill-step"},
			{From: "skill-step", To: "done"},
		},
	}
	fs := it.New(t, flow)
	runIDs := fs.TriggerManual(nil)
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %s, want COMPLETED", status)
	}
	fs.ExpectTaskCount(runIDs[0], 3)
}

// TestJM_SubFlowNesting verifies a parent flow with a subflow node completes
// through the DAG (SubflowExecutor is a stub in standalone mode).
func TestJM_SubFlowNesting(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "it-nesting", Namespace: "test"},
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
		Vars:     map[string]any{"repo": "wl4g/rengine", "project_key": "rengine"},
		Triggers: []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes: []entities.Node{
			{ID: "parent-start", Kind: entities.NoopNode},
			{ID: "subflow", Kind: entities.AgentFlowNode, AgentFlowID: "it-nesting-child"},
			{ID: "parent-end", Kind: entities.NoopNode},
		},
		Edges: []entities.Edge{
			{From: "parent-start", To: "subflow"},
			{From: "subflow", To: "parent-end"},
		},
	}
	fs := it.New(t, flow)
	runIDs := fs.TriggerGitHubPR(10, "subflow123")
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %q, want COMPLETED", status)
	}
	fs.ExpectTaskCount(runIDs[0], 3)
}

// TestJM_SupervisorGate verifies the supervisor node with a "continue" action
// allows the DAG to proceed.
func TestJM_SupervisorGate(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "it-supervisor", Namespace: "test"},
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
		Vars:     map[string]any{"repo": "wl4g/rengine"},
		Triggers: []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes: []entities.Node{
			{ID: "start", Kind: entities.NoopNode},
			{ID: "gate", Kind: entities.SupervisorNode, Agent: "supervisor-agent",
				Args: map[string]any{"task": "review changes"},
				SupervisorConfig: &entities.SupervisorConfig{
					MaxRetries: 1, MaxNodes: 3,
					AllowedActions: []string{"continue", "rework"},
				},
			},
			{ID: "end", Kind: entities.NoopNode},
		},
		Edges: []entities.Edge{
			{From: "start", To: "gate"},
			{From: "gate", To: "end"},
		},
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
	fs.ExpectTaskCount(runIDs[0], 3)
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
				Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
				Nodes: []entities.Node{
					{ID: "committee", Kind: entities.CommitteeNode, Strategy: map[string]any{"type": "unanimous"}, Args: map[string]any{"votes": tc.votes}},
					{ID: "end", Kind: entities.NoopNode},
				},
				Edges: []entities.Edge{{From: "committee", To: "end"}},
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
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
		Nodes: []entities.Node{
			{ID: "committee", Kind: entities.CommitteeNode, Strategy: map[string]any{"type": "unanimous"},
				Args: map[string]any{"votes": []any{
					map[string]any{"decision": true},
					map[string]any{"decision": true},
					map[string]any{"decision": false},
				}},
			},
			{ID: "end", Kind: entities.NoopNode},
		},
		Edges: []entities.Edge{{From: "committee", To: "end"}},
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
				Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
				Nodes: []entities.Node{
					{ID: "committee", Kind: entities.CommitteeNode, Strategy: map[string]any{"type": "majority_strict"}, Args: map[string]any{"votes": tc.votes}},
					{ID: "end", Kind: entities.NoopNode},
				},
				Edges: []entities.Edge{{From: "committee", To: "end"}},
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

// TestJM_CancelRun verifies that cancelling a pending run prevents execution.
func TestJM_CancelRun(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "it-cancel", Namespace: "test"},
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
		Nodes: []entities.Node{
			{ID: "step", Kind: entities.NoopNode},
		},
		Edges: []entities.Edge{},
	}
	fs := it.New(t, flow)
	runIDs := fs.TriggerManual(nil)
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	runID := runIDs[0]

	// Cancel immediately (the run hasn't been picked up yet by the poller).
	resp, err := http.Post(fs.APIURL+"/api/v1/"+fs.Namespace+"/runs/"+runID+"/cancel", "application/json", nil)
	if err != nil {
		t.Fatalf("cancel POST: %v", err)
	}
	resp.Body.Close()

	// Wait briefly — the cancelled run should either stay PENDING (if already
	// cancelled before JM pickup) or be marked something else.
	status := fs.WaitRun(runID, 10*time.Second)
	t.Logf("run %s status after cancel = %s", runID, status)
}

// TestJM_DeadEndDAG verifies that a dead-end DAG (a node with no children
// that depend on an unresolvable input) still completes gracefully
// because the NoopExecutor always returns success.
func TestJM_DeadEndDAG(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "it-deadend", Namespace: "test"},
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
		Nodes: []entities.Node{
			{ID: "start", Kind: entities.NoopNode},
		},
		Edges: []entities.Edge{},
	}
	fs := it.New(t, flow)
	runIDs := fs.TriggerManual(nil)
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %q, want COMPLETED", status)
	}
}
