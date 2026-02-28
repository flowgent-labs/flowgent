package scheduler

import (
	"context"
	"testing"

	"github.com/flowgent-labs/flowgent/src/engine"
	"github.com/flowgent-labs/flowgent/src/engine/testutil"
	"github.com/flowgent-labs/flowgent/src/model"
)

func TestNewResourceManager_Local(t *testing.T) {
	cfg := &ResourceManagerConfig{
		Provider: engine.ProviderLocal,
		PoolSize: 2,
		Store:    testutil.NewMockStore(),
	}
	rm, err := NewResourceManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rm.Provider() != engine.ProviderLocal {
		t.Error("expected local RM type")
	}
}

func TestLocalResourceManager_Schedule(t *testing.T) {
	mockStore := testutil.NewMockStore()
	cfg := &ResourceManagerConfig{
		Provider: engine.ProviderLocal,
		PoolSize: 2,
		Store:    mockStore,
		Agents:   nil,
	}
	rm, err := NewResourceManager(cfg)
	if err != nil {
		t.Fatal(err)
	}

	plan := &model.ExecutionPlan{
		PlanID:         "test-plan-1",
		AgentFlowRunID: "run-1",
		NodeID:         "node-1",
		TaskType:       model.TaskNoop,
		NodeSpec:       &model.NodeSpec{ID: "node-1", Type: model.NoopNode},
	}
	result, err := rm.Schedule(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected result")
	}

	_ = rm.Shutdown(context.Background())
}

func TestKubernetesResourceManager_Type(t *testing.T) {
	rm := &KubernetesResourceManager{
		namespace:  "default",
		deployName: "flowgent-taskmanager",
		minTMs:     1,
		maxTMs:     5,
		currentTMs: 1,
	}
	if rm.Provider() != engine.ProviderKubernetes {
		t.Error("expected kubernetes RM type")
	}
}
