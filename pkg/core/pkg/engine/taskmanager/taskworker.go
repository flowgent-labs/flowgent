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
	busy      chan struct{}
	id        string
	tmID      string
	namespace string
	clusterID string
	q         messager.IMessager
	router    *executor.TaskExecutorRouter
	state     NodeRunStateStore
	metrics   *TaskManagerMetrics
}

func NewSlotWorker(id, tmID, namespace, clusterID string, q messager.IMessager, router *executor.TaskExecutorRouter, state NodeRunStateStore, metrics *TaskManagerMetrics) *SlotWorker {
	return &SlotWorker{
		id:        id,
		busy:      make(chan struct{}, 1),
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
		select {
		case sw.busy <- struct{}{}:
			defer func() { <-sw.busy }()
		case <-ctx.Done():
			return
		}
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

		if claimer, ok := sw.state.(interface {
			ClaimNodeRun(context.Context, *entities.ExecutionPlan, string) (bool, error)
		}); ok && plan.DispatchEpoch > 0 {
			claimed, err := claimer.ClaimNodeRun(ctx, &plan, sw.tmID+"/"+sw.id)
			if err != nil || !claimed {
				slog.Warn("noderun claim rejected or already consumed", "noderun", plan.NodeRunID, "error", err)
				return
			}
		}
		slog.Debug("slot worker executing plan", "slot", sw.id, "plan", plan.PlanID, "node", plan.NodeInstanceID, "type", string(plan.NodeKind))
		execCtx := otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(plan.TraceContext))
		execCtx, span := tracing.Tracer("flowgent/taskmanager").Start(execCtx, "taskmanager.execute",
			trace.WithAttributes(
				attribute.String("graph_run.id", plan.GraphRunID),
				attribute.String("agent_graph.name", plan.AgentGraphName),
				attribute.String("flowgent.node_instance_id", plan.NodeInstanceID),
				attribute.String("flowgent.node_run_id", plan.NodeRunID),
				attribute.String("flowgent.plan_id", plan.PlanID),
				attribute.String("flowgent.node_kind", string(plan.NodeKind)),
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
			slog.Error("slot worker noderun failed",
				"plan_id", plan.PlanID,
				"node_kind", string(plan.NodeKind),
				"error", err,
			)
			plan.State = entities.NodeRunFailed
			plan.Result = &entities.NodeResult{Error: err.Error()}
			span.RecordError(err)
			span.SetStatus(codes.Error, "executor failed")
		} else if result != nil && result.Error != "" {
			slog.Error("slot worker noderun failed",
				"plan_id", plan.PlanID,
				"node_kind", string(plan.NodeKind),
				"error", result.Error,
			)
			plan.State = entities.NodeRunFailed
			plan.Result = result
			span.SetStatus(codes.Error, result.Error)
		} else if result != nil && result.State == entities.NodeRunWaiting {
			plan.State = entities.NodeRunWaiting
			plan.Result = result
		} else if result == nil {
			plan.State = entities.NodeRunFailed
			plan.Result = &entities.NodeResult{Error: "executor returned no result"}
		} else {
			slog.Info("slot worker noderun succeeded", "slot_id", sw.id, "plan_id", plan.PlanID, "node_instance_id", plan.NodeInstanceID)
			plan.State = entities.NodeRunSucceeded
			plan.Result = result
			span.SetAttributes(tracing.PayloadAttributes("output", result.Output)...)
			span.SetStatus(codes.Ok, "done")
		}
		if plan.State != entities.NodeRunWaiting {
			plan.FinishedAt = timePtr()
		}

		if sw.metrics != nil {
			sw.metrics.TaskDuration.Record(ctx, float64(execDur.Milliseconds()))
			sw.metrics.SlotsBusy.Add(ctx, -1)
			sw.metrics.TasksExecuted.Add(ctx, 1, metric.WithAttributes(taskTypeAttr(plan.NodeKind)))
		}

		if sw.state != nil {
			if err := sw.state.SaveNodeRun(execCtx, nodeRunFromPlan(&plan)); err != nil {
				slog.Error("noderun result persistence failed", "noderun", plan.NodeRunID, "error", err)
				span.RecordError(err)
				span.End()
				return
			}
		}

		span.End()
		sw.emitDownstream(execCtx, &plan)
	})
}

func nodeRunFromPlan(plan *entities.ExecutionPlan) *entities.NodeRun {
	return entities.NodeRunFromPlan(plan)
}

func planResultOutput(r *entities.NodeResult) map[string]any {
	if r == nil {
		return nil
	}
	return r.Output
}

func planResultError(r *entities.NodeResult) string {
	if r == nil {
		return ""
	}
	return r.Error
}

// emitDownstream publishes execution status so the JM can detect satisfied
// dependencies and publish the next wave of plans.
func (sw *SlotWorker) emitDownstream(ctx context.Context, plan *entities.ExecutionPlan) {
	b, _ := json.Marshal(map[string]any{
		"plan_id":     plan.PlanID,
		"node_run_id": plan.NodeRunID, "node_instance_id": plan.NodeInstanceID, "dispatch_epoch": plan.DispatchEpoch,
		"graph_run_id": plan.GraphRunID,
		"state":        string(plan.State),
		"output":       planResultOutput(plan.Result),
		"error":        planResultError(plan.Result),
	})
	topic := messager.ExecResultsTopic(plan.Namespace, plan.AgentGraphID, plan.GraphRunID)
	msg := &messager.InterMessage{
		ID:      fmt.Sprintf("status-%s-%d", plan.NodeRunID, plan.DispatchEpoch),
		Headers: map[string]string{"node_run_id": plan.GraphRunID, "node_instance_id": plan.NodeInstanceID},
		Payload: b,
	}

	for attempt := 1; attempt <= 5; attempt++ {
		if err := sw.q.Publish(ctx, topic, msg); err != nil {
			slog.Warn("slot worker result publish retry",
				"slot_id", sw.id,
				"plan_id", plan.PlanID,
				"node_instance_id", plan.NodeInstanceID,
				"topic", topic,
				"attempt", attempt,
				"error", err,
			)
			select {
			case <-ctx.Done():
				slog.Error("slot worker result publish canceled",
					"slot_id", sw.id,
					"plan_id", plan.PlanID,
					"node_instance_id", plan.NodeInstanceID,
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
		"node_instance_id", plan.NodeInstanceID,
		"topic", topic,
	)
}

func timePtr() *time.Time { t := time.Now(); return &t }
