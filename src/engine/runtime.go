package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrSupervisorAbort = errors.New("supervisor abort")
)

type SupervisorActionError struct {
	Action        string
	Target        string
	Reason        string
	InjectedNodes []InjectedNode
}

func (e *SupervisorActionError) Error() string {
	return fmt.Sprintf("supervisor action: %s (target=%s, reason=%s)", e.Action, e.Target, e.Reason)
}

type InjectedNode struct {
	ID        string
	NodeType  string
	DependsOn []string
}

// AgentFlowRuntime orchestrates full DAG execution with resume, idempotency,
// supervisor decision handling, and human approval integration.
type AgentFlowRuntime struct {
	store        Store
	stateMachine *StateMachine
	scheduler    *DAGScheduler
	executor     *Executor
	mapRunner    *MapRunner
	logger       *util.Logger
	tracer       trace.Tracer
	timeout      time.Duration

	injectionCount int
	injectionLimit int
	nodeLimit      int
	maxNodes       int

	nodeOutputs map[string]map[string]any
}

func NewAgentFlowRuntime(store Store, logger *util.Logger) *AgentFlowRuntime {
	return &AgentFlowRuntime{
		store:          store,
		stateMachine:   NewStateMachine(store),
		logger:         logger,
		nodeOutputs:    make(map[string]map[string]any),
		injectionLimit: 10,
	}
}

func (r *AgentFlowRuntime) SetExecutor(exec *Executor) {
	r.executor = exec
	r.mapRunner = newMapRunner(exec, r.store, r.logger)
}

func (r *AgentFlowRuntime) SetTimeout(d time.Duration) {
	r.timeout = d
}

func (r *AgentFlowRuntime) SetMaxRetries(n int) {
	r.nodeLimit = n
}

func (r *AgentFlowRuntime) SetScheduler(s *DAGScheduler) {
	r.scheduler = s
}

func (r *AgentFlowRuntime) SetInjectionLimit(n int) {
	r.injectionLimit = n
}

func (r *AgentFlowRuntime) applySupervisorConfig(spec *model.AgentFlowSpec) {
	for i := range spec.Nodes {
		n := &spec.Nodes[i]
		if n.Type == model.SupervisorNode && n.SupervisorConfig != nil {
			sc := n.SupervisorConfig
			if sc.MaxInjections > 0 {
				r.injectionLimit = sc.MaxInjections
			}
			if sc.MaxRetries > 0 && (r.nodeLimit == 0 || sc.MaxRetries < r.nodeLimit) {
				r.nodeLimit = sc.MaxRetries
			}
			if sc.MaxNodes > 0 {
				r.maxNodes = sc.MaxNodes
			}
		}
	}
}

func (r *AgentFlowRuntime) Execute(ctx context.Context, run *model.AgentFlowRun, spec *model.AgentFlowSpec) error {
	if r.tracer == nil {
		r.tracer = otel.Tracer("flowgent/engine")
	}

	ctx, span := r.tracer.Start(ctx, "agentflow.execute",
		trace.WithAttributes(
			attribute.String("agentflow.id", spec.ID),
			attribute.String("agentflow.description", spec.Description),
			attribute.String("run.id", run.ID),
			attribute.String("trigger.type", run.Trigger.Type),
			attribute.String("trigger.source", run.Trigger.Source),
			attribute.Int("node_count", len(spec.Nodes)),
			attribute.Int("edge_count", len(spec.Edges)),
		),
	)
	defer span.End()

	nodeMap := make(map[string]*model.Node)
	for i := range spec.Nodes {
		n := &spec.Nodes[i]
		nodeMap[n.ID] = n
	}

	r.applySupervisorConfig(spec)

	// Enforce supervisor max_nodes limit
	if r.maxNodes > 0 && len(spec.Nodes) > r.maxNodes {
		r.logger.Warn("supervisor max_nodes exceeded", "max", r.maxNodes, "actual", len(spec.Nodes))
	}

	var edges [][2]string
	var edgeConds []EdgeCondition
	for _, e := range spec.Edges {
		edges = append(edges, [2]string{e.From, e.To})
		edgeConds = append(edgeConds, EdgeCondition{From: e.From, To: e.To, Condition: e.Condition})
	}

	nodeIDs := make([]string, len(spec.Nodes))
	for i, n := range spec.Nodes {
		nodeIDs[i] = n.ID
	}

	scheduler := NewDAGScheduler(nodeIDs, edges)
	scheduler.SetEdgeConditions(edgeConds)
	r.scheduler = scheduler

	scope := make(map[string]map[string]any)
	if spec.Vars != nil {
		scope["vars"] = spec.Vars
	}

	run.Status = model.RunRunning
	now := time.Now()
	run.StartedAt = &now
	if err := r.store.UpdateAgentFlowRun(ctx, run); err != nil {
		return err
	}

	if r.executor != nil {
		r.executor.SetAgentFlowContext(spec.Description)
	}

	if r.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if scheduler.IsComplete() {
			run.Status = model.RunCompleted
			run.FinishedAt = ptrTime()
			run.Output = r.aggregateOutput(scope)
			span.SetStatus(codes.Ok, "agentflow completed")
			return r.store.UpdateAgentFlowRun(ctx, run)
		}

		if scheduler.HasFailed() {
			run.Status = model.RunFailed
			run.Error = "one or more nodes failed"
			run.FinishedAt = ptrTime()
			span.SetStatus(codes.Error, "node failed")
			return r.store.UpdateAgentFlowRun(ctx, run)
		}

		ready := scheduler.Ready()
		if len(ready) == 0 {
			break
		}

		for _, nodeID := range ready {
			node, ok := nodeMap[nodeID]
			if !ok {
				r.logger.Error("node not found in spec", "node", nodeID)
				continue
			}

			if err := r.executeNode(ctx, run, node, nodeMap, scheduler, scope); err != nil {
				var saErr *SupervisorActionError
				if errors.As(err, &saErr) {
					if err := r.handleSupervisorAction(ctx, saErr, scheduler, nodeMap, scope); err != nil {
						r.logger.Error("supervisor action failed", "error", err)
					}
					continue
				}

				if errors.Is(err, ErrSupervisorAbort) {
					run.Status = model.RunFailed
					run.Error = "supervisor aborted"
					run.FinishedAt = ptrTime()
					_ = r.store.UpdateAgentFlowRun(ctx, run)
					return nil
				}

				r.logger.Error("node execution failed", "node", nodeID, "error", err)
				scheduler.Fail(nodeID)
			}
		}
	}

	return nil
}

func (r *AgentFlowRuntime) executeNode(ctx context.Context, run *model.AgentFlowRun, node *model.Node, nodeMap map[string]*model.Node, scheduler *DAGScheduler, scope map[string]map[string]any) error {
	ctx, span := r.tracer.Start(ctx, "agentflow.node",
		trace.WithAttributes(
			attribute.String("node.id", node.ID),
			attribute.String("node.type", string(node.Type)),
			attribute.String("run.id", run.ID),
			attribute.String("agentflow.id", run.AgentFlowID),
		),
	)
	defer span.End()

	startTime := time.Now()

	task := &model.TaskRun{
		AgentFlowRunID: run.ID,
		NodeID:         node.ID,
		Status:         model.TaskPending,
		ExecID:         fmt.Sprintf("%s-%s-%s", run.ID, node.ID, time.Now().Format("20060102150405")),
	}

	if node.Retry != nil {
		task.MaxRetries = node.Retry.Max
	}

	if err := r.store.CreateTaskRun(ctx, task); err != nil {
		return err
	}

	existing, err := r.store.GetTaskRunByExecID(ctx, task.ExecID)
	if err == nil && existing != nil && existing.Status == model.Success {
		r.logger.Debug("idempotent replay", "task", existing.ID)
		scheduler.Done(node.ID)
		return nil
	}

	if node.Type == model.MapNode {
		if err := r.mapRunner.runMap(ctx, task, node, scope); err != nil {
			return err
		}
		if task.Output != nil {
			scope[node.ID] = task.Output
		}
		scheduler.Done(node.ID)
		return nil
	}

	retryPolicy := ModelRetry(node.Retry)
	if r.nodeLimit > 0 && retryPolicy.Max > r.nodeLimit {
		retryPolicy.Max = r.nodeLimit
	}

	execErr := RetryWithBackoff(ctx, retryPolicy, func() error {
		now := time.Now()
		task.StartedAt = &now
		task.Status = model.Running

		nodeScope := r.buildNodeScope(node.ID, scope, nodeMap)
		return r.executor.executeNode(ctx, task, node, nodeScope)
	})

	if execErr != nil {
		if task.Status == model.WaitingHuman {
			r.logger.Info("node waiting for human approval", "node", node.ID, "token", task.Output)
			span.SetAttributes(attribute.String("node.status", "waiting_human"))
			return nil
		}

		task.Status = model.Failed
		task.Error = execErr.Error()
		_ = r.store.UpdateTaskRun(ctx, task)
		scheduler.Fail(node.ID)
		span.SetStatus(codes.Error, execErr.Error())
		span.SetAttributes(
			attribute.String("node.status", "failed"),
			attribute.String("error", execErr.Error()),
			attribute.Float64("duration_ms", float64(time.Since(startTime).Milliseconds())),
		)
		return execErr
	}

	if task.Output != nil {
		scope[node.ID] = task.Output
		r.nodeOutputs[node.ID] = task.Output
	}

	if node.Type == model.ConditionNode {
		if result, ok := task.Output["result"].(bool); ok {
			scheduler.SetConditionResult(node.ID, result)
			span.SetAttributes(attribute.Bool("condition.result", result))

			for _, child := range scheduler.Children(node.ID) {
				cond := scheduler.GetChildCondition(node.ID, child)
				if cond != nil && *cond != result {
					scheduler.Skip(child)
					span.AddEvent("node skipped", trace.WithAttributes(attribute.String("skipped.node", child)))
				}
			}
		}
	}

	span.SetStatus(codes.Ok, "node completed")
	span.SetAttributes(
		attribute.String("node.status", "completed"),
		attribute.Float64("duration_ms", float64(time.Since(startTime).Milliseconds())),
	)
	scheduler.Done(node.ID)
	return nil
}

func (r *AgentFlowRuntime) buildNodeScope(nodeID string, scope map[string]map[string]any, nodeMap map[string]*model.Node) map[string]map[string]any {
	nodeScope := make(map[string]map[string]any)
	for k, v := range scope {
		nodeScope[k] = v
	}
	deps := r.scheduler.Deps(nodeID)
	for _, dep := range deps {
		if output, ok := r.nodeOutputs[dep]; ok {
			nodeScope[dep] = output
		}
	}
	return nodeScope
}

func (r *AgentFlowRuntime) handleSupervisorAction(ctx context.Context, action *SupervisorActionError, scheduler *DAGScheduler, nodeMap map[string]*model.Node, scope map[string]map[string]any) error {
	r.injectionCount++
	if r.injectionCount > r.injectionLimit {
		return fmt.Errorf("supervisor injection limit exceeded (%d)", r.injectionLimit)
	}

	switch action.Action {
	case "retry":
		r.logger.Info("supervisor retry", "target", action.Target, "reason", action.Reason)
		scheduler.Done(action.Target)
		return nil

	case "redirect":
		r.logger.Info("supervisor redirect", "target", action.Target, "reason", action.Reason)
		return nil

	case "inject":
		r.logger.Info("supervisor inject", "nodes", len(action.InjectedNodes), "reason", action.Reason)
		for _, inj := range action.InjectedNodes {
			injNode := &model.Node{
				ID:   inj.ID,
				Type: model.NodeType(inj.NodeType),
			}
			nodeMap[inj.ID] = injNode
			scheduler.Inject(inj.ID, inj.DependsOn)
			r.logger.Debug("node injected", "id", inj.ID, "type", inj.NodeType)
		}
		return nil
	}

	return nil
}

func (r *AgentFlowRuntime) aggregateOutput(scope map[string]map[string]any) map[string]any {
	output := make(map[string]any)
	for nodeID, out := range r.nodeOutputs {
		output[nodeID] = out
	}
	return output
}

func ptrTime() *time.Time {
	t := time.Now()
	return &t
}
