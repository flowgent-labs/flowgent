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

type subgraphStateStore interface {
	InitializeSubGraph(context.Context, string, string) (*entities.SubGraph, error)
	ApplyGraphPatch(context.Context, entities.GraphPatch) (*entities.SubGraph, error)
}

// executeSubGraph composes the same JobMaster and ResourceManager. The parent
// never occupies a TM slot while child nodes run, nor creates another GraphRun.
func (jm *JobMaster) executeSubGraph(ctx context.Context, parent *entities.GraphRun, plan *entities.ExecutionPlan) (result *entities.NodeResult, executeErr error) {
	store, ok := jm.state.(subgraphStateStore)
	if !ok {
		return nil, fmt.Errorf("subgraph requires durable API state store")
	}
	state, err := store.InitializeSubGraph(ctx, parent.ID, plan.NodeInstanceID)
	if err != nil {
		return nil, err
	}
	plan.NodeRunID = taskAttemptID(plan.NodeRunID, plan.RetryCount)
	plan.FencingToken = plan.DispatchEpoch
	plan.State = entities.NodeRunWaiting
	if plan.StartedAt == nil {
		now := time.Now().UTC()
		plan.StartedAt = &now
	}
	if err := jm.state.SaveNodeRun(ctx, nodeRunFromPlan(plan)); err != nil {
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
			plan.State = entities.NodeRunFailed
			plan.Result = &entities.NodeResult{Error: executeErr.Error()}
		} else {
			plan.State = entities.NodeRunSucceeded
		}
		if err := jm.state.SaveNodeRun(commitCtx, nodeRunFromPlan(plan)); err != nil {
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
		return nil, fmt.Errorf("subgraph input contract: %w", err)
	}
	for {
		child := NewJobMaster(jm.state, jm.rm, jm.logger, &JobManagerConfig{RuntimeClusterID: jm.clusterID, MaxConcurrentNodes: state.Config.Limits.MaxParallelism})
		child.child = true
		child.graphRevision = state.Revision
		child.slots = jm.slots
		child.instancePrefix = plan.NodeInstanceID + "/"
		run := *parent
		run.Input = plan.Input
		graph := state.Graph
		graph.Name = plan.AgentGraphName // All child transport belongs to the parent GraphRun.
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
			if run.Status == entities.GraphRunFailed {
				return nil, fmt.Errorf("subgraph failed: %s", run.Error)
			}
			if err := utils.ValidateJSONSchema(state.Graph.OutputSchema, output); err != nil {
				return nil, fmt.Errorf("subgraph output contract: %w", err)
			}
			return &entities.NodeResult{Output: output}, nil
		}
		// A stable planning identity makes a persisted model decision reusable after
		// failure. scheduleNodeWithRetry commits the result before any GraphPatch.
		supervisor := &entities.ExecutionPlan{PlanID: executionPlanID(parent.ID, fmt.Sprintf("%s-supervisor-%d", plan.NodeInstanceID, state.Revision)), NodeRunID: fmt.Sprintf("noderun-%s-%s-supervisor-%d", parent.ID, plan.NodeInstanceID, state.Revision), GraphRunID: parent.ID, AgentGraphName: plan.AgentGraphName, AgentGraphID: plan.AgentGraphID, Namespace: plan.Namespace, RuntimeMode: plan.RuntimeMode, RuntimeClusterID: plan.RuntimeClusterID, NodeKind: entities.SupervisorNode, NodeInstanceID: fmt.Sprintf("%s/__supervisor_%d", plan.NodeInstanceID, state.Revision), DispatchEpoch: 1, Node: &entities.GraphNode{Kind: entities.SupervisorNode, Agent: state.Config.Supervisor, Instruction: "Plan ONLY this subgraph. Return JSON {operations:[...]}; allowed operations are declared in config.mutation.allow. Seal only when all work and required nodes succeeded. Never modify the parent graph or contract."}, Input: map[string]any{"graph": state.Graph, "config": state.Config, "outputs": output, "error": run.Error, "graph_revision": state.Revision}, CreatedAt: time.Now()}
		supervisor.NodeInstanceID = supervisor.NodeInstanceID
		var decision *entities.NodeResult
		if reader, ok := jm.state.(interface {
			ListNodeRuns(context.Context, string) ([]*entities.NodeRun, error)
		}); ok {
			tasks, e := reader.ListNodeRuns(ctx, parent.ID)
			if e != nil {
				return nil, e
			}
			for _, t := range tasks {
				if t.NodeInstanceID == supervisor.NodeInstanceID && t.Status == entities.NodeRunSucceeded {
					decision = &entities.NodeResult{Output: t.Output}
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
		patch := entities.GraphPatch{ID: supervisor.PlanID, GraphRunID: parent.ID, SubGraphInstanceID: plan.NodeInstanceID, ExpectedRevision: state.Revision, Operations: body.Operations}
		state, err = store.ApplyGraphPatch(ctx, patch)
		if err != nil {
			return nil, fmt.Errorf("supervisor patch rejected: %w", err)
		}
	}
}
