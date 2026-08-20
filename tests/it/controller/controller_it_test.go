package controller

import (
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/tests/it"
)

// TestController_DeployAndRunLifecycle registers a flow, triggers it via a
// GitHub webhook, and verifies the run reaches COMPLETED.
func TestController_DeployAndRunLifecycle(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "ctrl-lifecycle", Namespace: "test"},
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
		Vars:     map[string]any{"repo": "wl4g/rengine"},
		Triggers: []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes:    []entities.Node{entities.Node{ID: "init", Kind: entities.NoopNode}, entities.Node{ID: "process", Kind: entities.NoopNode}, entities.Node{ID: "finalize", Kind: entities.NoopNode}},
		Edges:    []entities.Edge{entities.Edge{From: "init", To: "process"}, entities.Edge{From: "process", To: "finalize"}},
	}

	fs := it.New(t, flow)
	runIDs := fs.TriggerGitHubPR(42, "feed0001")
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %q, want COMPLETED", status)
	}
}

func TestController_MultipleRunsSameFlow(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "ctrl-concurrent", Namespace: "test"},
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
		Vars:     map[string]any{"repo": "wl4g/rengine"},
		Triggers: []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes:    []entities.Node{entities.Node{ID: "step1", Kind: entities.NoopNode}, entities.Node{ID: "step2", Kind: entities.NoopNode}, entities.Node{ID: "step3", Kind: entities.NoopNode}},
		Edges:    []entities.Edge{entities.Edge{From: "step1", To: "step2"}, entities.Edge{From: "step2", To: "step3"}},
	}

	fs := it.New(t, flow)
	runIDs1 := fs.TriggerGitHubPR(101, "aaa111")
	runIDs2 := fs.TriggerGitHubPR(102, "bbb222")
	allIDs := append(runIDs1, runIDs2...)
	if len(allIDs) != 2 {
		t.Fatalf("expected 2 runs, got %d: %v", len(allIDs), allIDs)
	}
	for _, id := range allIDs {
		if status := fs.WaitRun(id, 30*time.Second); status != string(entities.RunCompleted) {
			t.Errorf("run %s status = %q, want COMPLETED", id, status)
		}
	}
}

func TestController_FlowReRegistration(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "ctrl-reregister", Namespace: "test"},
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication,
		Vars:     map[string]any{"repo": "wl4g/rengine"},
		Triggers: []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes:    []entities.Node{entities.Node{ID: "start", Kind: entities.NoopNode}, entities.Node{ID: "intermediate", Kind: entities.NoopNode}, entities.Node{ID: "finish", Kind: entities.NoopNode}},
		Edges:    []entities.Edge{entities.Edge{From: "start", To: "intermediate"}, entities.Edge{From: "intermediate", To: "finish"}},
	}

	fs := it.New(t, flow)
	runIDs := fs.TriggerGitHubPR(200, "first")
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 initial run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("initial run status = %q, want COMPLETED", status)
	}
	runIDs2 := fs.TriggerGitHubPR(201, "second")
	if len(runIDs2) != 1 {
		t.Fatalf("expected 1 second run, got %v", runIDs2)
	}
	if status := fs.WaitRun(runIDs2[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("second run status = %q, want COMPLETED", status)
	}
}
