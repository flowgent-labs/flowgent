package engine

import (
	"context"
	"log/slog"

	"github.com/flowgent-labs/flowgent/src/model"
)

// LocalScheduler runs ExecutionPlans in-process via a goroutine pool.
// Used in all-in-one / daemon mode. Resource management is the semaphore.
type LocalScheduler struct {
	tm        *TaskManager
	poolSize  int
	sem       chan struct{}
}

func NewLocalScheduler(cfg *SchedulerConfig) (*LocalScheduler, error) {
	poolSize := cfg.PoolSize
	if poolSize <= 0 {
		poolSize = 10
	}
	return &LocalScheduler{
		tm:       cfg.TaskManager,
		poolSize: poolSize,
		sem:      make(chan struct{}, poolSize),
	}, nil
}

func (s *LocalScheduler) Type() SchedulerType { return SchedulerTypeLocal }

func (s *LocalScheduler) EnsureCapacity(ctx context.Context, neededSlots int) (int, error) {
	if neededSlots > s.poolSize {
		neededSlots = s.poolSize
	}
	return neededSlots, nil
}

func (s *LocalScheduler) SubmitTask(ctx context.Context, plan *model.ExecutionPlan) (*model.TaskResult, error) {
	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	slog.Debug("local scheduler executing plan",
		"plan_id", plan.PlanID,
		"node_id", plan.NodeID,
	)

	task := &model.TaskRun{
		AgentFlowRunID: plan.AgentFlowRunID,
		NodeID:         plan.NodeID,
		Status:         model.TaskPending,
		ExecID:         plan.PlanID,
	}
	if _, err := s.tm.ExecutePlan(ctx, plan, task); err != nil {
		return &model.TaskResult{Error: err.Error()}, nil
	}
	return &model.TaskResult{Output: task.Output}, nil
}

func (s *LocalScheduler) AvailableSlots() int {
	return s.poolSize - len(s.sem)
}

func (s *LocalScheduler) TotalSlots() int { return s.poolSize }
func (s *LocalScheduler) Shutdown(ctx context.Context) error { return nil }
