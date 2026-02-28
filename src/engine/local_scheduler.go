package engine

import (
	"context"
	"log/slog"

	"github.com/flowgent-labs/flowgent/src/model"
)

// LocalScheduler executes plans in-process via a goroutine pool.
// It owns the TaskManager internally — callers should NOT create a TM separately.
type LocalScheduler struct {
	tm       *TaskManager
	poolSize int
	sem      chan struct{}
}

func NewLocalScheduler(cfg *SchedulerConfig) (*LocalScheduler, error) {
	poolSize := cfg.PoolSize
	if poolSize <= 0 { poolSize = 10 }

	tm, err := NewTaskManager(&TaskManagerConfig{
		ID: "tm-local", SlotCount: poolSize,
		Store: cfg.Store, Agents: cfg.Agents,
		MCPClients: cfg.MCPClients, LLMClient: cfg.LLMClient,
		Logger: cfg.Logger,
	})
	if err != nil { return nil, err }

	return &LocalScheduler{
		tm: tm, poolSize: poolSize, sem: make(chan struct{}, poolSize),
	}, nil
}

func (s *LocalScheduler) Type() SchedulerType { return SchedulerTypeLocal }

func (s *LocalScheduler) EnsureCapacity(ctx context.Context, needed int) (int, error) {
	if needed > s.poolSize { needed = s.poolSize }
	return needed, nil
}

func (s *LocalScheduler) SubmitTask(ctx context.Context, plan *model.ExecutionPlan) (*model.TaskResult, error) {
	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	slog.Debug("local scheduler execute", "plan", plan.PlanID, "node", plan.NodeID)

	task := &model.TaskRun{
		AgentFlowRunID: plan.AgentFlowRunID, NodeID: plan.NodeID,
		Status: model.TaskPending, ExecID: plan.PlanID,
	}
	if _, err := s.tm.ExecutePlan(ctx, plan, task); err != nil {
		return &model.TaskResult{Error: err.Error()}, nil
	}
	return &model.TaskResult{Output: task.Output}, nil
}

func (s *LocalScheduler) AvailableSlots() int { return s.poolSize - len(s.sem) }
func (s *LocalScheduler) TotalSlots() int     { return s.poolSize }
func (s *LocalScheduler) Shutdown(ctx context.Context) error { return nil }
