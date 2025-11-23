package engine

import (
	"context"
	"fmt"

	"github.com/flowgent-labs/flowgent/src/model"
)

// StateMachine enforces valid run/task lifecycle transitions.
type StateMachine struct {
	store Store
}

func NewStateMachine(store Store) *StateMachine {
	return &StateMachine{store: store}
}

func (sm *StateMachine) TransitionRun(ctx context.Context, run *model.AgentFlowRun, to model.RunStatus) error {
	switch run.Status {
	case model.RunPending:
		if to != model.RunRunning {
			return fmt.Errorf("invalid transition: %s -> %s", run.Status, to)
		}
	case model.RunRunning:
		if to != model.RunCompleted && to != model.RunFailed && to != model.RunPaused {
			return fmt.Errorf("invalid transition: %s -> %s", run.Status, to)
		}
	case model.RunPaused:
		if to != model.RunRunning && to != model.RunFailed {
			return fmt.Errorf("invalid transition: %s -> %s", run.Status, to)
		}
	default:
		return fmt.Errorf("cannot transition from terminal state %s", run.Status)
	}
	run.Status = to
	return sm.store.UpdateAgentFlowRun(ctx, run)
}

func (sm *StateMachine) TransitionTask(ctx context.Context, task *model.TaskRun, to model.TaskStatus) error {
	task.Status = to
	return sm.store.UpdateTaskRun(ctx, task)
}
