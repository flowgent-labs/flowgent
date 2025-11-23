package engine

import "testing"

func TestDAGScheduler_BasicTopology(t *testing.T) {
	s := NewDAGScheduler([]string{"A", "B", "C"}, [][2]string{{"A", "B"}, {"B", "C"}})

	ready := s.Ready()
	if len(ready) != 1 || ready[0] != "A" {
		t.Fatalf("step 1: want [A], got %v", ready)
	}
	s.Done("A")

	ready = s.Ready()
	if len(ready) != 1 || ready[0] != "B" {
		t.Fatalf("step 2: want [B], got %v", ready)
	}
	s.Done("B")

	ready = s.Ready()
	if len(ready) != 1 || ready[0] != "C" {
		t.Fatalf("step 3: want [C], got %v", ready)
	}
	s.Done("C")

	if !s.IsComplete() {
		t.Fatal("should be complete")
	}
}

func TestDAGScheduler_ParallelReady(t *testing.T) {
	// A -> B, A -> C  (B and C both depend on A)
	s := NewDAGScheduler([]string{"A", "B", "C"}, [][2]string{{"A", "B"}, {"A", "C"}})

	ready := s.Ready()
	if len(ready) != 1 || ready[0] != "A" {
		t.Fatalf("want [A], got %v", ready)
	}
	s.Done("A")

	ready = s.Ready()
	if len(ready) != 2 {
		t.Fatalf("want [B C], got %v", ready)
	}
}

func TestDAGScheduler_Skip(t *testing.T) {
	s := NewDAGScheduler([]string{"A", "B", "C"}, [][2]string{{"A", "B"}, {"B", "C"}})
	s.Done("A")
	s.Skip("B")

	if s.IsComplete() {
		t.Fatal("C depends on B which is skipped, but C should still be needed")
	}
}

func TestDAGScheduler_Fail(t *testing.T) {
	s := NewDAGScheduler([]string{"A", "B"}, [][2]string{{"A", "B"}})
	s.Done("A")
	s.Fail("B")
	if !s.HasFailed() {
		t.Fatal("should have failed node")
	}
}

func TestDAGScheduler_Inject(t *testing.T) {
	s := NewDAGScheduler([]string{"A", "B"}, [][2]string{{"A", "B"}})
	s.Done("A")

	// Inject D depending on A (already done, so D should be ready)
	s.Inject("D", []string{"A"})
	ready := s.Ready()
	if len(ready) != 2 { // B and D both ready
		t.Fatalf("want [B D], got %v", ready)
	}
	s.Done("B")
	s.Done("D")
	if !s.IsComplete() {
		t.Fatal("should be complete")
	}
}

func TestDAGScheduler_EdgeCondition(t *testing.T) {
	s := NewDAGScheduler([]string{"A", "cond", "B", "C"},
		[][2]string{{"A", "cond"}, {"cond", "B"}, {"cond", "C"}})
	s.SetEdgeConditions([]EdgeCondition{
		{From: "cond", To: "B", Condition: boolPtr(true)},
		{From: "cond", To: "C", Condition: boolPtr(false)},
	})
	s.Done("A")
	s.Done("cond")

	// Check edge conditions exist
	cond := s.GetChildCondition("cond", "B")
	if cond == nil || !*cond {
		t.Error("expected cond→B condition to be true")
	}
	cond = s.GetChildCondition("cond", "C")
	if cond == nil || *cond {
		t.Error("expected cond→C condition to be false")
	}
}

func TestDAGScheduler_ConditionResult(t *testing.T) {
	s := NewDAGScheduler([]string{"A", "cond", "B"}, [][2]string{{"A", "cond"}, {"cond", "B"}})
	s.SetConditionResult("cond", true)
	result, ok := s.ConditionResult("cond")
	if !ok || !result {
		t.Fatal("condition result should be true")
	}
}

func boolPtr(b bool) *bool { return &b }
