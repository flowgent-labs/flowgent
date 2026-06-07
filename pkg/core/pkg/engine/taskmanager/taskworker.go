package taskmanager

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/flowgent-labs/flowgent/core/pkg/engine/executor"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/taskplan"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.opentelemetry.io/otel/metric"
)

// SlotWorker runs a persistent consume-execute loop within a TaskManager.
// Each SlotWorker subscribes to TopicExec and executes ExecutionPlans via a
// TaskExecutorRouter, then publishes downstream-ready plans back to the queue.
type SlotWorker struct {
	id        string
	tmID      string
	q         messager.IMessager
	router    *executor.TaskExecutorRouter
	planStore taskplan.ITaskPlanStore
	metrics   *TaskManagerMetrics
}

func NewSlotWorker(id, tmID string, q messager.IMessager, router *executor.TaskExecutorRouter, s store.IStore, metrics *TaskManagerMetrics) *SlotWorker {
	var planStore taskplan.ITaskPlanStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		planStore = taskplan.NewTaskPlanPostgresStore(db)
	case *sql.DB:
		planStore = taskplan.NewTaskPlanSQLiteStore(db)
	}
	return &SlotWorker{
		id:        id,
		tmID:      tmID,
		q:         q,
		router:    router,
		planStore: planStore,
		metrics:   metrics,
	}
}

// Loop subscribes to execution plans and blocks until ctx is done.
func (sw *SlotWorker) Loop(ctx context.Context) {
	slog.Info("slot worker started", "tm_id", sw.tmID, "slot_id", sw.id)
	defer slog.Info("slot worker stopped", "tm_id", sw.tmID, "slot_id", sw.id)

	sw.q.Subscribe(ctx, messager.SharedExecPlans(), func(topic string, payload []byte) {
		var plan model.ExecutionPlan
		if err := json.Unmarshal(payload, &plan); err != nil {
			slog.Error("slot worker cannot unmarshal execution plan", "error", err)
			return
		}

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
			plan.State = model.Failed
			plan.Result = &model.TaskResult{Error: err.Error()}
		} else {
			plan.State = model.Success
			plan.Result = result
			plan.FinishedAt = timePtr()
		}

		if sw.metrics != nil {
			sw.metrics.TaskDuration.Record(ctx, float64(execDur.Milliseconds()))
			sw.metrics.SlotsBusy.Add(ctx, -1)
			sw.metrics.TasksExecuted.Add(ctx, 1, metric.WithAttributes(taskTypeAttr(plan.TaskType)))
		}

		_ = sw.planStore.Save(ctx, taskplan.PlanToTaskRun(&plan))

		if plan.Result != nil && plan.Result.Output != nil {
			sw.emitDownstream(ctx, &plan)
		}
	})

	<-ctx.Done()
}

// emitDownstream publishes execution status so the JM can detect satisfied
// dependencies and publish the next wave of plans.
func (sw *SlotWorker) emitDownstream(ctx context.Context, plan *model.ExecutionPlan) {
	b, _ := json.Marshal(map[string]any{
		"plan_id":          plan.PlanID,
		"agentflow_run_id": plan.AgentFlowRunID,
		"node_id":          plan.NodeID,
		"state":            string(plan.State),
	})
	_ = sw.q.Publish(ctx, messager.ExecResultsTopic(plan.TenantID, plan.AgentFlowDefinitionID, plan.AgentFlowRunID), &messager.InterMessage{
		ID:      fmt.Sprintf("status-%s-%s", plan.AgentFlowRunID, plan.NodeID),
		Headers: map[string]string{"task_run_id": plan.AgentFlowRunID, "node_id": plan.NodeID},
		Payload: b,
	})
}

func timePtr() *time.Time { t := time.Now(); return &t }
