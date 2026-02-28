package engine

import (
	"context"
	"encoding/json"
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

// EdgeCondition stores an edge-level condition for routing.
type EdgeCondition struct {
	From      string
	To        string
	Condition *bool
}

// JobManager is the control-plane orchestrator for a single agent flow run.
// It builds ExecutionPlans from the AgentFlowSpec, dispatches them via
// the queue (MQTT), and monitors completion via status callbacks.
//
// The JM never executes tasks directly — all execution flows through
// TaskManager slot workers consuming from the queue.
type JobManager struct {
	store     Store
	q         queue.Queue
	logger    *util.Logger
	tracer    trace.Tracer
	timeout   time.Duration
	nodeLimit int
	maxNodes  int

	// DAG state (tracked by the JM, not executed by it)
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

	// Plan tracking
	planMap     map[string]*model.ExecutionPlan // nodeID → plan
	nodeOutputs map[string]map[string]any
}

// NewJobManager creates a control-plane-only JobManager.
func NewJobManager(store Store, q queue.Queue, logger *util.Logger) *JobManager {
	return &JobManager{
		store:    store,
		q:        q,
		logger:   logger,
		nodeOutputs: make(map[string]map[string]any),
	}
}

func (jm *JobManager) SetTimeout(t time.Duration) { jm.timeout = t }
func (jm *JobManager) SetNodeLimit(n int)         { jm.nodeLimit = n }

// ─── DAG state methods ──────────────────────────────────

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
	jm.nodeOutputs = make(map[string]map[string]any)
	jm.planMap = make(map[string]*model.ExecutionPlan)

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
	for _, dep := range jm.deps[node] {
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

func (jm *JobManager) Inject(node string, depsOn []string) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	jm.nodes = append(jm.nodes, node)
	jm.deps[node] = depsOn
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

// ─── Execution Plan Build + Dispatch ────────────────────

// buildGraph initializes DAG from spec.
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

// buildExecutionPlans creates an ExecutionPlan for every node in the spec.
func (jm *JobManager) buildExecutionPlans(runID string, spec *model.AgentFlowSpec) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	for i := range spec.Nodes {
		n := &spec.Nodes[i]
		nodeSpec := model.NodeSpecFromNode(n)
		plan := &model.ExecutionPlan{
			PlanID:         fmt.Sprintf("plan-%s-%s", runID, n.ID),
			AgentFlowRunID: runID,
			TaskID:         fmt.Sprintf("task-%s-%s", runID, n.ID),
			TaskType:       nodeTypeToTaskType(n.Type),
			NodeID:         n.ID,
			State:          model.TaskPending,
			MaxRetries:     maxRetry(n.Retry),
			NodeSpec:       nodeSpec,
			CreatedAt:      time.Now(),
		}
		jm.planMap[n.ID] = plan
	}
}

func nodeTypeToTaskType(nt model.NodeType) model.TaskType {
	switch nt {
	case model.AgentNode:
		return model.TaskAgent
	case model.ToolNode:
		return model.TaskTool
	case model.ConditionNode:
		return model.TaskCondition
	case model.TribunalNode:
		return model.TaskTribunal
	case model.SupervisorNode:
		return model.TaskSupervisor
	case model.MapNode:
		return model.TaskMap
	case model.HumanNode:
		return model.TaskHuman
	case model.AgentFlowNode:
		return model.TaskSubflow
	case model.NoopNode:
		return model.TaskNoop
	default:
		return model.TaskNoop
	}
}

func maxRetry(r *model.RetryPolicy) int {
	if r == nil {
		return 3
	}
	return r.Max
}

// ─── Start Job (control-plane entry point) ──────────────

// StartJob builds the DAG and execution plans, then dispatches initially
// ready nodes into the queue. It then enters a status-monitoring loop,
// dispatching newly-ready nodes as dependencies are satisfied.
func (jm *JobManager) StartJob(ctx context.Context, run *model.AgentFlowRun, spec *model.AgentFlowSpec) error {
	if jm.tracer == nil {
		jm.tracer = otel.Tracer("flowgent/jobmanager")
	}

	jm.buildGraph(spec)
	jm.buildExecutionPlans(run.ID, spec)
	jm.applySupervisorConfig(spec)

	clusterID := fmt.Sprintf("cluster-%s-%s", run.AgentFlowID, run.ID)
	ctx, span := jm.tracer.Start(ctx, "jobmanager.startjob",
		trace.WithAttributes(
			attribute.String("agentflow.id", spec.ID),
			attribute.String("run.id", run.ID),
			attribute.String("cluster.id", clusterID),
			attribute.Int("node_count", len(spec.Nodes)),
		),
	)
	defer span.End()

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

	// Dispatch initial ready nodes
	jm.dispatchReadyPlans(ctx, run, spec)

	// Consume status updates until complete or failed
	return jm.monitorStatus(ctx, run, spec, span)
}

// dispatchReadyPlans sends ExecutionPlans for all currently ready nodes to the queue.
func (jm *JobManager) dispatchReadyPlans(ctx context.Context, run *model.AgentFlowRun, spec *model.AgentFlowSpec) {
	ready := jm.Ready()
	nodeMap := specNodeMap(spec)

	for _, nodeID := range ready {
		plan, ok := jm.planMap[nodeID]
		if !ok {
			continue
		}

		// Resolve input from upstream node outputs
		plan.Input = jm.buildPlanInput(nodeID, nodeMap)
		plan.State = model.TaskPending

		// Persist plan
		_ = jm.store.SaveExecutionPlan(ctx, plan)

		// Serialize and enqueue
		b, _ := json.Marshal(plan)
		_ = jm.q.Push(ctx, &queue.Message{
			ID:        plan.PlanID,
			TaskRunID: run.ID,
			NodeID:    nodeID,
			Payload:   b,
		})

		jm.logger.Debug("dispatched execution plan", "plan_id", plan.PlanID, "node", nodeID)
	}
}

// buildPlanInput resolves JSONPath references in the node's input spec
// against upstream node outputs.
func (jm *JobManager) buildPlanInput(nodeID string, nodeMap map[string]*model.Node) map[string]any {
	input := make(map[string]any)
	deps := jm.Deps(nodeID)
	for _, dep := range deps {
		if out, ok := jm.nodeOutputs[dep]; ok {
			input[dep] = out
		}
	}
	return input
}

// monitorStatus consumes status updates from the queue and updates DAG state.
func (jm *JobManager) monitorStatus(ctx context.Context, run *model.AgentFlowRun, spec *model.AgentFlowSpec, span trace.Span) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check for completion or failure after dispatching this wave
		if jm.IsComplete() {
			run.Status = model.RunCompleted
			run.FinishedAt = timePtr()
			span.SetStatus(codes.Ok, "job completed")
			return jm.store.UpdateAgentFlowRun(ctx, run)
		}
		if jm.HasFailed() {
			run.Status = model.RunFailed
			run.Error = "one or more nodes failed"
			run.FinishedAt = timePtr()
			span.SetStatus(codes.Error, "node failed")
			return jm.store.UpdateAgentFlowRun(ctx, run)
		}

		// Poll for status messages
		msg, err := jm.q.Pop(ctx, 2*time.Second)
		if err != nil || msg == nil {
			continue
		}

		var status map[string]any
		if err := json.Unmarshal(msg.Payload, &status); err != nil {
			continue
		}

		nodeID, _ := status["node_id"].(string)
		state, _ := status["state"].(string)

		switch state {
		case string(model.Success):
			// Load the completed plan to get its output
			if plan, err := jm.store.LoadExecutionPlan(ctx, msg.ID); err == nil && plan != nil && plan.Result != nil {
				jm.nodeOutputs[nodeID] = plan.Result.Output
			}
			jm.Done(nodeID)

			// Condition routing
			jm.handleConditionRouting(nodeID)
			// Supervisor handling
			jm.handleSupervisorResult(nodeID)

			// Dispatch newly ready nodes (this wave's dependencies satisfied)
			jm.dispatchReadyPlans(ctx, run, spec)

		case string(model.Failed), string(model.Skipped):
			if state == string(model.Failed) {
				jm.Fail(nodeID)
			} else {
				jm.Skip(nodeID)
				jm.dispatchReadyPlans(ctx, run, spec)
			}
		}

		_ = jm.q.Ack(ctx, msg.ID)
	}
}

func (jm *JobManager) handleConditionRouting(nodeID string) {
	r, ok := jm.ConditionResult(nodeID)
	if !ok {
		return
	}
	for _, child := range jm.Children(nodeID) {
		cond := jm.GetChildCondition(nodeID, child)
		if cond != nil && *cond != r {
			jm.Skip(child)
		}
	}
}

func (jm *JobManager) handleSupervisorResult(nodeID string) {
	plan, ok := jm.planMap[nodeID]
	if !ok || plan.Result == nil {
		return
	}
	action, _ := plan.Result.Output["action"].(string)
	switch action {
	case "inject":
		// JM handles injection by marking the target as done
		if target, ok := plan.Result.Output["target"].(string); ok && target != "" {
			jm.logger.Info("supervisor inject", "target", target)
		}
	}
}

// ─── helpers ────────────────────────────────────────────

func (jm *JobManager) applySupervisorConfig(spec *model.AgentFlowSpec) {
	for i := range spec.Nodes {
		n := &spec.Nodes[i]
		if n.Type == model.SupervisorNode && n.SupervisorConfig != nil {
			if n.SupervisorConfig.MaxNodes > 0 {
				jm.maxNodes = n.SupervisorConfig.MaxNodes
			}
		}
	}
}

func specNodeMap(spec *model.AgentFlowSpec) map[string]*model.Node {
	m := make(map[string]*model.Node)
	for i := range spec.Nodes {
		m[spec.Nodes[i].ID] = &spec.Nodes[i]
	}
	return m
}

func timePtr() *time.Time {
	t := time.Now()
	return &t
}
