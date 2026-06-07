package resourcemanager

import (
	"context"
	"testing"

	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/tests/testutil"
)

func TestNewStandaloneResourceManager_Defaults(t *testing.T) {
	rm, err := NewStandaloneResourceManager(&ResourceManagerConfig{
		PoolSize: 0, // should default to 10
		Store:    testutil.NewMockStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rm.Provider() != engine.ProviderStandalone {
		t.Error("expected local provider type")
	}
	if rm.poolSize != 10 {
		t.Errorf("expected default pool size 10, got %d", rm.poolSize)
	}
	_ = rm.Shutdown(context.Background())
}

func TestNewStandaloneResourceManager_CustomPoolSize(t *testing.T) {
	rm, err := NewStandaloneResourceManager(&ResourceManagerConfig{
		PoolSize: 5,
		Store:    testutil.NewMockStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rm.poolSize != 5 {
		t.Errorf("expected 5, got %d", rm.poolSize)
	}
}

func TestStandaloneResourceManager_Schedule_Noop(t *testing.T) {
	mockStore := testutil.NewMockStore()
	rm, err := NewStandaloneResourceManager(&ResourceManagerConfig{
		PoolSize: 2, Store: mockStore,
	})
	if err != nil {
		t.Fatal(err)
	}

	plan := &model.ExecutionPlan{
		PlanID: "p1", AgentFlowRunID: "r1", NodeID: "n1",
		TaskType: model.TaskNoop,
		NodeSpec: &model.NodeSpec{ID: "n1", Type: model.NoopNode},
	}
	result, err := rm.Schedule(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
}

func TestStandaloneResourceManager_Schedule_ConcurrentSlots(t *testing.T) {
	mockStore := testutil.NewMockStore()
	rm, err := NewStandaloneResourceManager(&ResourceManagerConfig{
		PoolSize: 2, Store: mockStore,
	})
	if err != nil {
		t.Fatal(err)
	}

	plan := &model.ExecutionPlan{
		PlanID: "p1", AgentFlowRunID: "r1", NodeID: "n1",
		TaskType: model.TaskNoop,
		NodeSpec: &model.NodeSpec{ID: "n1", Type: model.NoopNode},
	}
	// Should schedule same plan twice without blocking (pool=2)
	result, err := rm.Schedule(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	result2, err := rm.Schedule(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result2 == nil {
		t.Fatal("expected result")
	}
}

func TestStandaloneResourceManager_Validate(t *testing.T) {
	rm, _ := NewStandaloneResourceManager(&ResourceManagerConfig{
		PoolSize: 1, Store: testutil.NewMockStore(),
	})
	if err := rm.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneResourceManager_AvailableSlots(t *testing.T) {
	mockStore := testutil.NewMockStore()
	rm, _ := NewStandaloneResourceManager(&ResourceManagerConfig{
		PoolSize: 3, Store: mockStore,
	})
	if n := rm.AvailableSlots(); n != 3 {
		t.Fatalf("expected 3 available, got %d", n)
	}
}
