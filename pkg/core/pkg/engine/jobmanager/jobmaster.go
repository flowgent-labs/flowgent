package jobmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// RunStateStore is the narrow state interface JobMaster needs for persistence.
// Implementations call the apiserver REST API (never direct DB).
type RunStateStore interface {
	UpdateRun(ctx context.Context, run *entities.FlowRunInfo) error
	SaveTask(ctx context.Context, task *entities.TaskRunInfo) error
}

// KnowledgePostWriter persists knowledge entries after a run completes.
// The engine uses this via a REST-client adapter — never a direct store import.
type KnowledgePostWriter interface {
	CreateKnowledge(ctx context.Context, tenant string, entry *entities.KnowledgeEntry) (*entities.KnowledgeEntry, error)
}

type EdgeCondition struct {
	From, To  string
	Condition *bool
}

// JobMaster is the per-run DAG orchestrator. It builds the execution graph
// from an FlowInfo and dispatches plans via resourcemanager.ResourceManager.Schedule().
// Each agentflow run gets its own JobMaster instance — no shared state.
type JobMaster struct {
	state   RunStateStore
	rm      resourcemanager.ResourceManager
	logger  *utils.Logger
	tracer  trace.Tracer
	timeout time.Duration
	nodeLimit int
	maxNodes  int

	knowledgeWriter KnowledgePostWriter

	mu             sync.Mutex
	nodes          []string
	edges          [][2]string
	edgeConditions map[string]*bool
	deps           map[string][]string
	children       map[string][]string
	completed      map[string]bool
	skipped        map[string]bool
	failed         map[string]bool
	nodeErrors     map[string]string
	pending        map[string]bool
	conditions     map[string]bool

	planMap     map[string]*entities.ExecutionPlan
	nodeOutputs map[string]map[string]any

	resolvedVars map[string]any
}

// NewJobMaster creates a per-run JobMaster. Config is read internally for timeout and retry limits.
func NewJobMaster(state RunStateStore, rm resourcemanager.ResourceManager, logger *utils.Logger, cfg *JobManagerConfig) *JobMaster {
	timeout := cfg.FlowExecutionTimeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}

	return &JobMaster{
		state: state, rm: rm, logger: logger,
		timeout: timeout, nodeLimit: cfg.MaxNodeRetries,
		nodeOutputs: make(map[string]map[string]any),
	}
}

// SetKnowledgeWriter configures the post-run knowledge extraction writer.
func (jm *JobMaster) SetKnowledgeWriter(w KnowledgePostWriter) { jm.knowledgeWriter = w }

// ─── Graph state ────────────────────────────────────────

func (jm *JobMaster) BuildGraphNodes(nodes []string, rawEdges [][2]string) {
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
	jm.nodeErrors = make(map[string]string)
	jm.nodeOutputs = make(map[string]map[string]any)
	jm.planMap = make(map[string]*entities.ExecutionPlan)
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

func (jm *JobMaster) SetEdgeConditions(ecs []EdgeCondition) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	if jm.edgeConditions == nil {
		jm.edgeConditions = make(map[string]*bool)
	}
	for _, ec := range ecs {
		jm.edgeConditions[ec.From+"->"+ec.To] = ec.Condition
	}
}
func (jm *JobMaster) GetChildCondition(from, to string) *bool {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	if jm.edgeConditions == nil {
		return nil
	}
	return jm.edgeConditions[from+"->"+to]
}
func (jm *JobMaster) Ready() []string {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	var r []string
	for _, n := range jm.nodes {
		if !jm.completed[n] && !jm.skipped[n] && !jm.failed[n] && jm.depsDone(n) {
			r = append(r, n)
		}
	}
	return r
}
func (jm *JobMaster) depsDone(n string) bool {
	anySatisfied := len(jm.deps[n]) == 0
	for _, d := range jm.deps[n] {
		if jm.skipped[d] || jm.completed[d] {
			anySatisfied = true
			continue
		}
		if c, exists := jm.edgeConditions[d+"->"+n]; exists && c != nil {
			continue // dormant conditional edge (source not yet evaluated)
		}
		return false
	}
	return anySatisfied
}
func (jm *JobMaster) Done(n string) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	jm.completed[n] = true
	jm.pending[n] = false
}
func (jm *JobMaster) Skip(n string) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	jm.skipped[n] = true
	jm.pending[n] = false
}
func (jm *JobMaster) Fail(n string) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	jm.failed[n] = true
	jm.pending[n] = false
}
func (jm *JobMaster) IsComplete() bool {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	for _, n := range jm.nodes {
		if !jm.completed[n] && !jm.skipped[n] && !jm.failed[n] {
			return false
		}
	}
	return true
}
func (jm *JobMaster) HasFailed() bool {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	for _, n := range jm.nodes {
		if jm.failed[n] {
			return true
		}
	}
	return false
}
func (jm *JobMaster) SetConditionResult(n string, r bool) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	jm.conditions[n] = r
}
func (jm *JobMaster) ConditionResult(n string) (bool, bool) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	r, o := jm.conditions[n]
	return r, o
}
func (jm *JobMaster) Inject(n string, d []string) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	jm.nodes = append(jm.nodes, n)
	jm.deps[n] = d
	jm.children[n] = []string{}
	jm.pending[n] = true
}
func (jm *JobMaster) Children(n string) []string {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	return jm.children[n]
}
func (jm *JobMaster) Deps(n string) []string { jm.mu.Lock(); defer jm.mu.Unlock(); return jm.deps[n] }

func (jm *JobMaster) dumpGraphState() string {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	var pending, notReady []string
	for _, n := range jm.nodes {
		if jm.completed[n] || jm.skipped[n] || jm.failed[n] {
			continue
		}
		pending = append(pending, n)
		if !jm.depsDoneLocked(n) {
			notReady = append(notReady, n)
		}
	}
	parts := make([]string, 0)
	for _, n := range notReady {
		var missing []string
		for _, d := range jm.deps[n] {
			if !jm.skipped[d] && !jm.completed[d] {
				missing = append(missing, d)
			}
		}
		parts = append(parts, fmt.Sprintf("%s(wait:%v)", n, missing))
	}
	return fmt.Sprintf("pending=%d not_ready=%s", len(pending), strings.Join(parts, " "))
}

func (jm *JobMaster) depsDoneLocked(n string) bool {
	anySatisfied := len(jm.deps[n]) == 0
	for _, d := range jm.deps[n] {
		if jm.skipped[d] || jm.completed[d] {
			anySatisfied = true
			continue
		}
		if c, exists := jm.edgeConditions[d+"->"+n]; exists && c != nil {
			continue // dormant conditional edge (source not yet evaluated)
		}
		return false
	}
	return anySatisfied
}

// ─── buildExecutionGraph — single pass: DAG state + ExecutionPlans ─

func (jm *JobMaster) buildExecutionGraph(spec *entities.FlowInfo, runID string) {
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

	jm.mu.Lock()
	defer jm.mu.Unlock()

	jm.nodes = nodeIDs
	jm.edges = edges
	jm.deps = make(map[string][]string)
	jm.children = make(map[string][]string)
	jm.completed = make(map[string]bool)
	jm.skipped = make(map[string]bool)
	jm.failed = make(map[string]bool)
	jm.pending = make(map[string]bool)
	jm.conditions = make(map[string]bool)
	jm.nodeErrors = make(map[string]string)
	jm.nodeOutputs = make(map[string]map[string]any)
	jm.planMap = make(map[string]*entities.ExecutionPlan)

	for _, n := range nodeIDs {
		jm.deps[n] = []string{}
		jm.children[n] = []string{}
		jm.pending[n] = true
	}
	for _, e := range edges {
		jm.deps[e[1]] = append(jm.deps[e[1]], e[0])
		jm.children[e[0]] = append(jm.children[e[0]], e[1])
	}

	if jm.edgeConditions == nil {
		jm.edgeConditions = make(map[string]*bool)
	}
	for _, ec := range edgeConds {
		jm.edgeConditions[ec.From+"->"+ec.To] = ec.Condition
	}

	for i := range spec.Nodes {
		n := &spec.Nodes[i]
		jm.planMap[n.ID] = &entities.ExecutionPlan{
			PlanID:                fmt.Sprintf("plan-%s-%s", runID, n.ID),
			AgentFlowRunID:        runID,
			AgentFlowDefinitionID: spec.ID,
			TenantID:              spec.TenantID,
			TaskID:                fmt.Sprintf("task-%s-%s", runID, n.ID),
			TaskType:              NodeToTaskType(n.Type), NodeID: n.ID,
			State: entities.TaskPending, MaxRetries: RetryMax(n.Retry),
			NodeSpec: entities.NodeSpecFromNode(n), CreatedAt: time.Now(),
		}
	}
}

// ─── Execute ─────────────────────────────────────────────

func (jm *JobMaster) Execute(ctx context.Context, run *entities.FlowRunInfo, spec *entities.FlowInfo) error {
	if jm.tracer == nil {
		jm.tracer = tracing.Tracer("flowgent/jobmaster")
	}

	jm.buildExecutionGraph(spec, run.ID)
	jm.applySupervisorConfig(spec)

	// Merge flow vars with run vars and inject built-in variables.
	jm.resolvedVars = make(map[string]any)
	for k, v := range spec.Vars {
		jm.resolvedVars[k] = v
	}
	for k, v := range run.Vars {
		jm.resolvedVars[k] = v
	}
	jm.resolvedVars["run_id"] = run.ID
	jm.resolvedVars["tenant_id"] = spec.TenantID
	jm.resolvedVars["flow_id"] = spec.ID

	ctx, span := jm.tracer.Start(ctx, "jobmaster.execute",
		trace.WithAttributes(
			attribute.String("agentflow.id", spec.ID), attribute.String("run.id", run.ID),
			attribute.String("resource_manager", string(jm.rm.Provider())),
			attribute.Int("node_count", len(spec.Nodes)),
		),
	)
	defer span.End()

	run.Status = entities.RunRunning
	now := time.Now()
	run.StartedAt = &now
	_ = jm.state.UpdateRun(ctx, run)

	if jm.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, jm.timeout)
		defer cancel()
	}

	for iteration := 1; ; iteration++ {
		select {
		case <-ctx.Done():
			slog.Debug("jobmaster execute context done", "err", ctx.Err())
			return ctx.Err()
		default:
		}
		if jm.HasFailed() {
			slog.Debug("jobmaster execute has failed", "error", jm.collectFirstError())
			run.Status = entities.RunFailed
			run.Error = jm.collectFirstError()
			run.FinishedAt = TimePtr()
			span.SetStatus(codes.Error, "failed")
			if err := jm.state.UpdateRun(ctx, run); err != nil {
				return err
			}
			jm.postHandle(run, spec)
			return nil
		}
		if jm.IsComplete() {
			slog.Debug("jobmaster execute is complete")
			run.Status = entities.RunCompleted
			run.FinishedAt = TimePtr()
			span.SetStatus(codes.Ok, "done")
			if err := jm.state.UpdateRun(ctx, run); err != nil {
				return err
			}
			jm.postHandle(run, spec)
			return nil
		}

		ready := jm.Ready()
		slog.Debug("jobmaster execute iteration", "iteration", iteration, "ready", ready,
			"completed", len(jm.completed), "total", len(jm.nodes), "deps", jm.dumpGraphState())
		if len(ready) == 0 {
			slog.Debug("jobmaster execute no ready nodes, breaking", "completed", len(jm.completed),
				"failed", len(jm.failed), "skipped", len(jm.skipped), "pending", len(jm.pending))
			break
		}

		for _, nodeID := range ready {
			plan, ok := jm.planMap[nodeID]
			if !ok {
				slog.Debug("jobmaster execute plan not found", "node", nodeID)
				continue
			}
			plan.Input = jm.resolveInput(nodeID, plan.NodeSpec.RawInput)
			_ = jm.state.SaveTask(ctx, taskRunFromPlan(plan))

			slog.Debug("jobmaster execute scheduling node", "node", nodeID, "type", plan.TaskType, "planID", plan.PlanID)
			result, err := jm.rm.Schedule(ctx, plan)
			if err != nil {
				slog.Warn("jobmaster execute schedule failed", "node", nodeID, "err", err)
				jm.logger.Error("submit failed", "node", nodeID, "err", err)
				jm.nodeErrors[nodeID] = err.Error()
				jm.Fail(nodeID)
				continue
			}
			if result.Error != "" {
				slog.Warn("jobmaster execute execution failed", "node", nodeID, "err", result.Error)
				jm.logger.Error("node execution failed", "node", nodeID, "err", result.Error)
				jm.nodeErrors[nodeID] = result.Error
				jm.Fail(nodeID)
				continue
			}

			slog.Debug("jobmaster execute node done", "node", nodeID, "hasOutput", result.Output != nil)
			if result.Output != nil {
				jm.nodeOutputs[nodeID] = result.Output
			}
			jm.Done(nodeID)

			if plan.TaskType == entities.TaskCondition {
				if r, ok := result.Output["result"].(bool); ok {
					jm.SetConditionResult(nodeID, r)
					for _, child := range jm.Children(nodeID) {
						if c := jm.GetChildCondition(nodeID, child); c != nil && *c != r {
							jm.Skip(child)
						}
					}
				}
			}
		}
	}
	slog.Debug("jobmaster execute loop exited, returning nil")
	return nil
}

func (jm *JobMaster) resolveInput(nodeID string, yamlInput map[string]any) map[string]any {
	// Scope: vars + ALL node outputs (not just direct deps), so that
	// a node can reference any ancestor's output via ${ancestor.field}.
	scope := map[string]map[string]any{"vars": jm.resolvedVars}
	for nid, output := range jm.nodeOutputs {
		scope[nid] = output
	}

	in := make(map[string]any)
	for k, v := range yamlInput {
		in[k] = resolveDeep(v, scope)
	}
	for nid, output := range jm.nodeOutputs {
		in[nid] = output
	}
	return in
}

func resolveDeep(v any, scope map[string]map[string]any) any {
	switch val := v.(type) {
	case string:
		// If the entire value is exactly one reference (${node} or ${node.field}),
		// return the raw value so that arrays/maps pass through as real Go types
		// rather than fmt.Sprintf("%v", ...) strings which downstream callers
		// (MCP tools, JSON serialization) cannot parse.
		if strings.HasPrefix(val, "${") && strings.HasSuffix(val, "}") && !strings.Contains(val, " ") {
			inner := val[2 : len(val)-1]
			if parts := strings.SplitN(inner, ".", 2); len(parts) > 0 {
				if node, ok := scope[parts[0]]; ok {
					if len(parts) == 1 {
						return node
					}
					if fieldVal, ok := node[parts[1]]; ok {
						return fieldVal
					}
				}
			}
		}
		return utils.Resolve(val, scope)
	case map[string]any:
		out := make(map[string]any)
		for k, vv := range val {
			out[k] = resolveDeep(vv, scope)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, vv := range val {
			out[i] = resolveDeep(vv, scope)
		}
		return out
	default:
		return v
	}
}

func (jm *JobMaster) collectFirstError() string {
	for _, err := range jm.nodeErrors {
		return err
	}
	return "node failed"
}

// postHandle runs asynchronously after the run completes, extracting knowledge
// from agent and tool node outputs for cross-workflow persistent memory.
func (jm *JobMaster) postHandle(run *entities.FlowRunInfo, spec *entities.FlowInfo) {
	if jm.knowledgeWriter == nil {
		return
	}

	// Snapshot fields needed by the async goroutine.
	tenant := spec.TenantID
	runID := run.ID
	flowID := spec.ID

	// Build a snapshot of completed node outputs.
	jm.mu.Lock()
	outputs := make(map[string]map[string]any, len(jm.nodeOutputs))
	for k, v := range jm.nodeOutputs {
		outputs[k] = v
	}
	jm.mu.Unlock()

	go func() {
		for nodeID, output := range outputs {
			if len(output) == 0 {
				continue
			}
			content, err := json.Marshal(output)
			if err != nil {
				continue
			}
			title := fmt.Sprintf("Run %s / node %s", runID, nodeID)
			sourceRef := fmt.Sprintf("%s:%s:%s", flowID, runID, nodeID)

			entry := &entities.KnowledgeEntry{
				Title:       title,
				Content:     string(content),
				ContentType: "json",
				Source:      "flow_run",
				SourceRef:   sourceRef,
				Tags:        []string{flowID, nodeID},
			}
			entry.TenantID = tenant

			if _, err := jm.knowledgeWriter.CreateKnowledge(context.Background(), tenant, entry); err != nil {
				slog.Warn("jobmaster postHandle create knowledge failed", "node", nodeID, "err", err)
			}
		}
	}()
}

func (jm *JobMaster) applySupervisorConfig(spec *entities.FlowInfo) {
	for _, n := range spec.Nodes {
		if n.Type == entities.SupervisorNode && n.SupervisorConfig != nil && n.SupervisorConfig.MaxNodes > 0 {
			jm.maxNodes = n.SupervisorConfig.MaxNodes
		}
	}
}

// taskRunFromPlan converts an ExecutionPlan to a minimal TaskRunInfo for persistence.
func taskRunFromPlan(plan *entities.ExecutionPlan) *entities.TaskRunInfo {
	return &entities.TaskRunInfo{
		BaseEntity:     entities.BaseEntity{ID: plan.TaskID},
		AgentFlowRunID: plan.AgentFlowRunID,
		NodeID:         plan.NodeID,
		Status:         plan.State,
		Input:          plan.Input,
	}
}

func NodeToTaskType(nt entities.NodeType) entities.TaskType {
	switch nt {
	case entities.AgentNode:
		return entities.TaskAgent
	case entities.ToolNode:
		return entities.TaskTool
	case entities.ConditionNode:
		return entities.TaskCondition
	case entities.CommitteeNode:
		return entities.TaskCommittee
	case entities.SupervisorNode:
		return entities.TaskSupervisor
	case entities.MapNode:
		return entities.TaskMap
	case entities.HumanNode:
		return entities.TaskHuman
	case entities.AgentFlowNode:
		return entities.TaskSubflow
	default:
		return entities.TaskNoop
	}
}

func RetryMax(r *entities.RetryPolicy) int {
	if r == nil {
		return 3
	}
	return r.Max
}
func TimePtr() *time.Time { t := time.Now(); return &t }

type RetryPolicy struct {
	Max      int
	Initial  time.Duration
	MaxDelay time.Duration
	Factor   float64
}

func ModelRetry(r *entities.RetryPolicy) RetryPolicy {
	if r == nil {
		return RetryPolicy{Max: 3, Initial: time.Second, MaxDelay: 30 * time.Second, Factor: 2.0}
	}
	rp := RetryPolicy{Max: r.Max, Initial: r.Initial.ToDuration(), MaxDelay: r.MaxDelay.ToDuration(), Factor: r.Factor}
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
