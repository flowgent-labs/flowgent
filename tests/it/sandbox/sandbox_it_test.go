package sandbox

import (
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/tests/it"
)

func TestSandbox_ExecutorWiring(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "sb-wiring", TenantID: "test"},
		Vars:       map[string]any{"repo": "wl4g/rengine"},
		Triggers:   []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes: []entities.Node{{
			ID: "shell-step", Type: entities.SandboxNode,
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
	if status != string(entities.RunCompleted) && status != string(entities.RunFailed) {
		t.Fatalf("run status = %q, expected COMPLETED or FAILED", status)
	}
}

func TestSandbox_NoopFallback(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "sb-noop", TenantID: "test"},
		Vars:       map[string]any{"repo": "wl4g/rengine"},
		Triggers:   []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes:      []entities.Node{entities.Node{ID: "pre-sandbox", Type: entities.NoopNode}, entities.Node{ID: "post-sandbox", Type: entities.NoopNode}},
		Edges:      []entities.Edge{entities.Edge{From: "pre-sandbox", To: "post-sandbox"}},
	}

	fs := it.New(t, flow)
	runIDs := fs.TriggerGitHubPR(61, "sb-noop-sha")
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %q, want COMPLETED", status)
	}
}
