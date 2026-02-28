package jobmanager

import (
	"github.com/flowgent-labs/flowgent/src/engine/resourcemanager"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/src/common/tracing"
	"go.opentelemetry.io/otel/metric"
	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/store"
	"github.com/flowgent-labs/flowgent/src/common/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type EdgeCondition struct {
	From, To  string
	Condition *bool
}

// JobMaster is the per-run DAG orchestrator. It builds the execution graph
// from an AgentFlowSpec and dispatches plans via resourcemanager.ResourceManager.Schedule().
// Each agentflow run gets its own JobMaster instance — no shared state.
type JobMaster struct {
	store     store.Store
	rm        resourcemanager.ResourceManager
	logger    *utils.Logger
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
	nodeErrors     map[string]string // nodeID → error message
	pending        map[string]bool
	conditions     map[string]bool

	planMap     map[string]*model.ExecutionPlan
	nodeOutputs map[string]map[string]any
}

// NewJobMaster creates a per-run JobMaster. Config is read internally for timeout and retry limits.
func NewJobMaster(store store.Store, rm resourcemanager.ResourceManager, logger *utils.Logger, cfg *JobManagerConfig) *JobMaster {
	timeout := cfg.FlowExecutionTimeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	return &JobMaster{
		store: store, rm: rm, logger: logger,
		timeout: timeout, nodeLimit: cfg.MaxNodeRetries,
		nodeOutputs: make(map[string]map[string]any),
	}
}

// ─── Graph state ────────────────────────────────────────

func (jm *JobMaster) BuildGraphNodes(nodes []string, rawEdges [][2]string) {
	jm.mu.Lock(); defer jm.mu.Unlock()
	jm.nodes = nodes; jm.edges = rawEdges
	jm.deps = make(map[string][]string); jm.children = make(map[string][]string)
	jm.completed = make(map[string]bool); jm.skipped = make(map[string]bool)
	jm.failed = make(map[string]bool); jm.pending = make(map[string]bool)
	jm.conditions = make(map[string]bool); jm.nodeErrors = make(map[string]string)
	jm.nodeOutputs = make(map[string]map[string]any)
	jm.planMap = make(map[string]*model.ExecutionPlan)
	for _, n := range nodes { jm.deps[n] = []string{}; jm.children[n] = []string{}; jm.pending[n] = true }
	for _, e := range rawEdges { jm.deps[e[1]] = append(jm.deps[e[1]], e[0]); jm.children[e[0]] = append(jm.children[e[0]], e[1]) }
}

func (jm *JobMaster) SetEdgeConditions(ecs []EdgeCondition) {
	jm.mu.Lock(); defer jm.mu.Unlock()
	if jm.edgeConditions == nil { jm.edgeConditions = make(map[string]*bool) }
	for _, ec := range ecs { jm.edgeConditions[ec.From+"->"+ec.To] = ec.Condition }
}
func (jm *JobMaster) GetChildCondition(from, to string) *bool {
	jm.mu.Lock(); defer jm.mu.Unlock()
	if jm.edgeConditions == nil { return nil }; return jm.edgeConditions[from+"->"+to]
}
func (jm *JobMaster) Ready() []string { jm.mu.Lock(); defer jm.mu.Unlock()
	var r []string
	for _, n := range jm.nodes { if !jm.completed[n] && !jm.skipped[n] && !jm.failed[n] && jm.depsDone(n) { r = append(r, n) } }
	return r
}
func (jm *JobMaster) depsDone(n string) bool {
	for _, d := range jm.deps[n] { if !jm.skipped[d] && !jm.completed[d] { return false } }; return true
}
func (jm *JobMaster) Done(n string)   { jm.mu.Lock(); defer jm.mu.Unlock(); jm.completed[n] = true; jm.pending[n] = false }
func (jm *JobMaster) Skip(n string)   { jm.mu.Lock(); defer jm.mu.Unlock(); jm.skipped[n] = true; jm.pending[n] = false }
func (jm *JobMaster) Fail(n string)   { jm.mu.Lock(); defer jm.mu.Unlock(); jm.failed[n] = true; jm.pending[n] = false }
func (jm *JobMaster) IsComplete() bool { jm.mu.Lock(); defer jm.mu.Unlock()
	for _, n := range jm.nodes { if !jm.completed[n] && !jm.skipped[n] && !jm.failed[n] { return false } }; return true
}
func (jm *JobMaster) HasFailed() bool { jm.mu.Lock(); defer jm.mu.Unlock()
	for _, n := range jm.nodes { if jm.failed[n] { return true } }; return false
}
func (jm *JobMaster) SetConditionResult(n string, r bool) { jm.mu.Lock(); defer jm.mu.Unlock(); jm.conditions[n] = r }
func (jm *JobMaster) ConditionResult(n string) (bool, bool) { jm.mu.Lock(); defer jm.mu.Unlock(); r, o := jm.conditions[n]; return r, o }
func (jm *JobMaster) Inject(n string, d []string) { jm.mu.Lock(); defer jm.mu.Unlock()
	jm.nodes = append(jm.nodes, n); jm.deps[n] = d; jm.children[n] = []string{}; jm.pending[n] = true }
func (jm *JobMaster) Children(n string) []string { jm.mu.Lock(); defer jm.mu.Unlock(); return jm.children[n] }
func (jm *JobMaster) Deps(n string) []string     { jm.mu.Lock(); defer jm.mu.Unlock(); return jm.deps[n] }

// ─── buildExecutionGraph — single pass: DAG state + ExecutionPlans ─

func (jm *JobMaster) buildExecutionGraph(spec *model.AgentFlowSpec, runID string) {
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
	jm.conditions = make(map[string]bool); jm.nodeErrors = make(map[string]string)
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
			PlanID:               fmt.Sprintf("plan-%s-%s", runID, n.ID),
			AgentFlowRunID:       runID,
			AgentFlowDefinitionID: spec.ID,
			TenantID:             spec.TenantID,
			TaskID:               fmt.Sprintf("task-%s-%s", runID, n.ID),
			TaskType:             NodeToTaskType(n.Type), NodeID: n.ID,
			State:                model.TaskPending, MaxRetries: RetryMax(n.Retry),
			NodeSpec:             model.NodeSpecFromNode(n), CreatedAt: time.Now(),
		}
	}
}

// ─── StartJob ─────────────────────────────────────────────

func (jm *JobMaster) Execute(ctx context.Context, run *model.AgentFlowRun, spec *model.AgentFlowSpec) error {
	if jm.tracer == nil { jm.tracer = tracing.Tracer("flowgent/jobmaster") }

	jm.buildExecutionGraph(spec, run.ID)
	jm.applySupervisorConfig(spec)

	ctx, span := jm.tracer.Start(ctx, "jobmaster.execute",
		trace.WithAttributes(
			attribute.String("agentflow.id", spec.ID), attribute.String("run.id", run.ID),
			attribute.String("resource_manager", string(jm.rm.Provider())),
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
		if jm.HasFailed() { run.Status = model.RunFailed; run.Error = jm.collectFirstError(); run.FinishedAt = TimePtr(); span.SetStatus(codes.Error, "failed"); return jm.store.UpdateAgentFlowRun(ctx, run) }
		if jm.IsComplete() { run.Status = model.RunCompleted; run.FinishedAt = TimePtr(); span.SetStatus(codes.Ok, "done"); return jm.store.UpdateAgentFlowRun(ctx, run) }

		ready := jm.Ready()
		if len(ready) == 0 { break }


		for _, nodeID := range ready {
			plan, ok := jm.planMap[nodeID]
			if !ok { continue }
			plan.Input = jm.resolveInput(nodeID, plan.NodeSpec.RawInput)
			_ = jm.store.SaveExecutionPlan(ctx, plan)

			result, err := jm.rm.Schedule(ctx, plan)
			if err != nil { jm.logger.Error("submit failed", "node", nodeID, "err", err); jm.nodeErrors[nodeID] = err.Error(); jm.Fail(nodeID); continue }
			if result.Error != "" { jm.logger.Error("node execution failed", "node", nodeID, "err", result.Error); jm.nodeErrors[nodeID] = result.Error; jm.Fail(nodeID); continue }

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

func (jm *JobMaster) resolveInput(nodeID string, yamlInput map[string]any) map[string]any {
	in := make(map[string]any)
	// Start with YAML-defined input (action, params, etc.)
	for k, v := range yamlInput {
		in[k] = v
	}
	// Merge upstream node outputs (overrides YAML if same key)
	for _, dep := range jm.Deps(nodeID) {
		if o, ok := jm.nodeOutputs[dep]; ok {
			in[dep] = o
		}
	}
	return in
}

func (jm *JobMaster) collectFirstError() string {
	for _, err := range jm.nodeErrors { return err }
	return "node failed"
}

func (jm *JobMaster) applySupervisorConfig(spec *model.AgentFlowSpec) {
	for _, n := range spec.Nodes {
		if n.Type == model.SupervisorNode && n.SupervisorConfig != nil && n.SupervisorConfig.MaxNodes > 0 { jm.maxNodes = n.SupervisorConfig.MaxNodes }
	}
}

func NodeToTaskType(nt model.NodeType) model.TaskType {
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

func RetryMax(r *model.RetryPolicy) int { if r == nil { return 3 }; return r.Max }
func TimePtr() *time.Time { t := time.Now(); return &t }

type RetryPolicy struct {
	Max      int
	Initial  time.Duration
	MaxDelay time.Duration
	Factor   float64
}

// ModelRetry converts model.RetryPolicy to engine RetryPolicy.
func ModelRetry(r *model.RetryPolicy) RetryPolicy {
	if r == nil {
		return RetryPolicy{Max: 3, Initial: time.Second, MaxDelay: 30 * time.Second, Factor: 2.0}
	}
	rp := RetryPolicy{Max: r.Max, Initial: r.Initial, MaxDelay: r.MaxDelay, Factor: r.Factor}
	if rp.Max <= 0 {
		rp.Max = 3
	}
	if rp.Initial <= 0 {
		rp.Initial = time.Second
	}
	if rp.MaxDelay <= 0 {
		rp.MaxDelay = 30 * time.Second
	}
	if rp.Factor <= 0 {
		rp.Factor = 2.0
	}
	return rp
}

func RetryWithBackoff(ctx context.Context, policy RetryPolicy, fn func() error) error {
	var err error
	delay := policy.Initial
	for i := 0; i <= policy.Max; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if i > 0 {
			time.Sleep(delay)
			delay = time.Duration(float64(delay) * policy.Factor)
			if delay > policy.MaxDelay {
				delay = policy.MaxDelay
			}
		}
		err = fn()
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("retry exhausted after %d attempts: %w", policy.Max, err)
}

type JobManagerMetrics struct {
	meter            metric.Meter
	RunsTotal        metric.Int64Counter
	RunsActive       metric.Int64UpDownCounter
	PlansDispatched  metric.Int64Counter
	PlansCompleted   metric.Int64Counter
	FailoverEvents   metric.Int64Counter
	DAGBuildDuration metric.Float64Histogram
}

func NewJobMasterMetrics() *JobManagerMetrics {
	m := &JobManagerMetrics{meter: tracing.Meter("flowgent/jobmaster")}

	m.RunsTotal, _ = m.meter.Int64Counter("flowgent.jm.runs.total",
		metric.WithDescription("Total agentflow runs started/finished"))
	m.RunsActive, _ = m.meter.Int64UpDownCounter("flowgent.jm.runs.active",
		metric.WithDescription("Currently running agentflows"))
	m.PlansDispatched, _ = m.meter.Int64Counter("flowgent.jm.plans.dispatched",
		metric.WithDescription("ExecutionPlans enqueued to queue"))
	m.PlansCompleted, _ = m.meter.Int64Counter("flowgent.jm.plans.completed",
		metric.WithDescription("Plans that reached terminal state"))
	m.FailoverEvents, _ = m.meter.Int64Counter("flowgent.jm.failover.events",
		metric.WithDescription("TM failure → plan requeue events"))
	m.DAGBuildDuration, _ = m.meter.Float64Histogram("flowgent.jm.dag.build.duration",
		metric.WithDescription("DAG parsing + plan creation time (ms)"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 50, 100, 500))
	return m
}

func (m *JobManagerMetrics) RecordRunStarted(ctx context.Context, agentFlowID string) {
	m.RunsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("agentflow_id", agentFlowID), attribute.String("status", "RUNNING")))
	m.RunsActive.Add(ctx, 1)
}

func (m *JobManagerMetrics) RecordRunCompleted(ctx context.Context, agentFlowID, status string) {
	m.RunsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("agentflow_id", agentFlowID), attribute.String("status", status)))
	m.RunsActive.Add(ctx, -1)
}

func (m *JobManagerMetrics) RecordPlanDispatched(ctx context.Context, agentFlowID string) {
	m.PlansDispatched.Add(ctx, 1, metric.WithAttributes(attribute.String("agentflow_id", agentFlowID)))
}

func (m *JobManagerMetrics) RecordPlanCompleted(ctx context.Context, agentFlowID, status string) {
	m.PlansCompleted.Add(ctx, 1, metric.WithAttributes(attribute.String("agentflow_id", agentFlowID), attribute.String("status", status)))
}

func (m *JobManagerMetrics) RecordFailover(ctx context.Context, tmID string) {
	m.FailoverEvents.Add(ctx, 1, metric.WithAttributes(attribute.String("tm_id", tmID)))
}

func (m *JobManagerMetrics) RecordDAGBuild(ctx context.Context, agentFlowID string, dur time.Duration) {
	m.DAGBuildDuration.Record(ctx, float64(dur.Milliseconds()), metric.WithAttributes(attribute.String("agentflow_id", agentFlowID)))
}
