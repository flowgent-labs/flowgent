package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/queue"
	"github.com/flowgent-labs/flowgent/src/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type EdgeCondition struct {
	From, To  string
	Condition *bool
}

// JobManager is the control-plane orchestrator. It builds the execution graph
// (DAG state + ExecutionPlans) from an AgentFlowSpec, dispatches plans via
// the Scheduler, and monitors completion. Never executes tasks directly.
type JobManager struct {
	store     Store
	q         queue.Queue
	scheduler Scheduler
	logger    *util.Logger
	tracer    trace.Tracer
	timeout   time.Duration
	nodeLimit int
	maxNodes  int

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

	planMap     map[string]*model.ExecutionPlan
	nodeOutputs map[string]map[string]any
}

func NewJobManager(store Store, q queue.Queue, scheduler Scheduler, logger *util.Logger) *JobManager {
	return &JobManager{
		store: store, q: q, scheduler: scheduler, logger: logger,
		nodeOutputs: make(map[string]map[string]any),
	}
}

func (jm *JobManager) SetTimeout(t time.Duration) { jm.timeout = t }
func (jm *JobManager) SetNodeLimit(n int)         { jm.nodeLimit = n }

// ─── Graph state ────────────────────────────────────────

func (jm *JobManager) BuildGraphNodes(nodes []string, rawEdges [][2]string) {
	jm.mu.Lock(); defer jm.mu.Unlock()
	jm.nodes = nodes; jm.edges = rawEdges
	jm.deps = make(map[string][]string); jm.children = make(map[string][]string)
	jm.completed = make(map[string]bool); jm.skipped = make(map[string]bool)
	jm.failed = make(map[string]bool); jm.pending = make(map[string]bool)
	jm.conditions = make(map[string]bool)
	jm.nodeOutputs = make(map[string]map[string]any)
	jm.planMap = make(map[string]*model.ExecutionPlan)
	for _, n := range nodes { jm.deps[n] = []string{}; jm.children[n] = []string{}; jm.pending[n] = true }
	for _, e := range rawEdges { jm.deps[e[1]] = append(jm.deps[e[1]], e[0]); jm.children[e[0]] = append(jm.children[e[0]], e[1]) }
}

func (jm *JobManager) SetEdgeConditions(ecs []EdgeCondition) {
	jm.mu.Lock(); defer jm.mu.Unlock()
	if jm.edgeConditions == nil { jm.edgeConditions = make(map[string]*bool) }
	for _, ec := range ecs { jm.edgeConditions[ec.From+"->"+ec.To] = ec.Condition }
}
func (jm *JobManager) GetChildCondition(from, to string) *bool {
	jm.mu.Lock(); defer jm.mu.Unlock()
	if jm.edgeConditions == nil { return nil }; return jm.edgeConditions[from+"->"+to]
}
func (jm *JobManager) Ready() []string { jm.mu.Lock(); defer jm.mu.Unlock()
	var r []string
	for _, n := range jm.nodes { if !jm.completed[n] && !jm.skipped[n] && !jm.failed[n] && jm.depsDone(n) { r = append(r, n) } }
	return r
}
func (jm *JobManager) depsDone(n string) bool {
	for _, d := range jm.deps[n] { if !jm.skipped[d] && !jm.completed[d] { return false } }; return true
}
func (jm *JobManager) Done(n string)   { jm.mu.Lock(); defer jm.mu.Unlock(); jm.completed[n] = true; jm.pending[n] = false }
func (jm *JobManager) Skip(n string)   { jm.mu.Lock(); defer jm.mu.Unlock(); jm.skipped[n] = true; jm.pending[n] = false }
func (jm *JobManager) Fail(n string)   { jm.mu.Lock(); defer jm.mu.Unlock(); jm.failed[n] = true; jm.pending[n] = false }
func (jm *JobManager) IsComplete() bool { jm.mu.Lock(); defer jm.mu.Unlock()
	for _, n := range jm.nodes { if !jm.completed[n] && !jm.skipped[n] && !jm.failed[n] { return false } }; return true
}
func (jm *JobManager) HasFailed() bool { jm.mu.Lock(); defer jm.mu.Unlock()
	for _, n := range jm.nodes { if jm.failed[n] { return true } }; return false
}
func (jm *JobManager) SetConditionResult(n string, r bool) { jm.mu.Lock(); defer jm.mu.Unlock(); jm.conditions[n] = r }
func (jm *JobManager) ConditionResult(n string) (bool, bool) { jm.mu.Lock(); defer jm.mu.Unlock(); r, o := jm.conditions[n]; return r, o }
func (jm *JobManager) Inject(n string, d []string) { jm.mu.Lock(); defer jm.mu.Unlock()
	jm.nodes = append(jm.nodes, n); jm.deps[n] = d; jm.children[n] = []string{}; jm.pending[n] = true }
func (jm *JobManager) Children(n string) []string { jm.mu.Lock(); defer jm.mu.Unlock(); return jm.children[n] }
func (jm *JobManager) Deps(n string) []string     { jm.mu.Lock(); defer jm.mu.Unlock(); return jm.deps[n] }

// ─── buildExecutionGraph — single pass: DAG state + ExecutionPlans ─

func (jm *JobManager) buildExecutionGraph(spec *model.AgentFlowSpec, runID string) {
	nodeIDs := make([]string, len(spec.Nodes))
	for i, n := range spec.Nodes { nodeIDs[i] = n.ID }
	var edges [][2]string
	var edgeConds []EdgeCondition
	for _, e := range spec.Edges {
		edges = append(edges, [2]string{e.From, e.To})
		edgeConds = append(edgeConds, EdgeCondition{From: e.From, To: e.To, Condition: e.Condition})
	}

	jm.mu.Lock(); defer jm.mu.Unlock()

	// DAG state
	jm.nodes = nodeIDs; jm.edges = edges
	jm.deps = make(map[string][]string); jm.children = make(map[string][]string)
	jm.completed = make(map[string]bool); jm.skipped = make(map[string]bool)
	jm.failed = make(map[string]bool); jm.pending = make(map[string]bool)
	jm.conditions = make(map[string]bool)
	jm.nodeOutputs = make(map[string]map[string]any)
	jm.planMap = make(map[string]*model.ExecutionPlan)

	for _, n := range nodeIDs { jm.deps[n] = []string{}; jm.children[n] = []string{}; jm.pending[n] = true }
	for _, e := range edges { jm.deps[e[1]] = append(jm.deps[e[1]], e[0]); jm.children[e[0]] = append(jm.children[e[0]], e[1]) }

	if jm.edgeConditions == nil { jm.edgeConditions = make(map[string]*bool) }
	for _, ec := range edgeConds { jm.edgeConditions[ec.From+"->"+ec.To] = ec.Condition }

	// ExecutionPlans — one per node, created in same pass
	for i := range spec.Nodes {
		n := &spec.Nodes[i]
		jm.planMap[n.ID] = &model.ExecutionPlan{
			PlanID: fmt.Sprintf("plan-%s-%s", runID, n.ID), AgentFlowRunID: runID,
			TaskID: fmt.Sprintf("task-%s-%s", runID, n.ID),
			TaskType: nodeToTaskType(n.Type), NodeID: n.ID,
			State: model.TaskPending, MaxRetries: retryMax(n.Retry),
			NodeSpec: model.NodeSpecFromNode(n), CreatedAt: time.Now(),
		}
	}
}

// ─── StartJob ─────────────────────────────────────────────

func (jm *JobManager) StartJob(ctx context.Context, run *model.AgentFlowRun, spec *model.AgentFlowSpec) error {
	if jm.tracer == nil { jm.tracer = otel.Tracer("flowgent/jobmanager") }

	jm.buildExecutionGraph(spec, run.ID)
	jm.applySupervisorConfig(spec)

	ctx, span := jm.tracer.Start(ctx, "jobmanager.startjob",
		trace.WithAttributes(
			attribute.String("agentflow.id", spec.ID), attribute.String("run.id", run.ID),
			attribute.String("scheduler", string(jm.scheduler.Type())),
			attribute.Int("node_count", len(spec.Nodes)),
		),
	)
	defer span.End()

	run.Status = model.RunRunning
	now := time.Now(); run.StartedAt = &now
	_ = jm.store.UpdateAgentFlowRun(ctx, run)

	if jm.timeout > 0 { var cancel context.CancelFunc; ctx, cancel = context.WithTimeout(ctx, jm.timeout); defer cancel() }

	for {
		select {
		case <-ctx.Done(): return ctx.Err()
		default:
		}
		if jm.IsComplete() { run.Status = model.RunCompleted; run.FinishedAt = timePtr(); span.SetStatus(codes.Ok, "done"); return jm.store.UpdateAgentFlowRun(ctx, run) }
		if jm.HasFailed() { run.Status = model.RunFailed; run.Error = "node failed"; run.FinishedAt = timePtr(); span.SetStatus(codes.Error, "failed"); return jm.store.UpdateAgentFlowRun(ctx, run) }

		ready := jm.Ready()
		if len(ready) == 0 { break }

		_, _ = jm.scheduler.EnsureCapacity(ctx, len(ready))

		for _, nodeID := range ready {
			plan, ok := jm.planMap[nodeID]
			if !ok { continue }
			plan.Input = jm.resolveInput(nodeID)
			_ = jm.store.SaveExecutionPlan(ctx, plan)

			result, err := jm.scheduler.SubmitTask(ctx, plan)
			if err != nil { jm.logger.Error("submit failed", "node", nodeID, "err", err); jm.Fail(nodeID); continue }
			if result.Error != "" { jm.Fail(nodeID); continue }

			if result.Output != nil { jm.nodeOutputs[nodeID] = result.Output }
			jm.Done(nodeID)

			if plan.TaskType == model.TaskCondition {
				if r, ok := result.Output["result"].(bool); ok {
					jm.SetConditionResult(nodeID, r)
					for _, child := range jm.Children(nodeID) {
						if c := jm.GetChildCondition(nodeID, child); c != nil && *c != r { jm.Skip(child) }
					}
				}
			}
		}
	}
	return nil
}

func (jm *JobManager) resolveInput(nodeID string) map[string]any {
	in := make(map[string]any)
	for _, dep := range jm.Deps(nodeID) { if o, ok := jm.nodeOutputs[dep]; ok { in[dep] = o } }
	return in
}

func (jm *JobManager) applySupervisorConfig(spec *model.AgentFlowSpec) {
	for _, n := range spec.Nodes {
		if n.Type == model.SupervisorNode && n.SupervisorConfig != nil && n.SupervisorConfig.MaxNodes > 0 { jm.maxNodes = n.SupervisorConfig.MaxNodes }
	}
}

func nodeToTaskType(nt model.NodeType) model.TaskType {
	switch nt {
	case model.AgentNode:      return model.TaskAgent
	case model.ToolNode:       return model.TaskTool
	case model.ConditionNode:  return model.TaskCondition
	case model.TribunalNode:   return model.TaskTribunal
	case model.SupervisorNode: return model.TaskSupervisor
	case model.MapNode:        return model.TaskMap
	case model.HumanNode:      return model.TaskHuman
	case model.AgentFlowNode:  return model.TaskSubflow
	default:                   return model.TaskNoop
	}
}

func retryMax(r *model.RetryPolicy) int { if r == nil { return 3 }; return r.Max }
func timePtr() *time.Time { t := time.Now(); return &t }
