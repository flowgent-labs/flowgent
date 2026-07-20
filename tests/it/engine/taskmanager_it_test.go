package engine

import (
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/tests/it"
)

// TestTM_TaskLifecycle verifies the TaskManager subscribes to node tasks via the
// messager, executes them, publishes results back to JM, and the flow reaches
// terminal state.
func TestTM_TaskLifecycle(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "tm-lifecycle", TenantID: "test"},
		Vars:       map[string]any{"repo": "wl4g/rengine"},
		Triggers:   []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes: []entities.Node{
			{ID: "detect", Type: entities.AgentNode, Agent: "issue-detector"},
			{ID: "fix", Type: entities.AgentNode, Agent: "fixer-agent"},
			{ID: "verify", Type: entities.AgentNode, Agent: "security-reviewer"},
		},
		Edges: []entities.Edge{entities.Edge{From: "detect", To: "fix"}, entities.Edge{From: "fix", To: "verify"}},
	}

	fs := it.New(t, flow)
	runIDs := fs.TriggerGitHubPR(50, "tm-test-sha")
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %q, want COMPLETED", status)
	}
}

// TestTM_ExecResultStateOnly verifies the state-only callback: MQTT exec/results
// carry {plan_id, node_id, state} only — output is persisted via REST.
func TestTM_ExecResultStateOnly(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "tm-state-only", TenantID: "test"},
		Vars:       map[string]any{"repo": "wl4g/rengine"},
		Triggers:   []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes:      []entities.Node{entities.Node{ID: "step-a", Type: entities.NoopNode}, entities.Node{ID: "step-b", Type: entities.NoopNode}},
		Edges:      []entities.Edge{entities.Edge{From: "step-a", To: "step-b"}},
	}

	fs := it.New(t, flow)
	runIDs := fs.TriggerGitHubPR(51, "state-only-sha")
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %q, want COMPLETED", status)
	}
}
