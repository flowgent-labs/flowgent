package resourcemanager

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/taskmanager"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// StandaloneResourceManager executes plans in-process via a goroutine pool.
// Implements ResourceManager with a single Schedule() entry point.
type StandaloneResourceManager struct {
	tm       *taskmanager.TaskManager
	poolSize int
	sem      chan struct{}
}

func NewStandaloneResourceManager(cfg *ResourceManagerConfig) (*StandaloneResourceManager, error) {
	poolSize := cfg.PoolSize
	if poolSize <= 0 {
		poolSize = 10
	}
	runtimeClusterID := cfg.RuntimeClusterID
	if runtimeClusterID == "" {
		runtimeClusterID = "local"
	}

	tm, err := taskmanager.NewTaskManager(&taskmanager.TaskManagerConfig{
		ID:           "tm-local",
		SlotCount:    poolSize,
		Messager:     cfg.Messager,
		State:        cfg.TaskState,
		ApprovalInfo: cfg.ApprovalInfo,
		APIServerURL: cfg.APIServerURL,
		Namespace:    cfg.Namespace,
		Logger:       cfg.Logger,

		RuntimeClusterID:         runtimeClusterID,
		SandboxMessager:          cfg.Messager,
		SandboxPolicy:            cfg.SandboxPolicy,
		SandboxWorkspace:         cfg.SandboxWorkspace,
		SandboxDeploymentEnabled: false,
	})
	if err != nil {
		return nil, err
	}
	return &StandaloneResourceManager{
		tm: tm, poolSize: poolSize, sem: make(chan struct{}, poolSize),
	}, nil
}

func (s *StandaloneResourceManager) Provider() engine.Provider { return engine.ProviderStandalone }

func (s *StandaloneResourceManager) Validate(ctx context.Context) error {
	if s.tm == nil {
		return fmt.Errorf("local rm: taskmanager is nil")
	}
	return nil
}

// Schedule acquires a slot (non-blocking), executes the plan via the local TM.
// Returns INSUFFICIENT_RESOURCES if all local slots are occupied.
func (s *StandaloneResourceManager) Schedule(ctx context.Context, plan *entities.ExecutionPlan) (*entities.TaskResult, error) {
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		return nil, fmt.Errorf("INSUFFICIENT_RESOURCES: all %d local slots occupied", s.poolSize)
	}

	slog.Debug("local rm schedule", "plan", plan.PlanID, "node", plan.NodeID)

	task := &entities.TaskRunInfo{
		AgentFlowRunID: plan.AgentFlowRunID, NodeID: plan.NodeID,
		Status: entities.TaskPending, ExecID: plan.PlanID,
	}
	if _, err := s.tm.ExecutePlan(ctx, plan, task); err != nil {
		return &entities.TaskResult{Error: err.Error()}, nil
	}
	return &entities.TaskResult{Output: task.Output}, nil
}

func (s *StandaloneResourceManager) Shutdown(ctx context.Context) error { return nil }

// AvailableSlots returns idle slot count (internal use).
func (s *StandaloneResourceManager) AvailableSlots() int { return s.poolSize - len(s.sem) }
