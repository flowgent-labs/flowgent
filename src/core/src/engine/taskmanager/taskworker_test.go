package taskmanager

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/core/src/engine/executor"
	"github.com/flowgent-labs/flowgent/model/src"
	messager "github.com/flowgent-labs/flowgent/messager/src"
)

func TestSlotWorker_Execute(t *testing.T) {
	q := messager.NewLocalMessager(10)
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go worker.Loop(ctx)
	time.Sleep(50 * time.Millisecond) // let subscription register

	q.Publish(ctx, messager.ExecPlansTopic("test-tenant", "test-flow", "test-run"), &messager.InterMessage{
		ID:      "msg-1",
		Payload: payload,
	})

	time.Sleep(300 * time.Millisecond)
}

func TestSlotWorker_InvalidPayload(t *testing.T) {
	q := messager.NewLocalMessager(10)
	router := executor.NewTaskExecutorRouter()
	worker := NewSlotWorker("slot-2", "tm-test", q, router, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go worker.Loop(ctx)
	time.Sleep(50 * time.Millisecond)

	q.Publish(ctx, messager.ExecPlansTopic("test-tenant", "test-flow", "test-run"), &messager.InterMessage{
		ID:      "msg-bad",
		Payload: []byte("not-valid-json"),
	})

	time.Sleep(300 * time.Millisecond)
}

func TestSlotWorker_ExecuteError(t *testing.T) {
	q := messager.NewLocalMessager(10)
	router := executor.NewTaskExecutorRouter()
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go worker.Loop(ctx)
	time.Sleep(50 * time.Millisecond)

	q.Publish(ctx, messager.ExecPlansTopic("test-tenant", "test-flow", "test-run"), &messager.InterMessage{
		ID:      "msg-fail",
		Payload: payload,
	})

	time.Sleep(300 * time.Millisecond)
}

type failingExecutor struct{}

func (e *failingExecutor) TaskType() model.TaskType { return "failing" }
func (e *failingExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	return nil, context.DeadlineExceeded
}
