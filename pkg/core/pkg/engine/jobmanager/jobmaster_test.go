package jobmanager

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/executor"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"go.opentelemetry.io/otel"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestConditionDispatchIncludesOnlyReferencedAncestorOutputs(t *testing.T) {
	for _, expression := range []string{"${committee.decision} == true", "${committee.decision == true}", "${committee.decision}"} {
		for _, decision := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/%t", expression, decision), func(t *testing.T) {
				jm := NewJobMaster(nil, nil, nil, nil)
				graph := &entities.AgentGraph{Name: "conditions", BaseEntity: entities.BaseEntity{ID: "graph", Namespace: "test"},
					Nodes: []entities.GraphNode{{ID: "committee", Kind: entities.NoopNode}, {ID: "supervisor", Kind: entities.NoopNode}, {ID: "check", Kind: entities.ConditionNode, Expression: expression}},
					Edges: []entities.GraphEdge{{From: "committee", To: "supervisor"}, {From: "supervisor", To: "check"}},
				}
				if err := jm.buildExecutionGraph(graph, "run"); err != nil {
					t.Fatal(err)
				}
				jm.nodeOutputs["committee"] = map[string]any{"decision": decision}
				jm.nodeOutputs["unrelated"] = map[string]any{"large_scan": "not needed by this condition"}
				plan := jm.planMap["check"]
				plan.Input = jm.resolveInput("check", plan.Node.Args)
				if len(plan.Input) != 1 || plan.Input["committee"] == nil {
					t.Fatalf("condition input contains missing or unrelated outputs: %+v", plan.Input)
				}
				result, err := (&executor.ConditionExecutor{}).Execute(t.Context(), plan, map[string]map[string]any{"input": plan.Input})
				if err != nil || result.Output["result"] != decision {
					t.Fatalf("dispatched condition = %+v, %v; want %t", result, err, decision)
				}
			})
		}
	}
}

func TestExecuteRecordsActualRunAndAttemptCounters(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetMeterProvider(metricnoop.NewMeterProvider())
	})
	state := &retryRecordingState{}
	jm := NewJobMaster(state, &retryResourceManager{}, utils.NewLogger("TEXT", "ERROR"), nil)
	graph := &entities.AgentGraph{Name: "metered", BaseEntity: entities.BaseEntity{ID: "definition", Namespace: "test"},
		Nodes: []entities.GraphNode{{ID: "first", Kind: entities.NoopNode}, {ID: "second", Kind: entities.NoopNode}},
		Edges: []entities.GraphEdge{{From: "first", To: "second"}},
	}
	run := &entities.GraphRun{BaseEntity: entities.BaseEntity{ID: "metered-run", Namespace: "test"}}
	if err := jm.Execute(t.Context(), run, graph); err != nil {
		t.Fatal(err)
	}
	var snapshot metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &snapshot); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int64{}
	for _, scope := range snapshot.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			if sum, ok := measurement.Data.(metricdata.Sum[int64]); ok {
				for _, point := range sum.DataPoints {
					status, _ := point.Attributes.Value("status")
					counts[measurement.Name+"/"+status.AsString()] += point.Value
				}
			}
		}
	}
	for name, want := range map[string]int64{
		"flowgent.jm.runs.total/RUNNING": 1, "flowgent.jm.runs.total/COMPLETED": 1,
		"flowgent.jm.plans.dispatched/":       int64(len(state.tasks)),
		"flowgent.jm.plans.completed/SUCCESS": int64(len(state.tasks)),
		"flowgent.jm.runs.active/":            0,
	} {
		if got, found := counts[name]; !found || got != want {
			t.Errorf("%s = %d (present=%v), want %d", name, got, found, want)
		}
	}
}

type pauseRecordingState struct {
	retryRecordingState
	paused bool
}

func (s *pauseRecordingState) UpdateGraphRun(ctx context.Context, run *entities.GraphRun) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.paused = run.Status == entities.GraphRunPaused
	return nil
}

type waitingResourceManager struct{ retryResourceManager }

func (*waitingResourceManager) Schedule(context.Context, *entities.ExecutionPlan) (*entities.NodeResult, error) {
	return &entities.NodeResult{State: entities.NodeRunWaiting}, nil
}

func TestRecoveryPausePersistsAfterExecutionContextIsCancelled(t *testing.T) {
	state := &pauseRecordingState{}
	jm := NewJobMaster(state, &waitingResourceManager{}, utils.NewLogger("TEXT", "ERROR"), nil)
	graph := &entities.AgentGraph{Name: "waiting", BaseEntity: entities.BaseEntity{ID: "definition", Namespace: "test"},
		Nodes: []entities.GraphNode{{ID: "wait", Kind: entities.NoopNode}},
	}
	run := &entities.GraphRun{BaseEntity: entities.BaseEntity{ID: "waiting-run", Namespace: "test"}}
	if err := jm.Execute(t.Context(), run, graph); !errors.Is(err, ErrRecoveryBlocked) {
		t.Fatalf("Execute = %v, want recovery reconciliation", err)
	}
	if !state.paused || run.Status != entities.GraphRunPaused {
		t.Fatalf("pause was not durably written: %+v", run)
	}
}

func TestJobManager_BasicTopology(t *testing.T) {
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{AgentGraphExecutionTimeout: 30 * time.Minute})
	jm.BuildGraphNodes([]string{"A", "B", "C"}, [][2]string{{"A", "B"}, {"B", "C"}})

	ready := jm.Ready()
	if len(ready) != 1 || ready[0] != "A" {
		t.Fatalf("expected A ready, got %v", ready)
	}

	jm.Done("A")
	ready = jm.Ready()
	if len(ready) != 1 || ready[0] != "B" {
		t.Fatalf("expected B ready, got %v", ready)
	}

	jm.Done("B")
	ready = jm.Ready()
	if len(ready) != 1 || ready[0] != "C" {
		t.Fatalf("expected C ready, got %v", ready)
	}

	jm.Done("C")
	if !jm.IsComplete() {
		t.Fatal("expected complete")
	}
}

func TestBuildExecutionGraphUsesCanonicalNodeKind(t *testing.T) {
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{AgentGraphExecutionTimeout: time.Minute})
	graph := &entities.AgentGraph{Name: "canonical-node-kind",
		BaseEntity: entities.BaseEntity{ID: "canonical-node-kind", Namespace: "test"},
		Nodes:      []entities.GraphNode{{ID: "condition", Kind: entities.ConditionNode}},
	}

	if err := jm.buildExecutionGraph(graph, "run-1"); err != nil {
		t.Fatal(err)
	}
	plan := jm.planMap["condition"]
	if plan == nil || plan.NodeKind != entities.ConditionNode || plan.Node.Kind != entities.ConditionNode {
		t.Fatalf("canonical node kind was not preserved: %#v", plan)
	}
}

func TestJobManager_ParallelReady(t *testing.T) {
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{AgentGraphExecutionTimeout: 30 * time.Minute})
	jm.BuildGraphNodes([]string{"A", "B", "C"}, [][2]string{{"A", "B"}, {"A", "C"}})

	ready := jm.Ready()
	if len(ready) != 1 || ready[0] != "A" {
		t.Fatalf("expected A ready, got %v", ready)
	}

	jm.Done("A")
	ready = jm.Ready()
	if len(ready) != 2 {
		t.Fatalf("expected B and C ready, got %v", ready)
	}
}

func TestJobManager_Skip(t *testing.T) {
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{AgentGraphExecutionTimeout: 30 * time.Minute})
	jm.BuildGraphNodes([]string{"A", "B", "C"}, [][2]string{{"A", "B"}, {"B", "C"}})
	jm.Skip("B")

	ready := jm.Ready()
	if len(ready) != 1 || ready[0] != "A" {
		t.Fatalf("expected only A ready and skipped branch to propagate, got %v", ready)
	}

	jm.Done("A")
	ready = jm.Ready()
	if len(ready) != 0 || !jm.skipped["C"] {
		t.Fatalf("expected C to remain skipped, ready=%v skipped=%v", ready, jm.skipped["C"])
	}
}

func TestJobManager_ConditionalBranchSkipsDescendantsButKeepsMerge(t *testing.T) {
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{AgentGraphExecutionTimeout: 30 * time.Minute})
	jm.BuildGraphNodes(
		[]string{"cond", "selected", "not-selected", "not-selected-child", "merge"},
		[][2]string{
			{"cond", "selected"},
			{"cond", "not-selected"},
			{"not-selected", "not-selected-child"},
			{"selected", "merge"},
			{"not-selected-child", "merge"},
		},
	)
	conditionTrue := true
	conditionFalse := false
	jm.SetEdgeConditions([]EdgeCondition{
		{From: "cond", To: "selected", Condition: &conditionTrue},
		{From: "cond", To: "not-selected", Condition: &conditionFalse},
	})
	jm.Done("cond")
	jm.SetConditionResult("cond", true)

	ready := jm.Ready()
	if len(ready) != 1 || ready[0] != "selected" {
		t.Fatalf("expected selected branch only, got %v", ready)
	}
	if !jm.skipped["not-selected"] || !jm.skipped["not-selected-child"] {
		t.Fatalf("inactive branch did not propagate: skipped=%v", jm.skipped)
	}

	jm.Done("selected")
	ready = jm.Ready()
	if len(ready) != 1 || ready[0] != "merge" {
		t.Fatalf("expected merge through active branch, got %v", ready)
	}
}

func TestJobManager_DormantConditionalFeedbackDoesNotBlockInitialPath(t *testing.T) {
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{AgentGraphExecutionTimeout: 30 * time.Minute})
	jm.BuildGraphNodes(
		[]string{"start", "work", "feedback-condition"},
		[][2]string{{"start", "work"}, {"work", "feedback-condition"}, {"feedback-condition", "work"}},
	)
	conditionFalse := false
	jm.SetEdgeConditions([]EdgeCondition{
		{From: "feedback-condition", To: "work", Condition: &conditionFalse},
	})

	jm.Done("start")
	ready := jm.Ready()
	if len(ready) != 1 || ready[0] != "work" {
		t.Fatalf("dormant feedback edge blocked initial path: ready=%v", ready)
	}
}

func TestJobManager_Fail(t *testing.T) {
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{AgentGraphExecutionTimeout: 30 * time.Minute})
	jm.BuildGraphNodes([]string{"A", "B"}, [][2]string{{"A", "B"}})
	jm.Fail("A")

	if !jm.HasFailed() {
		t.Fatal("expected HasFailed true")
	}

	ready := jm.Ready()
	if len(ready) != 0 {
		t.Fatalf("expected no ready nodes after fail, got %v", ready)
	}
}

func TestJobManager_Inject(t *testing.T) {
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{AgentGraphExecutionTimeout: 30 * time.Minute})
	jm.BuildGraphNodes([]string{"A", "B"}, [][2]string{{"A", "B"}})
	jm.Done("A")

	ready := jm.Ready()
	if len(ready) != 1 || ready[0] != "B" {
		t.Fatalf("expected B ready, got %v", ready)
	}

	jm.Inject("D", []string{"A"})
	ready = jm.Ready()
	if len(ready) != 2 {
		t.Fatalf("expected B and D ready, got %v", ready)
	}
}

func TestJobManager_EdgeCondition(t *testing.T) {
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{AgentGraphExecutionTimeout: 30 * time.Minute})
	jm.BuildGraphNodes([]string{"A", "cond", "B", "C"},
		[][2]string{{"A", "cond"}, {"cond", "B"}, {"cond", "C"}})

	condTrue := true
	jm.SetEdgeConditions([]EdgeCondition{
		{From: "cond", To: "B", Condition: &condTrue},
	})

	jm.Done("A")
	ready := jm.Ready()
	if len(ready) != 1 || ready[0] != "cond" {
		t.Fatalf("expected cond ready, got %v", ready)
	}

	jm.SetConditionResult("cond", true)
	jm.Done("cond")

	cond := jm.GetChildCondition("cond", "B")
	if cond == nil || *cond != true {
		t.Fatal("expected condition true for edge cond->B")
	}
}

func TestJobManager_ConditionResult(t *testing.T) {
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{AgentGraphExecutionTimeout: 30 * time.Minute})
	jm.BuildGraphNodes([]string{"A", "cond", "B"}, [][2]string{{"A", "cond"}, {"cond", "B"}})
	jm.SetConditionResult("cond", true)

	result, ok := jm.ConditionResult("cond")
	if !ok || result != true {
		t.Fatalf("expected true, got %v (ok=%v)", result, ok)
	}

	_, ok = jm.ConditionResult("nonexistent")
	if ok {
		t.Fatal("expected ok=false for nonexistent node")
	}
}

func TestRetryWithBackoff_Success(t *testing.T) {
	ctx := context.Background()
	calls := 0
	err := RetryWithBackoff(ctx, RetryPolicy{Max: 3, Initial: 1 * time.Millisecond, Factor: 1, MaxDelay: 10 * time.Millisecond}, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetryWithBackoff_RetriesThenSuccess(t *testing.T) {
	ctx := context.Background()
	calls := 0
	err := RetryWithBackoff(ctx, RetryPolicy{Max: 3, Initial: 1 * time.Millisecond, Factor: 1, MaxDelay: 10 * time.Millisecond}, func() error {
		calls++
		if calls <= 2 {
			return errors.New("fail")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryWithBackoff_Exhausted(t *testing.T) {
	ctx := context.Background()
	err := RetryWithBackoff(ctx, RetryPolicy{Max: 2, Initial: 1 * time.Millisecond, Factor: 1, MaxDelay: 10 * time.Millisecond}, func() error {
		return errors.New("always fail")
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRetryWithBackoff_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RetryWithBackoff(ctx, RetryPolicy{Max: 3, Initial: time.Second, Factor: 1, MaxDelay: time.Second}, func() error {
		return errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestModelRetry_Nil(t *testing.T) {
	rp := ModelRetry(nil)
	if rp.Max != 3 {
		t.Errorf("default max should be 3, got %d", rp.Max)
	}
	if rp.Initial != time.Second {
		t.Errorf("default initial should be 1s, got %v", rp.Initial)
	}
}

type retryRecordingState struct {
	tasks map[string]entities.NodeRun
}

func (s *retryRecordingState) UpdateGraphRun(context.Context, *entities.GraphRun) error { return nil }

func (s *retryRecordingState) SaveNodeRun(_ context.Context, noderun *entities.NodeRun) error {
	if s.tasks == nil {
		s.tasks = make(map[string]entities.NodeRun)
	}
	s.tasks[noderun.ID] = *noderun
	return nil
}

type retryResourceManager struct {
	failures int
	calls    int
}

func (r *retryResourceManager) Provider() engine.Provider      { return engine.ProviderStandalone }
func (r *retryResourceManager) Validate(context.Context) error { return nil }
func (r *retryResourceManager) Shutdown(context.Context) error { return nil }
func (r *retryResourceManager) Schedule(_ context.Context, _ *entities.ExecutionPlan) (*entities.NodeResult, error) {
	r.calls++
	if r.calls <= r.failures {
		return &entities.NodeResult{Error: "transient failure"}, nil
	}
	return &entities.NodeResult{Output: map[string]any{"ok": true}}, nil
}

func TestScheduleNodeWithRetryPersistsEachAttempt(t *testing.T) {
	state := &retryRecordingState{}
	rm := &retryResourceManager{failures: 2}
	jm := NewJobMaster(state, rm, utils.NewLogger("TEXT", "ERROR"), &JobManagerConfig{MaxNodeRetries: 3})
	jm.tracer = tracing.Tracer("test/jobmaster")
	plan := &entities.ExecutionPlan{
		PlanID: "plan-1", NodeRunID: "noderun-base", GraphRunID: "run-1",
		AgentGraphName: "graph-1", NodeInstanceID: "review", NodeKind: entities.AgentNode,
		Node: &entities.GraphNode{Retry: &entities.RetryPolicy{
			Max: 2, Initial: utils.UnitDuration{Duration: time.Millisecond},
			MaxDelay: utils.UnitDuration{Duration: time.Millisecond}, Factor: 1,
		}},
	}

	result, err := jm.scheduleNodeWithRetry(context.Background(), plan)
	if err != nil {
		t.Fatalf("scheduleNodeWithRetry: %v", err)
	}
	if result.Output["ok"] != true || rm.calls != 3 {
		t.Fatalf("result = %+v, calls = %d", result, rm.calls)
	}
	if len(state.tasks) != 3 {
		t.Fatalf("persisted attempts = %d", len(state.tasks))
	}
	var attempts [3]entities.NodeRun
	for _, noderun := range state.tasks {
		attempts[noderun.Attempt-1] = noderun
	}
	for index, noderun := range attempts {
		if noderun.Sequence != index+1 || noderun.MaxRetries != 2 || noderun.StartedAt == nil || noderun.FinishedAt == nil {
			t.Fatalf("attempt %d = %+v", index+1, noderun)
		}
		expectedStatus := entities.NodeRunFailed
		if index == 2 {
			expectedStatus = entities.NodeRunSucceeded
		}
		if noderun.Status != expectedStatus {
			t.Fatalf("attempt %d status = %s", index+1, noderun.Status)
		}
		if index > 0 && noderun.ParentNodeRunID != attempts[index-1].ID {
			t.Fatalf("attempt %d parent = %q", index+1, noderun.ParentNodeRunID)
		}
	}
}

func TestScheduleNodeWithoutRetryRunsOnce(t *testing.T) {
	state := &retryRecordingState{}
	rm := &retryResourceManager{failures: 1}
	jm := NewJobMaster(state, rm, utils.NewLogger("TEXT", "ERROR"), &JobManagerConfig{MaxNodeRetries: 3})
	jm.tracer = tracing.Tracer("test/jobmaster")
	plan := &entities.ExecutionPlan{
		PlanID: "plan-1", NodeRunID: "noderun-base", GraphRunID: "run-1",
		AgentGraphName: "graph-1", NodeInstanceID: "review", NodeKind: entities.AgentNode,
		Node: &entities.GraphNode{},
	}

	if _, err := jm.scheduleNodeWithRetry(context.Background(), plan); err == nil {
		t.Fatal("expected failure")
	}
	if rm.calls != 1 || len(state.tasks) != 1 {
		t.Fatalf("calls = %d, attempts = %d", rm.calls, len(state.tasks))
	}
}
