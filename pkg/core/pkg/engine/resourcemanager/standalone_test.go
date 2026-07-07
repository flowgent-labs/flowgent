package resourcemanager

import (
	"context"
	"testing"

	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
)

// fakeTaskState is a no-op taskmanager.TaskStateStore for unit tests that
// don't exercise persistence (TM/RM call SaveTask unconditionally — see
// taskmanager.go — so a nil interface would panic).
type fakeTaskState struct{}

func (fakeTaskState) SaveTask(ctx context.Context, task *entities.TaskRunInfo) error { return nil }

func TestNewStandaloneResourceManager_Defaults(t *testing.T) {
	rm, err := NewStandaloneResourceManager(&ResourceManagerConfig{
		PoolSize:  0, // should default to 10
		TaskState: fakeTaskState{},
		Messager:  messager.NewLocalMessager(10),
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
		PoolSize:  5,
		TaskState: fakeTaskState{},
		Messager:  messager.NewLocalMessager(10),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rm.poolSize != 5 {
		t.Errorf("expected 5, got %d", rm.poolSize)
	}
}

func TestStandaloneResourceManager_Schedule_Noop(t *testing.T) {
	rm, err := NewStandaloneResourceManager(&ResourceManagerConfig{
		PoolSize: 2, TaskState: fakeTaskState{}, Messager: messager.NewLocalMessager(10),
	})
	if err != nil {
		t.Fatal(err)
	}

	plan := &entities.ExecutionPlan{
		PlanID: "p1", AgentFlowRunID: "r1", NodeID: "n1",
		TaskType: entities.TaskNoop,
		NodeSpec: &entities.NodeSpec{ID: "n1", Type: entities.NoopNode},
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
	rm, err := NewStandaloneResourceManager(&ResourceManagerConfig{
		PoolSize: 2, TaskState: fakeTaskState{}, Messager: messager.NewLocalMessager(10),
	})
	if err != nil {
		t.Fatal(err)
	}

	plan := &entities.ExecutionPlan{
		PlanID: "p1", AgentFlowRunID: "r1", NodeID: "n1",
		TaskType: entities.TaskNoop,
		NodeSpec: &entities.NodeSpec{ID: "n1", Type: entities.NoopNode},
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
		PoolSize: 1, TaskState: fakeTaskState{}, Messager: messager.NewLocalMessager(10),
	})
	if err := rm.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneResourceManager_AvailableSlots(t *testing.T) {
	rm, _ := NewStandaloneResourceManager(&ResourceManagerConfig{
		PoolSize: 3, TaskState: fakeTaskState{}, Messager: messager.NewLocalMessager(10),
	})
	if n := rm.AvailableSlots(); n != 3 {
		t.Fatalf("expected 3 available, got %d", n)
	}
}
