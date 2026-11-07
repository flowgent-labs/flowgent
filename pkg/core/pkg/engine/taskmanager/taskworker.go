package taskmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/executor"
	"github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// SlotWorker runs a persistent consume-execute loop within a TaskManager.
// Each SlotWorker subscribes to TopicExec and executes ExecutionPlans via a
// TaskExecutorRouter, then publishes downstream-ready plans back to the queue.
type SlotWorker struct {
	id        string
	tmID      string
	namespace string
	clusterID string
	q         messager.IMessager
	router    *executor.TaskExecutorRouter
	state     TaskStateStore
	metrics   *TaskManagerMetrics
}

func NewSlotWorker(id, tmID, namespace, clusterID string, q messager.IMessager, router *executor.TaskExecutorRouter, state TaskStateStore, metrics *TaskManagerMetrics) *SlotWorker {
	return &SlotWorker{
		id:        id,
		tmID:      tmID,
		namespace: namespace,
		clusterID: clusterID,
		q:         q,
		router:    router,
		state:     state,
		metrics:   metrics,
	}
}

// Loop subscribes to execution plans and blocks until ctx is done.
func (sw *SlotWorker) Loop(ctx context.Context) {
	slog.Info("slot worker started", "tm_id", sw.tmID, "slot_id", sw.id)
	defer slog.Info("slot worker stopped", "tm_id", sw.tmID, "slot_id", sw.id)
	if err := sw.Subscribe(ctx); err != nil {
		slog.Error("slot worker subscription failed", "tm_id", sw.tmID, "slot_id", sw.id, "error", err)
		return
	}
	<-ctx.Done()
}

// Subscribe registers this slot's execution handler synchronously. TaskManager
// uses it as a startup barrier before advertising runtime readiness.
func (sw *SlotWorker) Subscribe(ctx context.Context) error {
	if sw.namespace == "" || sw.clusterID == "" {
		return fmt.Errorf("slot worker requires namespace and runtime cluster scope")
	}
	return sw.q.Subscribe(ctx, messager.SharedExecPlans(sw.namespace, sw.clusterID), func(topic string, payload []byte) {
		slog.Debug("slot worker received execution plan", "slot", sw.id, "len", len(payload))
		var plan entities.ExecutionPlan
		if err := json.Unmarshal(payload, &plan); err != nil {
			slog.Error("slot worker cannot unmarshal execution plan", "error", err)
			return
		}
		if plan.Namespace != sw.namespace || plan.RuntimeClusterID != sw.clusterID {
			slog.Error("slot worker rejected out-of-cluster plan", "plan", plan.PlanID,
				"plan_namespace", plan.Namespace, "plan_cluster", plan.RuntimeClusterID,
				"worker_namespace", sw.namespace, "worker_cluster", sw.clusterID)
			return
		}
		slog.Debug("slot worker executing plan", "slot", sw.id, "plan", plan.PlanID, "node", plan.NodeID, "type", string(plan.TaskType))
		execCtx := otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(plan.TraceContext))
		execCtx, span := tracing.Tracer("flowgent/taskmanager").Start(execCtx, "taskmanager.execute",
			trace.WithAttributes(
				attribute.String("run.id", plan.AgentFlowRunID),
				attribute.String("agentflow.id", plan.AgentFlowDefinitionID),
				attribute.String("flowgent.node_id", plan.NodeID),
				attribute.String("flowgent.task_id", plan.TaskID),
				attribute.String("flowgent.plan_id", plan.PlanID),
				attribute.String("flowgent.task_type", string(plan.TaskType)),
				attribute.Int("flowgent.attempt", plan.RetryCount+1),
				attribute.String("flowgent.tm_id", sw.tmID),
				attribute.String("flowgent.slot_id", sw.id),
			),
		)
		span.SetAttributes(tracing.PayloadAttributes("input", plan.Input)...)

		if sw.metrics != nil {
			sw.metrics.SlotsBusy.Add(ctx, 1)
		}

		execStart := time.Now()
		if plan.StartedAt == nil {
			startedAt := execStart.UTC()
			plan.StartedAt = &startedAt
		}
		scope := map[string]map[string]any{"input": plan.Input}
		result, err := sw.router.Execute(execCtx, &plan, scope)
		execDur := time.Since(execStart)

		if err != nil {
			slog.Error("slot worker task failed",
				"plan_id", plan.PlanID,
				"task_type", string(plan.TaskType),
				"error", err,
			)
			plan.State = entities.Failed
			plan.Result = &entities.TaskResult{Error: err.Error()}
			span.RecordError(err)
			span.SetStatus(codes.Error, "executor failed")
		} else if result != nil && result.Error != "" {
			slog.Error("slot worker task failed",
				"plan_id", plan.PlanID,
				"task_type", string(plan.TaskType),
				"error", result.Error,
			)
			plan.State = entities.Failed
			plan.Result = result
			span.SetStatus(codes.Error, result.Error)
		} else {
			slog.Info("slot worker task succeeded", "slot_id", sw.id, "plan_id", plan.PlanID, "node_id", plan.NodeID)
			plan.State = entities.Success
			plan.Result = result
			span.SetAttributes(tracing.PayloadAttributes("output", result.Output)...)
			span.SetStatus(codes.Ok, "done")
		}
		plan.FinishedAt = timePtr()

		if sw.metrics != nil {
			sw.metrics.TaskDuration.Record(ctx, float64(execDur.Milliseconds()))
			sw.metrics.SlotsBusy.Add(ctx, -1)
			sw.metrics.TasksExecuted.Add(ctx, 1, metric.WithAttributes(taskTypeAttr(plan.TaskType)))
		}

		if sw.state != nil {
			_ = sw.state.SaveTask(execCtx, taskRunFromPlan(&plan))
		}

		span.End()
		sw.emitDownstream(execCtx, &plan)
	})
}

func taskRunFromPlan(plan *entities.ExecutionPlan) *entities.TaskRunInfo {
	return &entities.TaskRunInfo{
		BaseEntity:      entities.BaseEntity{ID: plan.TaskID},
		RunID:           plan.AgentFlowRunID,
		NodeKey:         plan.NodeID,
		Attempt:         plan.RetryCount + 1,
		Status:          plan.State,
		Input:           plan.Input,
		Output:          planResultOutput(plan.Result),
		Error:           planResultError(plan.Result),
		MaxRetries:      plan.MaxRetries,
		ExecutionID:     fmt.Sprintf("%s-attempt-%d", plan.PlanID, plan.RetryCount+1),
		ParentNodeRunID: plan.ParentTaskRunID,
		Sequence:        plan.RetryCount + 1,
		StartedAt:       plan.StartedAt,
		FinishedAt:      plan.FinishedAt,
	}
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
		"error":            planResultError(plan.Result),
	})
	topic := messager.ExecResultsTopic(plan.Namespace, plan.AgentFlowDefinitionID, plan.AgentFlowRunID)
	msg := &messager.InterMessage{
		ID:      fmt.Sprintf("status-%s-%s", plan.AgentFlowRunID, plan.NodeID),
		Headers: map[string]string{"task_run_id": plan.AgentFlowRunID, "node_id": plan.NodeID},
		Payload: b,
	}

	for attempt := 1; attempt <= 5; attempt++ {
		if err := sw.q.Publish(ctx, topic, msg); err != nil {
			slog.Warn("slot worker result publish retry",
				"slot_id", sw.id,
				"plan_id", plan.PlanID,
				"node_id", plan.NodeID,
				"topic", topic,
				"attempt", attempt,
				"error", err,
			)
			select {
			case <-ctx.Done():
				slog.Error("slot worker result publish canceled",
					"slot_id", sw.id,
					"plan_id", plan.PlanID,
					"node_id", plan.NodeID,
					"error", ctx.Err(),
				)
				return
			case <-time.After(time.Duration(attempt) * time.Second):
				continue
			}
		}
		return
	}

	slog.Error("slot worker result publish failed",
		"slot_id", sw.id,
		"plan_id", plan.PlanID,
		"node_id", plan.NodeID,
		"topic", topic,
	)
}

func timePtr() *time.Time { t := time.Now(); return &t }
