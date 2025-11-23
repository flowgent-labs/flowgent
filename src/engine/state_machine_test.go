package engine

import (
	"context"
	"testing"

	"github.com/flowgent-labs/flowgent/src/model"
)

func TestStateMachine_ValidTransition(t *testing.T) {
	store := newMockStore()
	sm := NewStateMachine(store)

	run := &model.AgentFlowRun{ID: "r1", Status: model.RunPending}
	if err := sm.TransitionRun(context.Background(), run, model.RunRunning); err != nil {
		t.Fatalf("PENDING→RUNNING should work: %v", err)
	}
	if run.Status != model.RunRunning {
		t.Error("status should be RUNNING")
	}

	if err := sm.TransitionRun(context.Background(), run, model.RunCompleted); err != nil {
		t.Fatalf("RUNNING→COMPLETED should work: %v", err)
	}
}

func TestStateMachine_InvalidTransition(t *testing.T) {
	store := newMockStore()
	sm := NewStateMachine(store)

	run := &model.AgentFlowRun{ID: "r2", Status: model.RunCompleted}
	err := sm.TransitionRun(context.Background(), run, model.RunRunning)
	if err == nil {
		t.Fatal("COMPLETED→RUNNING should be invalid")
	}
	run2 := &model.AgentFlowRun{ID: "r3", Status: model.RunPending}
	err = sm.TransitionRun(context.Background(), run2, model.RunCompleted)
	if err == nil {
		t.Fatal("PENDING→COMPLETED should be invalid")
	}
}

func TestStateMachine_PausedResume(t *testing.T) {
	store := newMockStore()
	sm := NewStateMachine(store)

	run := &model.AgentFlowRun{ID: "r4", Status: model.RunRunning}
	sm.TransitionRun(context.Background(), run, model.RunPaused)
	if run.Status != model.RunPaused {
		t.Fatal("should be PAUSED")
	}
	if err := sm.TransitionRun(context.Background(), run, model.RunRunning); err != nil {
		t.Fatalf("PAUSED→RUNNING should work: %v", err)
	}
}
