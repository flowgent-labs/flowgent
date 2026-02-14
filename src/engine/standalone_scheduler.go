package engine

import (
	"context"
	"log/slog"
)

// StandaloneScheduler runs tasks in a local goroutine pool.
// Designed for dev/test and all-in-one deployment mode.
// In Flink terms this is a Standalone cluster with a fixed TaskManager pool.
type StandaloneScheduler struct {
	taskManager *TaskManager
	sem         chan struct{}
}

func NewStandaloneScheduler(tm *TaskManager, poolSize int) *StandaloneScheduler {
	if poolSize <= 0 {
		poolSize = 10
	}
	return &StandaloneScheduler{
		taskManager: tm,
		sem:         make(chan struct{}, poolSize),
	}
}

func (s *StandaloneScheduler) Type() SchedulerType { return SchedulerTypeStandalone }

// SubmitTask acquires a slot from the goroutine pool and runs the task
// via TaskManager.ExecuteNode. Blocks until execution completes.
func (s *StandaloneScheduler) SubmitTask(ctx context.Context, submit *TaskSubmit) (*TaskResult, error) {
	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	slog.Debug("standalone scheduler executing task",
		"cluster_id", submit.ClusterID,
		"run_id", submit.RunID,
		"node_id", submit.NodeID,
		"agentflow_id", submit.AgentFlowID,
	)

	scope := map[string]map[string]any{"input": submit.Input}

	task, err := s.taskManager.store.GetTaskRun(ctx, submit.TaskID)
	if err != nil || task == nil {
		return nil, err
	}

	if err := s.taskManager.ExecuteNode(ctx, task, submit.Node, scope); err != nil {
		return &TaskResult{Error: err.Error()}, nil
	}
	return &TaskResult{Output: task.Output}, nil
}

func (s *StandaloneScheduler) Close() error { return nil }
