package sandbox

import (
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/tests/it"
)

func TestSandbox_ExecutorWiring(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "sb-wiring", Namespace: "test"},
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
		Vars:     map[string]any{"repo": "wl4g/rengine"},
		Triggers: []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes: []entities.Node{{
			ID: "shell-step", Kind: entities.SandboxNode,
			Skill: "shell", Script: "echo 'sandbox ok' && exit 0", Timeout: "10s",
		}},
		Edges: []entities.Edge{},
	}

	fs := it.New(t, flow)
	runIDs := fs.TriggerGitHubPR(60, "sb-shell-sha")
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	status := fs.WaitRun(runIDs[0], 30*time.Second)
	if status != string(entities.RunCompleted) {
		t.Fatalf("run status = %q, expected COMPLETED", status)
	}
	fs.ExpectTaskCount(runIDs[0], 1)
}

func TestSandbox_NoopFallback(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "sb-noop", Namespace: "test"},
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
		Vars:     map[string]any{"repo": "wl4g/rengine"},
		Triggers: []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes:    []entities.Node{{ID: "pre-sandbox", Kind: entities.NoopNode}, {ID: "post-sandbox", Kind: entities.NoopNode}},
		Edges:    []entities.Edge{{From: "pre-sandbox", To: "post-sandbox"}},
	}

	fs := it.New(t, flow)
	runIDs := fs.TriggerGitHubPR(61, "sb-noop-sha")
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %q, want COMPLETED", status)
	}
	fs.ExpectTaskCount(runIDs[0], 2)
}
