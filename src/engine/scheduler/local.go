package scheduler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/flowgent-labs/flowgent/src/engine"
	"github.com/flowgent-labs/flowgent/src/engine/taskmanager"
	"github.com/flowgent-labs/flowgent/src/model"
)

// LocalResourceManager executes plans in-process via a goroutine pool.
// Implements ResourceManager with a single Schedule() entry point.
type LocalResourceManager struct {
	tm       *taskmanager.TaskManager
	poolSize int
	sem      chan struct{}
}

func NewLocalResourceManager(cfg *ResourceManagerConfig) (*LocalResourceManager, error) {
	poolSize := cfg.PoolSize
	if poolSize <= 0 {
		poolSize = 10
	}
	tm, err := taskmanager.NewTaskManager(&engine.TaskManagerConfig{
		ID: "tm-local", SlotCount: poolSize,
		Store: cfg.Store, Agents: cfg.Agents,
		MCPClients: cfg.MCPClients, LLMClient: cfg.LLMClient,
		Logger: cfg.Logger,
	})
	if err != nil {
		return nil, err
	}
	return &LocalResourceManager{
		tm: tm, poolSize: poolSize, sem: make(chan struct{}, poolSize),
	}, nil
}

func (s *LocalResourceManager) Provider() engine.Provider { return engine.ProviderLocal }

func (s *LocalResourceManager) Validate(ctx context.Context) error {
	if s.tm == nil {
		return fmt.Errorf("local rm: taskmanager is nil")
	}
	return nil
}

// Schedule acquires a slot, executes the plan via the local TM, and returns the result.
func (s *LocalResourceManager) Schedule(ctx context.Context, plan *model.ExecutionPlan) (*model.TaskResult, error) {
	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	slog.Debug("local rm schedule", "plan", plan.PlanID, "node", plan.NodeID)

	task := &model.TaskRun{
		AgentFlowRunID: plan.AgentFlowRunID, NodeID: plan.NodeID,
		Status: model.TaskPending, ExecID: plan.PlanID,
	}
	if _, err := s.tm.ExecutePlan(ctx, plan, task); err != nil {
		return &model.TaskResult{Error: err.Error()}, nil
	}
	return &model.TaskResult{Output: task.Output}, nil
}

func (s *LocalResourceManager) Shutdown(ctx context.Context) error { return nil }

// AvailableSlots returns idle slot count (internal use).
func (s *LocalResourceManager) AvailableSlots() int { return s.poolSize - len(s.sem) }
