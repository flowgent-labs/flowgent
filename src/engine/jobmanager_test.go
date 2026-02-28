package engine

import (
	"testing"
)

func TestJobManager_BasicTopology(t *testing.T) {
	jm := NewJobManager(nil, nil, nil, nil)
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
	jm := NewJobManager(nil, nil, nil, nil)
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
	jm := NewJobManager(nil, nil, nil, nil)
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
	jm := NewJobManager(nil, nil, nil, nil)
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
	jm := NewJobManager(nil, nil, nil, nil)
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
	jm := NewJobManager(nil, nil, nil, nil)
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
	jm := NewJobManager(nil, nil, nil, nil)
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
