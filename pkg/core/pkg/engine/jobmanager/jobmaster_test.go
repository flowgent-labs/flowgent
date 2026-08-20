package jobmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func TestJobManager_BasicTopology(t *testing.T) {
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{FlowExecutionTimeout: 30 * time.Minute})
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
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{FlowExecutionTimeout: time.Minute})
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "canonical-node-kind", Namespace: "test"},
		Nodes:      []entities.Node{{ID: "condition", Kind: entities.ConditionNode}},
	}

	if err := jm.buildExecutionGraph(flow, "run-1"); err != nil {
		t.Fatal(err)
	}
	plan := jm.planMap["condition"]
	if plan == nil || plan.TaskType != entities.TaskCondition || plan.NodeSpec.Kind != entities.ConditionNode {
		t.Fatalf("canonical node kind was not preserved: %#v", plan)
	}
}

func TestJobManager_ParallelReady(t *testing.T) {
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{FlowExecutionTimeout: 30 * time.Minute})
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
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{FlowExecutionTimeout: 30 * time.Minute})
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
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{FlowExecutionTimeout: 30 * time.Minute})
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
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{FlowExecutionTimeout: 30 * time.Minute})
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
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{FlowExecutionTimeout: 30 * time.Minute})
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
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{FlowExecutionTimeout: 30 * time.Minute})
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
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{FlowExecutionTimeout: 30 * time.Minute})
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
	jm := NewJobMaster(nil, nil, nil, &JobManagerConfig{FlowExecutionTimeout: 30 * time.Minute})
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
	tasks map[string]entities.TaskRunInfo
}

func (s *retryRecordingState) UpdateRun(context.Context, *entities.FlowRunInfo) error { return nil }

func (s *retryRecordingState) SaveTask(_ context.Context, task *entities.TaskRunInfo) error {
	if s.tasks == nil {
		s.tasks = make(map[string]entities.TaskRunInfo)
	}
	s.tasks[task.ID] = *task
	return nil
}

type retryResourceManager struct {
	failures int
	calls    int
}

func (r *retryResourceManager) Provider() engine.Provider      { return engine.ProviderStandalone }
func (r *retryResourceManager) Validate(context.Context) error { return nil }
func (r *retryResourceManager) Shutdown(context.Context) error { return nil }
func (r *retryResourceManager) Schedule(_ context.Context, _ *entities.ExecutionPlan) (*entities.TaskResult, error) {
	r.calls++
	if r.calls <= r.failures {
		return &entities.TaskResult{Error: "transient failure"}, nil
	}
	return &entities.TaskResult{Output: map[string]any{"ok": true}}, nil
}

func TestScheduleNodeWithRetryPersistsEachAttempt(t *testing.T) {
	state := &retryRecordingState{}
	rm := &retryResourceManager{failures: 2}
	jm := NewJobMaster(state, rm, utils.NewLogger("TEXT", "ERROR"), &JobManagerConfig{MaxNodeRetries: 3})
	jm.tracer = tracing.Tracer("test/jobmaster")
	plan := &entities.ExecutionPlan{
		PlanID: "plan-1", TaskID: "task-base", AgentFlowRunID: "run-1",
		AgentFlowDefinitionID: "flow-1", NodeID: "review", TaskType: entities.TaskAgent,
		NodeSpec: &entities.NodeSpec{Retry: &entities.RetryPolicy{
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
	var attempts [3]entities.TaskRunInfo
	for _, task := range state.tasks {
		attempts[task.RetryCount] = task
	}
	for index, task := range attempts {
		if task.Sequence != index+1 || task.MaxRetries != 2 || task.StartedAt == nil || task.FinishedAt == nil {
			t.Fatalf("attempt %d = %+v", index+1, task)
		}
		expectedStatus := entities.Failed
		if index == 2 {
			expectedStatus = entities.Success
		}
		if task.Status != expectedStatus {
			t.Fatalf("attempt %d status = %s", index+1, task.Status)
		}
		if index > 0 && task.ParentTaskRunID != attempts[index-1].ID {
			t.Fatalf("attempt %d parent = %q", index+1, task.ParentTaskRunID)
		}
	}
}

func TestScheduleNodeWithoutRetryRunsOnce(t *testing.T) {
	state := &retryRecordingState{}
	rm := &retryResourceManager{failures: 1}
	jm := NewJobMaster(state, rm, utils.NewLogger("TEXT", "ERROR"), &JobManagerConfig{MaxNodeRetries: 3})
	jm.tracer = tracing.Tracer("test/jobmaster")
	plan := &entities.ExecutionPlan{
		PlanID: "plan-1", TaskID: "task-base", AgentFlowRunID: "run-1",
		AgentFlowDefinitionID: "flow-1", NodeID: "review", TaskType: entities.TaskAgent,
		NodeSpec: &entities.NodeSpec{},
	}

	if _, err := jm.scheduleNodeWithRetry(context.Background(), plan); err == nil {
		t.Fatal("expected failure")
	}
	if rm.calls != 1 || len(state.tasks) != 1 {
		t.Fatalf("calls = %d, attempts = %d", rm.calls, len(state.tasks))
	}
}
