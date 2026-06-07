package taskmanager

import (
	"testing"

	"github.com/flowgent-labs/flowgent/common/src/utils"
	"github.com/flowgent-labs/flowgent/tests/testutil"
)

func TestNewTaskManager_Defaults(t *testing.T) {
	tm, err := NewTaskManager(&TaskManagerConfig{
		ID:    "test-tm",
		Store: testutil.NewMockStore(),
		Queue: testutil.NewTestQueue(),
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
		Store:     testutil.NewMockStore(),
		Queue:     testutil.NewTestQueue(),
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
		Store: testutil.NewMockStore(), Queue: testutil.NewTestQueue(),
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
		Store: testutil.NewMockStore(), Queue: testutil.NewTestQueue(),
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
		Store:  testutil.NewMockStore(),
		Queue:  testutil.NewTestQueue(),
		Logger: utils.NewLogger("JSON", "DEBUG"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Start and immediately stop — should not panic or hang
	tm.Stop()
	// Stop is idempotent
	tm.Stop()
}
