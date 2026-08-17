package taskmanager

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/core/pkg/engine/executor"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func TestSlotWorker_Execute(t *testing.T) {
	q := messager.NewLocalMessager(10)
	router := executor.NewTaskExecutorRouter()
	router.Register(&executor.NoopExecutor{})

	worker := NewSlotWorker("slot-1", "tm-test", "test-namespace", "default", q, router, nil, nil)

	plan := &entities.ExecutionPlan{
		PlanID:         "plan-test-1",
		Namespace:      "test-namespace",
		ResourcePoolID: "default",
		NodeID:         "node-1",
		TaskType:       entities.TaskNoop,
		State:          entities.TaskPending,
		Input:          map[string]any{"key": "val"},
		NodeSpec:       &entities.NodeSpec{Type: entities.NoopNode},
	}
	payload, _ := json.Marshal(plan)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go worker.Loop(ctx)
	time.Sleep(50 * time.Millisecond) // let subscription register

	q.Publish(ctx, messager.ExecPlansTopic("test-namespace", "default", "test-flow", "test-run"), &messager.InterMessage{
		ID:      "msg-1",
		Payload: payload,
	})

	time.Sleep(300 * time.Millisecond)
}

func TestSlotWorker_InvalidPayload(t *testing.T) {
	q := messager.NewLocalMessager(10)
	router := executor.NewTaskExecutorRouter()
	worker := NewSlotWorker("slot-2", "tm-test", "test-namespace", "default", q, router, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go worker.Loop(ctx)
	time.Sleep(50 * time.Millisecond)

	q.Publish(ctx, messager.ExecPlansTopic("test-namespace", "default", "test-flow", "test-run"), &messager.InterMessage{
		ID:      "msg-bad",
		Payload: []byte("not-valid-json"),
	})

	time.Sleep(300 * time.Millisecond)
}

func TestSlotWorker_ExecuteError(t *testing.T) {
	q := messager.NewLocalMessager(10)
	router := executor.NewTaskExecutorRouter()
	router.Register(&failingExecutor{})

	worker := NewSlotWorker("slot-3", "tm-test", "test-namespace", "default", q, router, nil, nil)

	plan := &entities.ExecutionPlan{
		PlanID:         "plan-fail-1",
		Namespace:      "test-namespace",
		ResourcePoolID: "default",
		NodeID:         "node-fail",
		TaskType:       entities.TaskType("failing"),
		State:          entities.TaskPending,
		NodeSpec:       &entities.NodeSpec{Type: entities.NodeType("failing")},
	}
	payload, _ := json.Marshal(plan)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go worker.Loop(ctx)
	time.Sleep(50 * time.Millisecond)

	q.Publish(ctx, messager.ExecPlansTopic("test-namespace", "default", "test-flow", "test-run"), &messager.InterMessage{
		ID:      "msg-fail",
		Payload: payload,
	})

	time.Sleep(300 * time.Millisecond)
}

func TestSlotWorker_EmitDownstreamRetriesPublish(t *testing.T) {
	q := &flakyMessager{failures: 1}
	worker := NewSlotWorker("slot-retry", "tm-test", "test-namespace", "default", q, nil, nil, nil)
	plan := &entities.ExecutionPlan{
		PlanID:                "plan-retry",
		NodeID:                "node-retry",
		AgentFlowDefinitionID: "flow-retry",
		AgentFlowRunID:        "run-retry",
		Namespace:             "test-namespace",
		State:                 entities.Success,
		Result:                &entities.TaskResult{Output: map[string]any{"ok": true}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	worker.emitDownstream(ctx, plan)

	if q.calls != 2 {
		t.Fatalf("expected one failed publish and one retry, got %d calls", q.calls)
	}
	if q.topic != messager.ExecResultsTopic("test-namespace", "flow-retry", "run-retry") {
		t.Fatalf("unexpected topic: %s", q.topic)
	}
}

type failingExecutor struct{}

func (e *failingExecutor) TaskType() entities.TaskType { return "failing" }
func (e *failingExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.TaskResult, error) {
	return nil, context.DeadlineExceeded
}

type flakyMessager struct {
	failures int
	calls    int
	topic    string
}

func (m *flakyMessager) Publish(ctx context.Context, topic string, msg *messager.InterMessage) error {
	m.calls++
	m.topic = topic
	if m.calls <= m.failures {
		return errors.New("transient publish failure")
	}
	return nil
}

func (m *flakyMessager) Subscribe(ctx context.Context, topic string, handler messager.SubHandler) error {
	return nil
}

func (m *flakyMessager) Ack(ctx context.Context, msgID string) error  { return nil }
func (m *flakyMessager) Nack(ctx context.Context, msgID string) error { return nil }
func (m *flakyMessager) Close() error                                 { return nil }
