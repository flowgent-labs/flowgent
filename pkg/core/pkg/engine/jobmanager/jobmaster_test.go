package jobmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"time"
)

func TestJobManager_BasicTopology(t *testing.T) {
	jm := NewJobMaster(nil, nil, nil, &config.FlowgentConfig{Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "30m"}})
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

func TestJobManager_ParallelReady(t *testing.T) {
	jm := NewJobMaster(nil, nil, nil, &config.FlowgentConfig{Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "30m"}})
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
	jm := NewJobMaster(nil, nil, nil, &config.FlowgentConfig{Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "30m"}})
	jm.BuildGraphNodes([]string{"A", "B", "C"}, [][2]string{{"A", "B"}, {"B", "C"}})
	jm.Skip("B")

	ready := jm.Ready()
	if len(ready) != 2 {
		t.Fatalf("expected A and C ready (B skipped), got %v", ready)
	}

	jm.Done("A")
	ready = jm.Ready()
	if len(ready) != 1 || ready[0] != "C" {
		t.Fatalf("expected C ready, got %v", ready)
	}
}

func TestJobManager_Fail(t *testing.T) {
	jm := NewJobMaster(nil, nil, nil, &config.FlowgentConfig{Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "30m"}})
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
	jm := NewJobMaster(nil, nil, nil, &config.FlowgentConfig{Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "30m"}})
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
	jm := NewJobMaster(nil, nil, nil, &config.FlowgentConfig{Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "30m"}})
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
	jm := NewJobMaster(nil, nil, nil, &config.FlowgentConfig{Orchestration: config.OrchestrationConfig{FlowExecutionTimeout: "30m"}})
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
