package jobmanager

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
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

var ErrRecoveryBlocked = errors.New("recovery requires reconciliation")

// RunStateStore is the narrow state interface JobMaster needs for persistence.
// Implementations call the apiserver REST API (never direct DB).
type RunStateStore interface {
	UpdateRun(ctx context.Context, run *entities.FlowRunInfo) error
	SaveTask(ctx context.Context, task *entities.TaskRunInfo) error
}

// KnowledgePostWriter creates immutable candidates. Publication remains behind
// the unified human-approval transaction in the API/storage layer.
type KnowledgePostWriter interface {
	CreateKnowledgeCandidate(ctx context.Context, namespace string, candidate *entities.KnowledgeCandidate) (*entities.ApprovalInfo, error)
}

type EdgeCondition struct {
	From, To  string
	Condition *bool
}

// JobMaster is the per-run DAG orchestrator. It builds the execution graph
// from an FlowInfo and dispatches plans via resourcemanager.ResourceManager.Schedule().
// Each agentflow run gets its own JobMaster instance — no shared state.
type JobMaster struct {
	state          RunStateStore
	rm             resourcemanager.ResourceManager
	logger         *utils.Logger
	tracer         trace.Tracer
	timeout        time.Duration
	nodeLimit      int
	maxNodes       int
	clusterID      string
	maxParallelism int
	graphRevision  int64
	instancePrefix string
	child          bool
	slots          chan struct{}

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
	if cfg == nil {
		cfg = &JobManagerConfig{}
	}
	timeout := cfg.FlowExecutionTimeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}

	parallelism := cfg.MaxConcurrentNodes
	if parallelism < 1 {
		parallelism = 1
	}
	return &JobMaster{maxParallelism: parallelism,
		state: state, rm: rm, logger: logger,
		timeout: timeout, nodeLimit: cfg.MaxNodeRetries,
		clusterID:   cfg.RuntimeClusterID,
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

	// Propagate an inactive branch through its single-path descendants. A
	// convergence node remains reachable when at least one inbound edge is
	// active; a node whose inbound edges are all inactive is skipped. Repeat to
	// a fixed point so an entire unselected branch is removed in one pass.
	for {
		changed := false
		for _, n := range jm.nodes {
			if jm.completed[n] || jm.skipped[n] || jm.failed[n] || len(jm.deps[n]) == 0 {
				continue
			}
			resolved, active := jm.dependencyStateLocked(n)
			if resolved && !active {
				jm.skipped[n] = true
				jm.pending[n] = false
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	var r []string
	for _, n := range jm.nodes {
		if !jm.completed[n] && !jm.skipped[n] && !jm.failed[n] && jm.depsDone(n) {
			r = append(r, n)
		}
	}
	return r
}
func (jm *JobMaster) depsDone(n string) bool {
	resolved, active := jm.dependencyStateLocked(n)
	return resolved && active
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
	resolved, active := jm.dependencyStateLocked(n)
	return resolved && active
}

// dependencyStateLocked resolves incoming edges independently of node state.
// An unconditional edge is active when its source completed. A conditional
// edge is active only when the source's evaluated result matches the edge.
// Skipped sources and non-matching conditional edges are inactive. Callers
// must hold jm.mu.
func (jm *JobMaster) dependencyStateLocked(n string) (resolved, active bool) {
	if len(jm.deps[n]) == 0 {
		return true, true
	}
	if plan := jm.planMap[n]; plan != nil && plan.NodeSpec.JoinMode == "or" {
		allResolved := true
		for _, dep := range jm.deps[n] {
			if jm.completed[dep] {
				if expected := jm.edgeConditions[dep+"->"+n]; expected == nil || jm.conditions[dep] == *expected {
					return true, true
				}
			}
			if !jm.completed[dep] && !jm.skipped[dep] {
				allResolved = false
			}
		}
		return allResolved, false
	}

	anyActive := false
	hasDormantConditional := false
	for _, d := range jm.deps[n] {
		if jm.skipped[d] {
			continue
		}
		if expected := jm.edgeConditions[d+"->"+n]; expected != nil {
			if !jm.completed[d] {
				// A conditional edge whose source has not run is dormant. It
				// must not block an already-active initial path (feedback edges),
				// but a node fed only by dormant edges must keep waiting.
				hasDormantConditional = true
				continue
			}
			actual, evaluated := jm.conditions[d]
			if !evaluated {
				hasDormantConditional = true
				continue
			}
			if actual != *expected {
				continue
			}
		} else if !jm.completed[d] {
			return false, false
		}
		anyActive = true
	}
	if anyActive {
		return true, true
	}
	if hasDormantConditional {
		return false, false
	}
	return true, anyActive
}

// ─── buildExecutionGraph — single pass: DAG state + ExecutionPlans ─

func (jm *JobMaster) buildExecutionGraph(spec *entities.FlowInfo, runID string) error {
	if err := spec.ValidateDAG(); err != nil {
		return err
	}
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
		flowNodeID := n.FlowNodeID
		if flowNodeID == "" {
			flowNodeID = n.ID
		}
		nodeSpec := entities.NodeSpecFromNode(n)
		taskType, err := NodeToTaskType(nodeSpec.Kind)
		if err != nil {
			return err
		}
		jm.planMap[n.ID] = &entities.ExecutionPlan{
			PlanID:                executionPlanID(runID, jm.instancePrefix+n.ID),
			AgentFlowRunID:        runID,
			AgentFlowDefinitionID: spec.ResourceName(),
			Namespace:             spec.Namespace,
			RuntimeMode:           spec.RuntimeMode,
			RuntimeClusterID:      jm.clusterID,
			TaskID:                fmt.Sprintf("task-%s-%s", runID, jm.instancePrefix+n.ID),
			TaskType:              taskType, NodeID: jm.instancePrefix + n.ID,
			NodeInstanceID: jm.instancePrefix + n.ID, DispatchEpoch: 1,
			State: entities.TaskPending, MaxRetries: RetryMax(n.Retry),
			GraphRevision: jm.graphRevision, SubflowInstanceID: strings.TrimSuffix(jm.instancePrefix, "/"), FlowNodeID: flowNodeID,
			NodeSpec: nodeSpec, CreatedAt: time.Now(),
		}
	}
	return nil
}

// ─── Execute ─────────────────────────────────────────────

func (jm *JobMaster) Execute(ctx context.Context, run *entities.FlowRunInfo, spec *entities.FlowInfo) (executeErr error) {
	defer func() {
		if !jm.child && errors.Is(executeErr, ErrRecoveryBlocked) {
			run.Status = entities.RunPaused
			run.Error = executeErr.Error()
			if err := jm.state.UpdateRun(ctx, run); err != nil {
				executeErr = fmt.Errorf("%w; persist pause: %v", executeErr, err)
			}
		}
	}()
	if jm.tracer == nil {
		jm.tracer = tracing.Tracer("flowgent/jobmaster")
	}

	if jm.slots == nil {
		capacity := jm.maxParallelism
		for _, node := range spec.Nodes {
			if node.Subflow != nil {
				limit := node.Subflow.Limits.MaxParallelism
				if limit == 0 && node.Subflow.Adaptive() {
					limit = 4
				}
				if limit > capacity {
					capacity = limit
				}
			}
		}
		jm.slots = make(chan struct{}, capacity)
	}
	if err := jm.buildExecutionGraph(spec, run.ID); err != nil {
		return fmt.Errorf("build execution graph: %w", err)
	}
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
	jm.resolvedVars["namespace_id"] = spec.Namespace
	jm.resolvedVars["flow_id"] = spec.ResourceName()

	ctx, span := jm.tracer.Start(ctx, "jobmaster.execute",
		trace.WithAttributes(
			attribute.String("agentflow.id", spec.ResourceName()), attribute.String("run.id", run.ID),
			attribute.String("resource_manager", string(jm.rm.Provider())),
			attribute.Int("node_count", len(spec.Nodes)),
		),
	)
	defer span.End()

	run.Status = entities.RunRunning
	now := time.Now()
	run.StartedAt = &now
	if !jm.child {
		if err := jm.state.UpdateRun(ctx, run); err != nil {
			return err
		}
	}

	if jm.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, jm.timeout)
		defer cancel()
	}

	if reader, ok := jm.state.(interface {
		ListTasks(context.Context, string) ([]*entities.TaskRunInfo, error)
	}); ok {
		tasks, err := reader.ListTasks(ctx, run.ID)
		if err != nil {
			return fmt.Errorf("restore node attempts: %w", err)
		}
		latest := map[string]*entities.TaskRunInfo{}
		for _, t := range tasks {
			t.NormalizeAliases()
			if old := latest[t.NodeKey]; old == nil || t.Attempt > old.Attempt {
				latest[t.NodeKey] = t
			}
		}
		for id, plan := range jm.planMap {
			if t := latest[plan.NodeID]; t != nil {
				switch t.Status {
				case entities.Success:
					jm.nodeOutputs[id] = t.Output
					jm.Done(id)
					if plan.TaskType == entities.TaskCondition {
						if v, ok := t.Output["result"].(bool); ok {
							jm.SetConditionResult(id, v)
						}
					}
				case entities.TaskWaiting:
					if plan.TaskType != entities.TaskSubflow {
						return fmt.Errorf("%w: unresolved waiting task %s", ErrRecoveryBlocked, plan.NodeID)
					}
					plan.RetryCount = t.Attempt - 1
					plan.StartedAt = t.StartedAt
				case entities.TaskPending:
					plan.RetryCount = t.Attempt - 1
				case entities.Running:
					return fmt.Errorf("%w: node %s has an unresolved running attempt", ErrRecoveryBlocked, plan.NodeID)
				case entities.Skipped:
					jm.Skip(id)
				case entities.Failed:
					plan.RetryCount = t.Attempt
					plan.ParentTaskRunID = t.ID
				}
			}
		}
	}
	type completion struct {
		id     string
		result *entities.TaskResult
		err    error
	}
	events := make(chan completion, jm.maxParallelism)
	inflight := map[string]bool{}
	workCtx, cancelWork := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancelWork(); workers.Wait() }()
	for {
		if err := ctx.Err(); err != nil {
			cancelWork()
			return err
		}
		if jm.HasFailed() && len(inflight) == 0 {
			run.Status = entities.RunFailed
			run.Error = jm.collectFirstError()
			run.FinishedAt = TimePtr()
			span.SetStatus(codes.Error, "failed")
			if !jm.child {
				if err := jm.state.UpdateRun(ctx, run); err != nil {
					return err
				}
				jm.postHandle(run, spec)
			}
			return nil
		}
		if jm.IsComplete() && len(inflight) == 0 {
			run.Status = entities.RunCompleted
			run.FinishedAt = TimePtr()
			span.SetStatus(codes.Ok, "done")
			if !jm.child {
				if err := jm.state.UpdateRun(ctx, run); err != nil {
					return err
				}
				jm.postHandle(run, spec)
			}
			return nil
		}
		ready := jm.Ready()
		for _, id := range ready {
			if inflight[id] || len(inflight) >= jm.maxParallelism || jm.HasFailed() {
				continue
			}
			plan := jm.planMap[id]
			if plan == nil {
				return fmt.Errorf("missing execution plan %s", id)
			}
			plan.Input = jm.resolveInput(id, plan.NodeSpec.Args)
			inflight[id] = true
			workers.Add(1)
			go func(id string, plan *entities.ExecutionPlan) {
				defer workers.Done()
				nodeCtx, nodeSpan := jm.tracer.Start(workCtx, "jobmaster.node", trace.WithAttributes(attribute.String("flowgent.node_id", id), attribute.String("run.id", run.ID)))
				defer nodeSpan.End()
				var result *entities.TaskResult
				var err error
				if plan.TaskType == entities.TaskSubflow {
					result, err = jm.executeSubflow(nodeCtx, run, plan)
				} else {
					result, err = jm.scheduleNodeWithRetry(nodeCtx, plan)
				}
				events <- completion{id, result, err}
			}(id, plan)
		}
		if len(inflight) == 0 {
			if jm.IsComplete() {
				continue
			}
			return fmt.Errorf("execution stalled with unresolved dependencies: %s", jm.dumpGraphState())
		}
		select {
		case <-ctx.Done():
			cancelWork()
			return ctx.Err()
		case event := <-events:
			delete(inflight, event.id)
			if errors.Is(event.err, ErrRecoveryBlocked) {
				return event.err
			}
			if event.err != nil {
				jm.nodeErrors[event.id] = event.err.Error()
				jm.Fail(event.id)
				continue
			}
			if event.result == nil {
				jm.nodeErrors[event.id] = "missing task result"
				jm.Fail(event.id)
				continue
			}
			jm.nodeOutputs[event.id] = event.result.Output
			jm.Done(event.id)
			if jm.planMap[event.id].TaskType == entities.TaskCondition {
				if v, ok := event.result.Output["result"].(bool); ok {
					jm.SetConditionResult(event.id, v)
				}
			}
		}
	}
}

func (jm *JobMaster) scheduleNodeWithRetry(ctx context.Context, plan *entities.ExecutionPlan) (*entities.TaskResult, error) {
	policy := jm.nodeRetryPolicy(plan.NodeSpec.Retry)
	plan.MaxRetries = policy.Max
	baseTaskID := plan.TaskID
	firstAttempt := plan.RetryCount
	previousTaskID := plan.ParentTaskRunID
	delay := policy.Initial
	var lastErr error = fmt.Errorf("persisted retry budget exhausted")

	for attempt := 0; firstAttempt+attempt <= policy.Max; attempt++ {
		if attempt > 0 {
			if err := waitForRetry(ctx, delay); err != nil {
				return nil, err
			}
			delay = nextRetryDelay(delay, policy)
		}

		plan.TaskID = taskAttemptID(baseTaskID, firstAttempt+attempt)
		plan.ParentTaskRunID = previousTaskID
		plan.RetryCount = firstAttempt + attempt
		plan.FencingToken = plan.DispatchEpoch
		plan.State = entities.TaskPending
		plan.Result = nil
		plan.FinishedAt = nil
		startedAt := time.Now().UTC()
		plan.StartedAt = &startedAt

		attemptCtx, attemptSpan := jm.tracer.Start(ctx, "jobmaster.node.attempt",
			trace.WithAttributes(
				attribute.String("run.id", plan.AgentFlowRunID),
				attribute.String("agentflow.id", plan.AgentFlowDefinitionID),
				attribute.String("flowgent.node_id", plan.NodeID),
				attribute.String("flowgent.task_id", plan.TaskID),
				attribute.String("flowgent.plan_id", plan.PlanID),
				attribute.Int("flowgent.attempt", attempt+1),
				attribute.Int("flowgent.max_retries", policy.Max),
			),
		)
		attemptSpan.SetAttributes(tracing.PayloadAttributes("input", plan.Input)...)
		task := taskRunFromPlan(plan)
		if err := jm.state.SaveTask(attemptCtx, task); err != nil {
			attemptSpan.RecordError(err)
			attemptSpan.SetStatus(codes.Error, "persist attempt failed")
			attemptSpan.End()
			if plan.SubflowInstanceID != "" {
				return nil, fmt.Errorf("%w: persist task attempt: %v", ErrRecoveryBlocked, err)
			}
			return nil, fmt.Errorf("persist task attempt: %w", err)
		}

		var result *entities.TaskResult
		var scheduleErr error
		if jm.slots != nil {
			select {
			case jm.slots <- struct{}{}:
			case <-attemptCtx.Done():
				attemptSpan.End()
				return nil, attemptCtx.Err()
			}
		}
		result, scheduleErr = jm.rm.Schedule(attemptCtx, plan)
		if jm.slots != nil {
			<-jm.slots
		}
		if scheduleErr == nil && result != nil && result.State == entities.TaskWaiting {
			attemptSpan.End()
			return nil, fmt.Errorf("%w: task yielded waiting", ErrRecoveryBlocked)
		}
		// A transport failure does not prove a remote tool stopped. Preserve the
		// committed Attempt and require reconciliation rather than issue it again.
		if plan.DispatchEpoch > 0 && scheduleErr != nil {
			attemptSpan.RecordError(scheduleErr)
			attemptSpan.End()
			return nil, fmt.Errorf("%w: %v", ErrRecoveryBlocked, scheduleErr)
		}
		lastErr = scheduleErr
		if lastErr == nil && result == nil {
			lastErr = fmt.Errorf("task execution returned no result")
		}
		if lastErr == nil && result.Error != "" {
			lastErr = fmt.Errorf("%s", result.Error)
		}

		finishedAt := time.Now().UTC()
		plan.FinishedAt = &finishedAt
		task.FinishedAt = &finishedAt
		if lastErr == nil {
			plan.State = entities.Success
			plan.Result = result
			task.Status = entities.Success
			task.Output = result.Output
			task.Error = ""
			attemptSpan.SetAttributes(tracing.PayloadAttributes("output", result.Output)...)
			if err := jm.state.SaveTask(attemptCtx, task); err != nil {
				attemptSpan.RecordError(err)
				attemptSpan.SetStatus(codes.Error, "persist result failed")
				attemptSpan.End()
				return nil, fmt.Errorf("persist task result: %w", err)
			}
			attemptSpan.SetStatus(codes.Ok, "done")
			attemptSpan.End()
			return result, nil
		}

		plan.State = entities.Failed
		plan.Result = &entities.TaskResult{Error: lastErr.Error()}
		task.Status = entities.Failed
		task.Error = lastErr.Error()
		if err := jm.state.SaveTask(attemptCtx, task); err != nil {
			attemptSpan.End()
			return nil, fmt.Errorf("persist failed attempt: %w", err)
		}
		attemptSpan.RecordError(lastErr)
		attemptSpan.SetStatus(codes.Error, "attempt failed")
		attemptSpan.End()
		previousTaskID = plan.TaskID
	}

	return nil, fmt.Errorf("retry exhausted after %d attempt(s): %w", policy.Max+1, lastErr)
}

func (jm *JobMaster) nodeRetryPolicy(config *entities.RetryPolicy) RetryPolicy {
	if config == nil {
		return RetryPolicy{Max: 0, Initial: time.Second, MaxDelay: 30 * time.Second, Factor: 2}
	}
	policy := ModelRetry(config)
	if config.Max <= 0 && jm.nodeLimit > 0 {
		policy.Max = jm.nodeLimit
	}
	return policy
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
	// Only inject node outputs that are actually referenced via ${node} or ${node.field}
	// in this node's yamlInput; avoid copying ALL prior outputs into every DB task row,
	// which can overflow PostgreSQL's 256 MiB JSONB limit for large payloads like
	// SonarQube scan results.
	refs := collectRefs(yamlInput)
	for nid := range refs {
		if output, ok := jm.nodeOutputs[nid]; ok {
			in[nid] = output
		}
	}
	return in
}

// collectRefs walks yamlInput values recursively and returns the set of
// node-ids referenced via ${node} or ${node.field} patterns (not ${vars.x}).
func collectRefs(input map[string]any) map[string]struct{} {
	refs := make(map[string]struct{})
	var walk func(v any)
	walk = func(v any) {
		switch val := v.(type) {
		case string:
			if strings.HasPrefix(val, "${") && strings.HasSuffix(val, "}") {
				inner := val[2 : len(val)-1]
				if parts := strings.SplitN(inner, ".", 2); len(parts) > 0 && parts[0] != "vars" {
					refs[parts[0]] = struct{}{}
				}
			}
		case map[string]any:
			for _, vv := range val {
				walk(vv)
			}
		case []any:
			for _, vv := range val {
				walk(vv)
			}
		}
	}
	walk(input)
	return refs
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

var summarySecretPattern = regexp.MustCompile(`(?i)(-----BEGIN [A-Z ]*PRIVATE KEY-----|\bbearer\s+[a-z0-9._~+/=-]{8,}|\b(?:sk|ghp|gho|github_pat)_[a-z0-9_-]{8,}|(?:api[_-]?key|password|secret|token)\s*[:=]\s*[^\s,;}]+)`)

// postHandle runs asynchronously after a successfully completed run. It only
// creates approval-bound candidates; it never publishes cross-run knowledge.
func (jm *JobMaster) postHandle(run *entities.FlowRunInfo, spec *entities.FlowInfo) {
	if jm.knowledgeWriter == nil || run == nil || spec == nil ||
		run.Status != entities.RunCompleted || !run.SummarizeEnabled {
		return
	}

	// Snapshot fields needed by the async goroutine.
	namespace := spec.Namespace
	runID := run.ID
	flowID := run.FlowID
	if flowID == "" {
		flowID = spec.ID
	}
	flowName := spec.ResourceName()

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
			sanitized, ok := sanitizeSummaryValue(output)
			if !ok {
				continue
			}
			content, err := json.Marshal(map[string]any{
				"node": nodeID, "result": sanitized,
			})
			if err != nil {
				continue
			}
			if len(content) > 64*1024 || summarySecretPattern.Match(content) {
				slog.Warn("jobmaster skipped unsafe run summary", "node", nodeID, "run", runID)
				continue
			}
			candidate := &entities.KnowledgeCandidate{
				BaseEntity: entities.BaseEntity{
					Namespace:   namespace,
					CreatedBy:   "service:jobmaster",
					Description: fmt.Sprintf("Run %s node %s summary", runID, nodeID),
				},
				SourceRunID:    runID,
				TargetScope:    "flow",
				TargetFlowID:   flowID,
				TargetFlowName: flowName,
				Type:           "knowledge",
				Content:        string(content),
				Provenance: map[string]any{
					"origin": "built_in_summarizer", "run_id": runID, "node_key": nodeID,
				},
				ExpectedRevision: 0,
				IdempotencyKey:   fmt.Sprintf("summary:%s:%s:v1", runID, nodeID),
			}
			if _, err := jm.knowledgeWriter.CreateKnowledgeCandidate(context.Background(), namespace, candidate); err != nil {
				slog.Warn("jobmaster postHandle create knowledge candidate failed", "node", nodeID, "err", err)
			}
		}
	}()
}

func sanitizeSummaryValue(value any) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		clean := make(map[string]any, len(typed))
		for key, child := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
			if strings.Contains(normalized, "secret") || strings.Contains(normalized, "password") ||
				strings.Contains(normalized, "token") || strings.Contains(normalized, "api_key") ||
				strings.Contains(normalized, "authorization") || strings.Contains(normalized, "cookie") ||
				strings.Contains(normalized, "private_key") {
				continue
			}
			if sanitized, keep := sanitizeSummaryValue(child); keep {
				clean[key] = sanitized
			}
		}
		return clean, len(clean) > 0
	case []any:
		clean := make([]any, 0, len(typed))
		for _, child := range typed {
			if sanitized, keep := sanitizeSummaryValue(child); keep {
				clean = append(clean, sanitized)
			}
		}
		return clean, len(clean) > 0
	case string:
		if summarySecretPattern.MatchString(typed) {
			return nil, false
		}
		return typed, strings.TrimSpace(typed) != ""
	case nil:
		return nil, false
	default:
		return typed, true
	}
}

func (jm *JobMaster) applySupervisorConfig(spec *entities.FlowInfo) {
	for _, n := range spec.Nodes {
		if n.Kind == entities.SupervisorNode && n.SupervisorConfig != nil && n.SupervisorConfig.MaxNodes > 0 {
			jm.maxNodes = n.SupervisorConfig.MaxNodes
		}
	}
}

// taskRunFromPlan converts an ExecutionPlan to a minimal TaskRunInfo for persistence.
func taskRunFromPlan(plan *entities.ExecutionPlan) *entities.TaskRunInfo {
	return &entities.TaskRunInfo{
		BaseEntity:    entities.BaseEntity{ID: plan.TaskID, Metadata: map[string]any{"node_spec": plan.NodeSpec}},
		RunID:         plan.AgentFlowRunID,
		GraphRevision: plan.GraphRevision, SubflowInstanceID: plan.SubflowInstanceID,
		FlowNodeID:      plan.FlowNodeID,
		NodeInstanceID:  plan.NodeInstanceID,
		DispatchEpoch:   plan.DispatchEpoch,
		FencingToken:    plan.FencingToken,
		NodeKey:         plan.NodeID,
		Attempt:         plan.RetryCount + 1,
		Status:          plan.State,
		Input:           plan.Input,
		Output:          planResultOutput(plan.Result),
		Error:           planResultError(plan.Result),
		ExecutionID:     fmt.Sprintf("%s-attempt-%d", plan.PlanID, plan.RetryCount+1),
		MaxRetries:      plan.MaxRetries,
		ParentNodeRunID: plan.ParentTaskRunID,
		Sequence:        plan.RetryCount + 1,
		StartedAt:       plan.StartedAt,
		FinishedAt:      plan.FinishedAt,
	}
}

func planResultOutput(result *entities.TaskResult) map[string]any {
	if result == nil {
		return nil
	}
	return result.Output
}

func planResultError(result *entities.TaskResult) string {
	if result == nil {
		return ""
	}
	return result.Error
}

func taskAttemptID(baseTaskID string, attempt int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", baseTaskID, attempt)))
	return fmt.Sprintf("task-%x", sum[:16])
}

func NodeToTaskType(nt entities.NodeType) (entities.TaskType, error) {
	switch nt {
	case entities.AgentNode:
		return entities.TaskAgent, nil
	case entities.ToolNode:
		return entities.TaskTool, nil
	case entities.ConditionNode:
		return entities.TaskCondition, nil
	case entities.CommitteeNode:
		return entities.TaskCommittee, nil
	case entities.SupervisorNode:
		return entities.TaskSupervisor, nil
	case entities.MapNode:
		return entities.TaskMap, nil
	case entities.HumanNode:
		return entities.TaskHuman, nil
	case entities.AgentFlowNode:
		return entities.TaskSubflow, nil
	case entities.SandboxNode:
		return entities.TaskSandbox, nil
	case entities.SkillNode:
		return entities.TaskSkill, nil
	case entities.JoinNode:
		return entities.TaskJoin, nil
	case entities.NoopNode:
		return entities.TaskNoop, nil
	default:
		return "", fmt.Errorf("unsupported node kind %q", nt)
	}
}

func RetryMax(r *entities.RetryPolicy) int {
	if r == nil {
		return 0
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
			if waitErr := waitForRetry(ctx, delay); waitErr != nil {
				return waitErr
			}
			delay = nextRetryDelay(delay, policy)
		}
		err = fn()
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("retry exhausted after %d attempts: %w", policy.Max, err)
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func nextRetryDelay(delay time.Duration, policy RetryPolicy) time.Duration {
	next := time.Duration(float64(delay) * policy.Factor)
	if next > policy.MaxDelay {
		return policy.MaxDelay
	}
	return next
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

func executionPlanID(runID, instance string) string {
	id := fmt.Sprintf("plan-%s-%s", runID, instance)
	if len(id) <= 96 {
		return id
	}
	sum := sha256.Sum256([]byte(runID + "\x00" + instance))
	return fmt.Sprintf("plan-%x", sum[:24])
}
