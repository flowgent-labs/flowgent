package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

// SupervisorActionError is returned by supervisor node actions that require
// JobManager-level handling (retry, redirect, inject, abort).
type SupervisorActionError struct {
	Action        string
	Target        string
	Reason        string
	InjectedNodes []InjectedNode
}

func (e *SupervisorActionError) Error() string {
	return fmt.Sprintf("supervisor action: %s (target=%s, reason=%s)", e.Action, e.Target, e.Reason)
}

// InjectedNode describes a node to be dynamically injected by a supervisor.
type InjectedNode struct {
	ID        string
	NodeType  string
	DependsOn []string
}

// EdgeCondition stores an edge-level condition for routing.
type EdgeCondition struct {
	From      string
	To        string
	Condition *bool
}

// SchedulerType identifies the scheduling backend.
type SchedulerType string

const (
	SchedulerTypeStandalone SchedulerType = "standalone"
	SchedulerTypeK8s        SchedulerType = "k8s"
)

// TaskSubmit carries the parameters needed for a single node execution.
// In Flink terms, this is the Task that the JobManager submits to a TaskManager.
// The ClusterID identifies the resource pool for all tasks of a given job.
type TaskSubmit struct {
	ClusterID   string                       `json:"cluster_id"`
	AgentFlowID string                       `json:"agentflow_id"`
	RunID       string                       `json:"run_id"`
	NodeID      string                       `json:"node_id"`
	TaskID      string                       `json:"task_id"`
	Node        *model.Node                  `json:"node"`
	Input       map[string]any               `json:"input"`
}

// TaskResult carries the outcome of a submitted task.
type TaskResult struct {
	Output map[string]any `json:"output,omitempty"`
	Error  string         `json:"error,omitempty"`
}

// Scheduler is the abstraction for dispatching tasks to TaskManagers.
// Inspired by Flink's pluggable scheduler (Standalone, Kubernetes, YARN).
//
// Implementations:
//   - StandaloneScheduler — goroutine pool, for dev/test/all-in-one mode
//   - K8sScheduler       — Kubernetes pod per task, for production distributed mode
type Scheduler interface {
	Type() SchedulerType
	SubmitTask(ctx context.Context, submit *TaskSubmit) (*TaskResult, error)
	Close() error
}

// NewScheduler creates a Scheduler by type.
func NewScheduler(schedType SchedulerType, tm *TaskManager, poolSize int) (Scheduler, error) {
	switch schedType {
	case SchedulerTypeStandalone:
		return NewStandaloneScheduler(tm, poolSize), nil
	case SchedulerTypeK8s:
		return NewK8sScheduler(), nil
	default:
		return nil, fmt.Errorf("unknown scheduler type: %s", schedType)
	}
}

// JobManager is the master orchestrator for a single agent flow execution.
// It corresponds to a Flink JobManager:
//   - Builds the DAG execution graph from an AgentFlowSpec
//   - Drives the topological execution loop
//   - Dispatches individual node tasks to a Scheduler (→ TaskManager)
//   - Handles condition routing, supervisor actions, and completion
//   - Each StartJob call creates a ClusterID for resource tracking
//
// The execution loop is:
//
//	Ready nodes → SubmitTask via Scheduler → Collect results → Mark done → Repeat
type JobManager struct {
	mu             sync.Mutex
	nodes          []string
	edges          [][2]string
	edgeConditions map[string]*bool
	deps           map[string][]string
	children       map[string][]string
	completed      map[string]bool
	skipped        map[string]bool
	failed         map[string]bool
	pending        map[string]bool
	conditions     map[string]bool
	injected       map[string][]string

	store       Store
	scheduler   Scheduler
	taskManager *TaskManager // used for inline map node execution
	logger      *util.Logger
	tracer      trace.Tracer

	timeout        time.Duration
	injectionLimit int
	injectionCount int
	nodeLimit      int
	maxNodes       int

	nodeOutputs map[string]map[string]any
}

// NewJobManager creates a JobManager with the given store, scheduler, and logger.
func NewJobManager(store Store, scheduler Scheduler, logger *util.Logger) *JobManager {
	return &JobManager{
		store:          store,
		scheduler:      scheduler,
		logger:         logger,
		deps:           make(map[string][]string),
		children:       make(map[string][]string),
		completed:      make(map[string]bool),
		skipped:        make(map[string]bool),
		failed:         make(map[string]bool),
		pending:        make(map[string]bool),
		conditions:     make(map[string]bool),
		injected:       make(map[string][]string),
		nodeOutputs:    make(map[string]map[string]any),
		injectionLimit: 10,
	}
}

// NewJobManagerFromSpec creates a JobManager pre-loaded with DAG state from a spec.
func NewJobManagerFromSpec(spec *model.AgentFlowSpec, store Store, scheduler Scheduler, logger *util.Logger) *JobManager {
	jm := NewJobManager(store, scheduler, logger)
	jm.buildGraph(spec)
	return jm
}

// buildGraph initializes the DAG from the spec's nodes and edges.
func (jm *JobManager) buildGraph(spec *model.AgentFlowSpec) {
	nodeIDs := make([]string, len(spec.Nodes))
	for i, n := range spec.Nodes {
		nodeIDs[i] = n.ID
	}
	var edges [][2]string
	var edgeConds []EdgeCondition
	for _, e := range spec.Edges {
		edges = append(edges, [2]string{e.From, e.To})
		edgeConds = append(edgeConds, EdgeCondition{From: e.From, To: e.To, Condition: e.Condition})
	}
	jm.BuildGraphNodes(nodeIDs, edges)
	jm.SetEdgeConditions(edgeConds)
}

// BuildGraphNodes initializes DAG state from raw node and edge lists.
// Exported for testing; use buildGraph in production.
func (jm *JobManager) BuildGraphNodes(nodes []string, rawEdges [][2]string) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	jm.nodes = nodes
	jm.edges = rawEdges

	jm.deps = make(map[string][]string)
	jm.children = make(map[string][]string)
	jm.completed = make(map[string]bool)
	jm.skipped = make(map[string]bool)
	jm.failed = make(map[string]bool)
	jm.pending = make(map[string]bool)
	jm.conditions = make(map[string]bool)
	jm.injected = make(map[string][]string)
	jm.nodeOutputs = make(map[string]map[string]any)
	jm.injectionCount = 0

	for _, n := range nodes {
		jm.deps[n] = []string{}
		jm.children[n] = []string{}
		jm.pending[n] = true
	}
	for _, e := range rawEdges {
		jm.deps[e[1]] = append(jm.deps[e[1]], e[0])
		jm.children[e[0]] = append(jm.children[e[0]], e[1])
	}
}

// ─── DAG state methods ──────────────────────────────────

func (jm *JobManager) SetEdgeConditions(ecs []EdgeCondition) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	if jm.edgeConditions == nil {
		jm.edgeConditions = make(map[string]*bool)
	}
	for _, ec := range ecs {
		key := ec.From + "->" + ec.To
		jm.edgeConditions[key] = ec.Condition
	}
}

func (jm *JobManager) GetChildCondition(from, to string) *bool {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	if jm.edgeConditions == nil {
		return nil
	}
	key := from + "->" + to
	return jm.edgeConditions[key]
}

func (jm *JobManager) SetTimeout(t time.Duration)      { jm.timeout = t }
func (jm *JobManager) SetNodeLimit(n int)               { jm.nodeLimit = n }
func (jm *JobManager) SetInjectionLimit(n int)          { jm.injectionLimit = n }
func (jm *JobManager) SetTaskManager(tm *TaskManager)   { jm.taskManager = tm }

func (jm *JobManager) Ready() []string {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	var ready []string
	for _, n := range jm.nodes {
		if jm.completed[n] || jm.skipped[n] || jm.failed[n] {
			continue
		}
		if jm.allDepsDone(n) {
			ready = append(ready, n)
		}
	}
	return ready
}

func (jm *JobManager) allDepsDone(node string) bool {
	deps := jm.deps[node]
	if injDeps, ok := jm.injected[node]; ok {
		deps = append(deps, injDeps...)
	}
	for _, dep := range deps {
		if jm.skipped[dep] {
			continue
		}
		if !jm.completed[dep] {
			return false
		}
	}
	return true
}

func (jm *JobManager) Done(node string) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	jm.completed[node] = true
	jm.pending[node] = false
}

func (jm *JobManager) Skip(node string) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	jm.skipped[node] = true
	jm.pending[node] = false
}

func (jm *JobManager) Fail(node string) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	jm.failed[node] = true
	jm.pending[node] = false
}

func (jm *JobManager) IsComplete() bool {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	for _, n := range jm.nodes {
		if !jm.completed[n] && !jm.skipped[n] && !jm.failed[n] {
			return false
		}
	}
	return true
}

func (jm *JobManager) HasFailed() bool {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	for _, n := range jm.nodes {
		if jm.failed[n] {
			return true
		}
	}
	return false
}

func (jm *JobManager) SetConditionResult(node string, result bool) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	jm.conditions[node] = result
}

func (jm *JobManager) ConditionResult(node string) (bool, bool) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	r, ok := jm.conditions[node]
	return r, ok
}

func (jm *JobManager) Inject(node string, dependsOn []string) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	jm.injected[node] = dependsOn
	jm.nodes = append(jm.nodes, node)
	jm.deps[node] = dependsOn
	jm.children[node] = []string{}
	jm.pending[node] = true
}

func (jm *JobManager) Children(node string) []string {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	return jm.children[node]
}

func (jm *JobManager) Deps(node string) []string {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	return jm.deps[node]
}

// ─── Execution ──────────────────────────────────────────

// StartJob drives full DAG execution for the given run and spec.
// It generates a ClusterID for this job's resource pool.
func (jm *JobManager) StartJob(ctx context.Context, run *model.AgentFlowRun, spec *model.AgentFlowSpec) error {
	if jm.tracer == nil {
		jm.tracer = otel.Tracer("flowgent/jobmanager")
	}

	jm.buildGraph(spec)

	clusterID := fmt.Sprintf("cluster-%s-%s", run.AgentFlowID, run.ID)
	ctx, span := jm.tracer.Start(ctx, "jobmanager.startjob",
		trace.WithAttributes(
			attribute.String("agentflow.id", spec.ID),
			attribute.String("agentflow.description", spec.Description),
			attribute.String("run.id", run.ID),
			attribute.String("cluster.id", clusterID),
			attribute.String("scheduler.type", string(jm.scheduler.Type())),
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

	jm.applySupervisorConfig(spec)
	if jm.maxNodes > 0 && len(spec.Nodes) > jm.maxNodes {
		jm.logger.Warn("supervisor max_nodes exceeded", "max", jm.maxNodes, "actual", len(spec.Nodes))
	}

	scope := make(map[string]map[string]any)
	if spec.Vars != nil {
		scope["vars"] = spec.Vars
	}

	run.Status = model.RunRunning
	now := time.Now()
	run.StartedAt = &now
	if err := jm.store.UpdateAgentFlowRun(ctx, run); err != nil {
		return err
	}

	if jm.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, jm.timeout)
		defer cancel()
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if jm.IsComplete() {
			run.Status = model.RunCompleted
			run.FinishedAt = ptrTime()
			run.Output = jm.aggregateOutput()
			span.SetStatus(codes.Ok, "job completed")
			return jm.store.UpdateAgentFlowRun(ctx, run)
		}

		if jm.HasFailed() {
			run.Status = model.RunFailed
			run.Error = "one or more nodes failed"
			run.FinishedAt = ptrTime()
			span.SetStatus(codes.Error, "node failed")
			return jm.store.UpdateAgentFlowRun(ctx, run)
		}

		ready := jm.Ready()
		if len(ready) == 0 {
			break
		}

		for _, nodeID := range ready {
			node, ok := nodeMap[nodeID]
			if !ok {
				jm.logger.Error("node not found in spec", "node", nodeID)
				continue
			}

			if err := jm.executeNode(ctx, clusterID, run, node, nodeMap, scope); err != nil {
				var saErr *SupervisorActionError
				if errors.As(err, &saErr) {
					if err := jm.handleSupervisorAction(ctx, saErr, nodeMap, scope); err != nil {
						jm.logger.Error("supervisor action failed", "error", err)
					}
					continue
				}

				if errors.Is(err, ErrSupervisorAbort) {
					run.Status = model.RunFailed
					run.Error = "supervisor aborted"
					run.FinishedAt = ptrTime()
					_ = jm.store.UpdateAgentFlowRun(ctx, run)
					return nil
				}

				jm.logger.Error("node execution failed", "node", nodeID, "error", err)
				jm.Fail(nodeID)
			}
		}
	}

	return nil
}

func (jm *JobManager) executeNode(ctx context.Context, clusterID string, run *model.AgentFlowRun, node *model.Node, nodeMap map[string]*model.Node, scope map[string]map[string]any) error {
	ctx, span := jm.tracer.Start(ctx, "jobmanager.node",
		trace.WithAttributes(
			attribute.String("node.id", node.ID),
			attribute.String("node.type", string(node.Type)),
			attribute.String("run.id", run.ID),
			attribute.String("cluster.id", clusterID),
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

	if err := jm.store.CreateTaskRun(ctx, task); err != nil {
		return err
	}

	existing, err := jm.store.GetTaskRunByExecID(ctx, task.ExecID)
	if err == nil && existing != nil && existing.Status == model.Success {
		jm.logger.Debug("idempotent replay", "task", existing.ID)
		jm.Done(node.ID)
		return nil
	}

	// Map nodes are handled inline — each child dispatched directly to TaskManager
	if node.Type == model.MapNode {
		mapRunner := newMapRunner(jm.store, jm.logger)
		if err := mapRunner.runMap(ctx, task, node, scope, jm.taskManager); err != nil {
			return err
		}
		if task.Output != nil {
			scope[node.ID] = task.Output
		}
		jm.Done(node.ID)
		return nil
	}

	// Resolve input from upstream node outputs
	nodeScope := jm.buildNodeScope(node.ID, scope, nodeMap)

	submit := &TaskSubmit{
		ClusterID:   clusterID,
		AgentFlowID: run.AgentFlowID,
		RunID:       run.ID,
		NodeID:      node.ID,
		TaskID:      task.ID,
		Node:        node,
		Input:       nodeScope["input"],
	}

	retryPolicy := ModelRetry(node.Retry)
	if jm.nodeLimit > 0 && retryPolicy.Max > jm.nodeLimit {
		retryPolicy.Max = jm.nodeLimit
	}

	var result *TaskResult
	submitErr := RetryWithBackoff(ctx, retryPolicy, func() error {
		r, err := jm.scheduler.SubmitTask(ctx, submit)
		if err != nil {
			return err
		}
		result = r
		if r.Error != "" {
			return fmt.Errorf("%s", r.Error)
		}
		return nil
	})

	if submitErr != nil {
		if task.Status == model.WaitingHuman {
			jm.logger.Info("node waiting for human approval", "node", node.ID, "token", task.Output)
			span.SetAttributes(attribute.String("node.status", "waiting_human"))
			return nil
		}

		task.Status = model.Failed
		task.Error = submitErr.Error()
		_ = jm.store.UpdateTaskRun(ctx, task)
		jm.Fail(node.ID)
		span.SetStatus(codes.Error, submitErr.Error())
		span.SetAttributes(
			attribute.String("node.status", "failed"),
			attribute.Float64("duration_ms", float64(time.Since(startTime).Milliseconds())),
		)
		return submitErr
	}

	if result != nil && result.Output != nil {
		scope[node.ID] = result.Output
		jm.nodeOutputs[node.ID] = result.Output
	}

	if node.Type == model.ConditionNode {
		if result != nil {
			if r, ok := result.Output["result"].(bool); ok {
				jm.SetConditionResult(node.ID, r)
				span.SetAttributes(attribute.Bool("condition.result", r))
				for _, child := range jm.Children(node.ID) {
					cond := jm.GetChildCondition(node.ID, child)
					if cond != nil && *cond != r {
						jm.Skip(child)
						span.AddEvent("node skipped", trace.WithAttributes(attribute.String("skipped.node", child)))
					}
				}
			}
		}
	}

	span.SetStatus(codes.Ok, "node completed")
	span.SetAttributes(
		attribute.String("node.status", "completed"),
		attribute.Float64("duration_ms", float64(time.Since(startTime).Milliseconds())),
	)
	jm.Done(node.ID)
	return nil
}

func (jm *JobManager) buildNodeScope(nodeID string, scope map[string]map[string]any, nodeMap map[string]*model.Node) map[string]map[string]any {
	nodeScope := make(map[string]map[string]any)
	for k, v := range scope {
		nodeScope[k] = v
	}
	deps := jm.Deps(nodeID)
	for _, dep := range deps {
		if output, ok := jm.nodeOutputs[dep]; ok {
			nodeScope[dep] = output
		}
	}
	return nodeScope
}

func (jm *JobManager) handleSupervisorAction(ctx context.Context, action *SupervisorActionError, nodeMap map[string]*model.Node, scope map[string]map[string]any) error {
	jm.injectionCount++
	if jm.injectionCount > jm.injectionLimit {
		return fmt.Errorf("supervisor injection limit exceeded (%d)", jm.injectionLimit)
	}

	switch action.Action {
	case "retry":
		jm.logger.Info("supervisor retry", "target", action.Target, "reason", action.Reason)
		jm.Done(action.Target)
		return nil
	case "redirect":
		jm.logger.Info("supervisor redirect", "target", action.Target, "reason", action.Reason)
		return nil
	case "inject":
		jm.logger.Info("supervisor inject", "nodes", len(action.InjectedNodes), "reason", action.Reason)
		for _, inj := range action.InjectedNodes {
			injNode := &model.Node{
				ID:   inj.ID,
				Type: model.NodeType(inj.NodeType),
			}
			nodeMap[inj.ID] = injNode
			jm.Inject(inj.ID, inj.DependsOn)
			jm.logger.Debug("node injected", "id", inj.ID, "type", inj.NodeType)
		}
		return nil
	}
	return nil
}

func (jm *JobManager) applySupervisorConfig(spec *model.AgentFlowSpec) {
	for i := range spec.Nodes {
		n := &spec.Nodes[i]
		if n.Type == model.SupervisorNode && n.SupervisorConfig != nil {
			sc := n.SupervisorConfig
			if sc.MaxInjections > 0 {
				jm.injectionLimit = sc.MaxInjections
			}
			if sc.MaxRetries > 0 && (jm.nodeLimit == 0 || sc.MaxRetries < jm.nodeLimit) {
				jm.nodeLimit = sc.MaxRetries
			}
			if sc.MaxNodes > 0 {
				jm.maxNodes = sc.MaxNodes
			}
		}
	}
}

func (jm *JobManager) aggregateOutput() map[string]any {
	output := make(map[string]any)
	for nodeID, out := range jm.nodeOutputs {
		output[nodeID] = out
	}
	return output
}

func ptrTime() *time.Time {
	t := time.Now()
	return &t
}
