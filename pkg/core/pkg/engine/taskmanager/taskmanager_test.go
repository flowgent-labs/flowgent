package taskmanager

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// fakeTaskState is a no-op TaskStateStore for unit tests that don't
// exercise persistence (TM calls SaveTask unconditionally — see
// taskmanager.go/taskworker.go — so a nil interface would panic).
type fakeTaskState struct{}

func (fakeTaskState) SaveTask(ctx context.Context, task *entities.TaskRunInfo) error { return nil }

func TestNewTaskManager_Defaults(t *testing.T) {
	tm, err := NewTaskManager(&TaskManagerConfig{
		ID:               "test-tm",
		State:            fakeTaskState{},
		Messager:         messager.NewLocalMessager(10),
		RuntimeClusterID: "default",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tm.ID != "test-tm" {
		t.Errorf("expected ID test-tm, got %s", tm.ID)
	}
	if tm.slotWorkers == nil {
		t.Fatal("expected slot workers")
	}
	// Default SlotCount = 4
	if len(tm.slotWorkers) != 4 {
		t.Fatalf("expected 4 slot workers, got %d", len(tm.slotWorkers))
	}
}

func TestNewTaskManager_ZeroSlotCount(t *testing.T) {
	tm, err := NewTaskManager(&TaskManagerConfig{
		SlotCount:        0,
		State:            fakeTaskState{},
		Messager:         messager.NewLocalMessager(10),
		RuntimeClusterID: "default",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tm.slotWorkers) != 4 {
		t.Fatalf("zero slot count should default to 4, got %d", len(tm.slotWorkers))
	}
}

func TestNewTaskManager_CustomSlots(t *testing.T) {
	tm, err := NewTaskManager(&TaskManagerConfig{
		ID:               "tm-custom",
		SlotCount:        3,
		State:            fakeTaskState{},
		Messager:         messager.NewLocalMessager(10),
		RuntimeClusterID: "default",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tm.slotWorkers) != 3 {
		t.Fatalf("expected 3 slots, got %d", len(tm.slotWorkers))
	}
}

func TestNewTaskManager_AutoID(t *testing.T) {
	tm, err := NewTaskManager(&TaskManagerConfig{
		State:            fakeTaskState{},
		Messager:         messager.NewLocalMessager(10),
		RuntimeClusterID: "default",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tm.ID == "" {
		t.Fatal("expected auto-generated ID")
	}
}

func TestTaskManager_StartStop(t *testing.T) {
	q := messager.NewLocalMessager(10)
	readyCh := make(chan messager.RuntimeReady, 1)
	if err := q.Subscribe(context.Background(), messager.RuntimeReadyWildcard("default", "default", "taskmanager"), func(_ string, payload []byte) {
		var ready messager.RuntimeReady
		if json.Unmarshal(payload, &ready) == nil {
			readyCh <- ready
		}
	}); err != nil {
		t.Fatal(err)
	}
	tm, err := NewTaskManager(&TaskManagerConfig{
		ID:               "tm-startstop",
		SlotCount:        2,
		State:            fakeTaskState{},
		Messager:         q,
		Namespace:        "default",
		RuntimeClusterID: "default",
		Logger:           utils.NewLogger("JSON", "DEBUG"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := tm.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case ready := <-readyCh:
		if ready.WorkerID != "tm-startstop" || ready.Role != "taskmanager" {
			t.Fatalf("unexpected readiness: %+v", ready)
		}
	case <-time.After(time.Second):
		t.Fatal("TaskManager did not advertise readiness after slot subscriptions")
	}
	tm.Stop()
	// Stop is idempotent
	tm.Stop()
}
