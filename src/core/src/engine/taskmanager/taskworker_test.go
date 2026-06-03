package taskmanager

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/core/src/engine/executor"
	"github.com/flowgent-labs/flowgent/model/src"
	messaging "github.com/flowgent-labs/flowgent/messaging/src"
)

func TestSlotWorker_DequeueAndExecute(t *testing.T) {
	q := messaging.NewLocalMessager(10)
	router := executor.NewTaskExecutorRouter()
	router.Register(&executor.NoopExecutor{})

	worker := NewSlotWorker("slot-1", "tm-test", q, router, nil, nil)

	plan := &model.ExecutionPlan{
		PlanID:   "plan-test-1",
		NodeID:   "node-1",
		TaskType: model.TaskNoop,
		State:    model.TaskPending,
		Input:    map[string]any{"key": "val"},
		NodeSpec: &model.NodeSpec{Type: model.NoopNode},
	}
	payload, _ := json.Marshal(plan)
	msg := &messaging.Message{
		ID:        "msg-1",
		Topic:     "flowgent/exec",
		Payload:   payload,
		TaskRunID: "run-1",
		NodeID:    "node-1",
	}
	q.Push(context.Background(), msg)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		worker.Loop(ctx)
	}()

	time.Sleep(500 * time.Millisecond)
}

func TestSlotWorker_InvalidPayload(t *testing.T) {
	q := messaging.NewLocalMessager(10)
	router := executor.NewTaskExecutorRouter()
	worker := NewSlotWorker("slot-2", "tm-test", q, router, nil, nil)

	msg := &messaging.Message{
		ID:      "msg-bad",
		Topic:   "flowgent/exec",
		Payload: []byte("not-valid-json"),
		NodeID:  "node-1",
	}
	q.Push(context.Background(), msg)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		worker.Loop(ctx)
	}()

	time.Sleep(500 * time.Millisecond)
}

func TestSlotWorker_ExecuteError(t *testing.T) {
	q := messaging.NewLocalMessager(10)
	router := executor.NewTaskExecutorRouter()
	// Register a failing executor
	router.Register(&failingExecutor{})

	worker := NewSlotWorker("slot-3", "tm-test", q, router, nil, nil)

	plan := &model.ExecutionPlan{
		PlanID:   "plan-fail-1",
		NodeID:   "node-fail",
		TaskType: model.TaskType("failing"),
		State:    model.TaskPending,
		NodeSpec: &model.NodeSpec{Type: model.NodeType("failing")},
	}
	payload, _ := json.Marshal(plan)
	q.Push(context.Background(), &messaging.Message{
		ID: "msg-fail", Topic: "flowgent/exec", Payload: payload,
		TaskRunID: "run-1", NodeID: "node-fail",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		worker.Loop(ctx)
	}()
	time.Sleep(500 * time.Millisecond)
}

// failingExecutor always returns an error.
type failingExecutor struct{}

func (e *failingExecutor) TaskType() model.TaskType { return "failing" }
func (e *failingExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	return nil, context.DeadlineExceeded
}
