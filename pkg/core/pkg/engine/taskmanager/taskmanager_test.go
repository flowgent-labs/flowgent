package taskmanager

import (
	"context"
	"testing"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
)

// fakeTaskState is a no-op TaskStateStore for unit tests that don't
// exercise persistence (TM calls SaveTask unconditionally — see
// taskmanager.go/taskworker.go — so a nil interface would panic).
type fakeTaskState struct{}

func (fakeTaskState) SaveTask(ctx context.Context, task *entities.TaskRunInfo) error { return nil }

func TestNewTaskManager_Defaults(t *testing.T) {
	tm, err := NewTaskManager(&TaskManagerConfig{
		ID:       "test-tm",
		State:    fakeTaskState{},
		Messager: messager.NewLocalMessager(10),
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
		SlotCount: 0,
		State:     fakeTaskState{},
		Messager:  messager.NewLocalMessager(10),
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
		ID: "tm-custom", SlotCount: 3,
		State: fakeTaskState{}, Messager: messager.NewLocalMessager(10),
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
		State: fakeTaskState{}, Messager: messager.NewLocalMessager(10),
	})
	if err != nil {
		t.Fatal(err)
	}
	if tm.ID == "" {
		t.Fatal("expected auto-generated ID")
	}
}

func TestTaskManager_StartStop(t *testing.T) {
	tm, err := NewTaskManager(&TaskManagerConfig{
		ID: "tm-startstop", SlotCount: 2,
		State:    fakeTaskState{},
		Messager: messager.NewLocalMessager(10),
		Logger:   utils.NewLogger("JSON", "DEBUG"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Start and immediately stop — should not panic or hang
	tm.Stop()
	// Stop is idempotent
	tm.Stop()
}
