package taskmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/flowgent-labs/flowgent/core/pkg/engine/executor"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/messager/pkg"

	"go.opentelemetry.io/otel/metric"
)

// SlotWorker runs a persistent consume-execute loop within a TaskManager.
// Each SlotWorker subscribes to TopicExec and executes ExecutionPlans via a
// TaskExecutorRouter, then publishes downstream-ready plans back to the queue.
type SlotWorker struct {
	id      string
	tmID    string
	q       messager.IMessager
	router  *executor.TaskExecutorRouter
	state   TaskStateStore
	metrics *TaskManagerMetrics
}

func NewSlotWorker(id, tmID string, q messager.IMessager, router *executor.TaskExecutorRouter, state TaskStateStore, metrics *TaskManagerMetrics) *SlotWorker {
	return &SlotWorker{
		id:      id,
		tmID:    tmID,
		q:       q,
		router:  router,
		state:   state,
		metrics: metrics,
	}
}

// Loop subscribes to execution plans and blocks until ctx is done.
func (sw *SlotWorker) Loop(ctx context.Context) {
	slog.Info("slot worker started", "tm_id", sw.tmID, "slot_id", sw.id)
	defer slog.Info("slot worker stopped", "tm_id", sw.tmID, "slot_id", sw.id)

	sw.q.Subscribe(ctx, messager.SharedExecPlans(), func(topic string, payload []byte) {
		slog.Debug("slot worker received execution plan", "slot", sw.id, "len", len(payload))
		var plan entities.ExecutionPlan
		if err := json.Unmarshal(payload, &plan); err != nil {
			slog.Error("slot worker cannot unmarshal execution plan", "error", err)
			return
		}
		slog.Debug("slot worker executing plan", "slot", sw.id, "plan", plan.PlanID, "node", plan.NodeID, "type", string(plan.TaskType))

		if sw.metrics != nil {
			sw.metrics.SlotsBusy.Add(ctx, 1)
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
			plan.State = entities.Failed
			plan.Result = &entities.TaskResult{Error: err.Error()}
		} else {
			slog.Info("slot worker task succeeded", "slot_id", sw.id, "plan_id", plan.PlanID, "node_id", plan.NodeID)
			plan.State = entities.Success
			plan.Result = result
			plan.FinishedAt = timePtr()
		}

		if sw.metrics != nil {
			sw.metrics.TaskDuration.Record(ctx, float64(execDur.Milliseconds()))
			sw.metrics.SlotsBusy.Add(ctx, -1)
			sw.metrics.TasksExecuted.Add(ctx, 1, metric.WithAttributes(taskTypeAttr(plan.TaskType)))
		}

		_ = sw.state.SaveTask(ctx, &entities.TaskRunInfo{
			BaseEntity:     entities.BaseEntity{ID: plan.TaskID},
			AgentFlowRunID: plan.AgentFlowRunID,
			NodeID:         plan.NodeID,
			Status:         plan.State,
			Output:         planResultOutput(plan.Result),
			Error:          planResultError(plan.Result),
			ExecID:         plan.PlanID,
			Input:          plan.Input,
		})

		sw.emitDownstream(ctx, &plan)
	})

	<-ctx.Done()
}

func planResultOutput(r *entities.TaskResult) map[string]any {
	if r == nil {
		return nil
	}
	return r.Output
}

func planResultError(r *entities.TaskResult) string {
	if r == nil {
		return ""
	}
	return r.Error
}

// emitDownstream publishes execution status so the JM can detect satisfied
// dependencies and publish the next wave of plans.
func (sw *SlotWorker) emitDownstream(ctx context.Context, plan *entities.ExecutionPlan) {
	b, _ := json.Marshal(map[string]any{
		"plan_id":          plan.PlanID,
		"agentflow_run_id": plan.AgentFlowRunID,
		"node_id":          plan.NodeID,
		"state":            string(plan.State),
		"output":           planResultOutput(plan.Result),
	})
	_ = sw.q.Publish(ctx, messager.ExecResultsTopic(plan.Namespace, plan.AgentFlowDefinitionID, plan.AgentFlowRunID), &messager.InterMessage{
		ID:      fmt.Sprintf("status-%s-%s", plan.AgentFlowRunID, plan.NodeID),
		Headers: map[string]string{"task_run_id": plan.AgentFlowRunID, "node_id": plan.NodeID},
		Payload: b,
	})
}

func timePtr() *time.Time { t := time.Now(); return &t }
