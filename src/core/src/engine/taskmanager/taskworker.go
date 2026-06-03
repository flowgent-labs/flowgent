package taskmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/flowgent-labs/flowgent/core/src/engine/executor"
	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/messaging/src"
	"github.com/flowgent-labs/flowgent/store/src"

	"go.opentelemetry.io/otel/metric"
)

// SlotWorker runs a persistent consume-execute loop within a TaskManager.
// Each SlotWorker is a goroutine that blocks on queue.Dequeue(), executes
// the received ExecutionPlan via a TaskExecutorRouter, and emits
// downstream-ready plans back into the queue.
type SlotWorker struct {
	id      string
	tmID    string
	q       messaging.Queue
	router  *executor.TaskExecutorRouter
	store   store.Store
	metrics *TaskManagerMetrics
}

func NewSlotWorker(id, tmID string, q messaging.Queue, router *executor.TaskExecutorRouter, store store.Store, metrics *TaskManagerMetrics) *SlotWorker {
	return &SlotWorker{
		id:      id,
		tmID:    tmID,
		q:       q,
		router:  router,
		store:   store,
		metrics: metrics,
	}
}

// Loop runs the persistent dequeue→execute→emit loop. Blocks until ctx is done.
func (sw *SlotWorker) Loop(ctx context.Context) {
	slog.Info("slot worker started", "tm_id", sw.tmID, "slot_id", sw.id)
	defer slog.Info("slot worker stopped", "tm_id", sw.tmID, "slot_id", sw.id)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		dequeueStart := time.Now()
		msg, err := sw.q.Dequeue(ctx, "tm-pool")
		if err != nil {
			slog.Debug("slot worker dequeue error", "slot_id", sw.id, "error", err)
			time.Sleep(time.Second)
			continue
		}
		if msg == nil {
			continue
		}

		if sw.metrics != nil {
			sw.metrics.DequeueLatency.Record(ctx, float64(time.Since(dequeueStart).Milliseconds()))
			sw.metrics.SlotsBusy.Add(ctx, 1)
		}

		var plan model.ExecutionPlan
		if err := json.Unmarshal(msg.Payload, &plan); err != nil {
			slog.Error("slot worker cannot unmarshal execution plan", "msg_id", msg.ID, "error", err)
			sw.q.Nack(ctx, msg.ID)
			continue
		}

		execStart := time.Now()
		scope := map[string]map[string]any{"input": plan.Input}
		result, err := sw.router.Execute(ctx, &plan, scope)
		execDur := time.Since(execStart)

		if err != nil {
			slog.Error("slot worker task failed",
				"plan_id", plan.PlanID,
				"task_type", string(plan.TaskType),
				"error", err,
			)
			plan.State = model.Failed
			plan.Result = &model.TaskResult{Error: err.Error()}
			sw.q.Nack(ctx, msg.ID)
		} else {
			plan.State = model.Success
			plan.Result = result
			plan.FinishedAt = timePtr()
			sw.q.Ack(ctx, msg.ID)
		}

		if sw.metrics != nil {
			sw.metrics.TaskDuration.Record(ctx, float64(execDur.Milliseconds()))
			sw.metrics.SlotsBusy.Add(ctx, -1)
			sw.metrics.TasksExecuted.Add(ctx, 1, metric.WithAttributes(taskTypeAttr(plan.TaskType)))
		}

		// Persist updated plan state to store
		_ = sw.store.SaveExecutionPlan(ctx, &plan)

		// Emit downstream ready plans (condition results → branch routing)
		if plan.Result != nil && plan.Result.Output != nil {
			sw.emitDownstream(ctx, &plan)
		}
	}
}

// emitDownstream computes and publishes downstream-ready execution plans
// for nodes whose dependencies are now satisfied.
func (sw *SlotWorker) emitDownstream(ctx context.Context, plan *model.ExecutionPlan) {
	// The downstream nodes are computed by the JobManager during DAG build.
	// The plan.NodeID output is stored, and the JM (listening on status topic)
	// detects that dependencies are satisfied and publishes the next wave.
	// SlotWorker just publishes the status; JM does the rest.
	status := map[string]any{
		"plan_id":          plan.PlanID,
		"agentflow_run_id": plan.AgentFlowRunID,
		"node_id":          plan.NodeID,
		"state":            string(plan.State),
	}

	b, _ := json.Marshal(status)
	_ = sw.q.Push(ctx, &messaging.Message{
		ID:        fmt.Sprintf("status-%s-%s", plan.AgentFlowRunID, plan.NodeID),
		TaskRunID: plan.AgentFlowRunID,
		NodeID:    plan.NodeID,
		Payload:   b,
	})
}

func timePtr() *time.Time { t := time.Now(); return &t }
