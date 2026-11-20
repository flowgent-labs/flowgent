package jobmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

type subflowStateStore interface {
	InitializeSubflow(context.Context, string, string) (*entities.SubflowState, error)
	ApplyGraphPatch(context.Context, entities.GraphPatch) (*entities.SubflowState, error)
}

// executeSubflow composes the same JobMaster and ResourceManager. The parent
// never occupies a TM slot while child nodes run, nor creates another FlowRun.
func (jm *JobMaster) executeSubflow(ctx context.Context, parent *entities.FlowRunInfo, plan *entities.ExecutionPlan) (result *entities.TaskResult, executeErr error) {
	store, ok := jm.state.(subflowStateStore)
	if !ok {
		return nil, fmt.Errorf("subflow requires durable API state store")
	}
	state, err := store.InitializeSubflow(ctx, parent.ID, plan.NodeInstanceID)
	if err != nil {
		return nil, err
	}
	plan.TaskID = taskAttemptID(plan.TaskID, plan.RetryCount)
	plan.FencingToken = plan.DispatchEpoch
	plan.State = entities.TaskWaiting
	if plan.StartedAt == nil {
		now := time.Now().UTC()
		plan.StartedAt = &now
	}
	if err := jm.state.SaveTask(ctx, taskRunFromPlan(plan)); err != nil {
		return nil, fmt.Errorf("persist composite wait: %w", err)
	}
	commitCtx := ctx
	defer func() {
		if errors.Is(executeErr, ErrRecoveryBlocked) {
			return
		}
		plan.FinishedAt = TimePtr()
		plan.Result = result
		if executeErr != nil {
			plan.State = entities.Failed
			plan.Result = &entities.TaskResult{Error: executeErr.Error()}
		} else {
			plan.State = entities.Success
		}
		if err := jm.state.SaveTask(commitCtx, taskRunFromPlan(plan)); err != nil {
			executeErr = fmt.Errorf("persist composite outcome: %w", err)
		}
	}()
	if d := state.Config.Limits.MaxActiveDuration; d != "" {
		timeout, e := time.ParseDuration(d)
		if e != nil {
			return nil, e
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if err := utils.ValidateJSONSchema(state.Graph.InputSchema, plan.Input); err != nil {
		return nil, fmt.Errorf("subflow input contract: %w", err)
	}
	for {
		child := NewJobMaster(jm.state, jm.rm, jm.logger, &JobManagerConfig{RuntimeClusterID: jm.clusterID, MaxConcurrentNodes: state.Config.Limits.MaxParallelism})
		child.child = true
		child.graphRevision = state.Revision
		child.slots = jm.slots
		child.instancePrefix = plan.NodeInstanceID + "/"
		run := *parent
		run.Input = plan.Input
		run.Vars = plan.Input
		graph := state.Graph
		graph.Name = plan.AgentFlowDefinitionID // All child transport belongs to the parent FlowRun.
		graph.Namespace = plan.Namespace
		graph.RuntimeMode = plan.RuntimeMode
		if err = child.Execute(ctx, &run, &graph); err != nil {
			return nil, err
		}
		output := map[string]any{}
		for id, value := range child.nodeOutputs {
			output[id] = value
		}
		if !state.Config.Adaptive() || state.Sealed {
			if run.Status == entities.RunFailed {
				return nil, fmt.Errorf("subflow failed: %s", run.Error)
			}
			if err := utils.ValidateJSONSchema(state.Graph.OutputSchema, output); err != nil {
				return nil, fmt.Errorf("subflow output contract: %w", err)
			}
			return &entities.TaskResult{Output: output}, nil
		}
		// A stable planning identity makes a persisted model decision reusable after
		// failure. scheduleNodeWithRetry commits the result before any GraphPatch.
		supervisor := &entities.ExecutionPlan{PlanID: executionPlanID(parent.ID, fmt.Sprintf("%s-supervisor-%d", plan.NodeInstanceID, state.Revision)), TaskID: fmt.Sprintf("task-%s-%s-supervisor-%d", parent.ID, plan.NodeInstanceID, state.Revision), AgentFlowRunID: parent.ID, AgentFlowDefinitionID: plan.AgentFlowDefinitionID, Namespace: plan.Namespace, RuntimeMode: plan.RuntimeMode, RuntimeClusterID: plan.RuntimeClusterID, TaskType: entities.TaskSupervisor, NodeID: fmt.Sprintf("%s/__supervisor_%d", plan.NodeInstanceID, state.Revision), DispatchEpoch: 1, NodeSpec: &entities.NodeSpec{Kind: entities.SupervisorNode, Agent: state.Config.Supervisor, Instruction: "Plan ONLY this subflow. Return JSON {operations:[...]}; allowed operations are declared in config.mutation.allow. Seal only when all work and required nodes succeeded. Never modify the parent graph or contract."}, Input: map[string]any{"graph": state.Graph, "config": state.Config, "outputs": output, "error": run.Error, "graph_revision": state.Revision}, CreatedAt: time.Now()}
		supervisor.NodeInstanceID = supervisor.NodeID
		var decision *entities.TaskResult
		if reader, ok := jm.state.(interface {
			ListTasks(context.Context, string) ([]*entities.TaskRunInfo, error)
		}); ok {
			tasks, e := reader.ListTasks(ctx, parent.ID)
			if e != nil {
				return nil, e
			}
			for _, t := range tasks {
				if t.NodeKey == supervisor.NodeID && t.Status == entities.Success {
					decision = &entities.TaskResult{Output: t.Output}
					break
				}
			}
		}
		if decision == nil {
			decision, err = jm.scheduleNodeWithRetry(ctx, supervisor)
			if err != nil {
				return nil, err
			}
		}
		data, err := json.Marshal(decision.Output)
		if err != nil {
			return nil, err
		}
		var body struct {
			Operations []entities.PatchOperation `json:"operations"`
		}
		if err = json.Unmarshal(data, &body); err != nil {
			return nil, err
		}
		patch := entities.GraphPatch{ID: supervisor.PlanID, FlowRunID: parent.ID, SubflowInstanceID: plan.NodeInstanceID, ExpectedRevision: state.Revision, Operations: body.Operations}
		state, err = store.ApplyGraphPatch(ctx, patch)
		if err != nil {
			return nil, fmt.Errorf("supervisor patch rejected: %w", err)
		}
	}
}
