package engine

import (
	"context"
	"log/slog"
)

// LocalScheduler runs tasks in the same process using a goroutine pool.
// It is the default scheduler for dev/test and all-in-one deployments.
// Resource management is handled by a semaphore that limits concurrency.
type LocalScheduler struct {
	taskManager *TaskManager
	sem         chan struct{}
}

func NewLocalScheduler(tm *TaskManager, poolSize int) *LocalScheduler {
	if poolSize <= 0 {
		poolSize = 10
	}
	return &LocalScheduler{
		taskManager: tm,
		sem:         make(chan struct{}, poolSize),
	}
}

func (s *LocalScheduler) Type() SchedulerType { return SchedulerTypeLocal }

// SubmitTask acquires a slot from the goroutine pool and runs the task
// via TaskManager.ExecuteNode. Blocks until execution completes.
func (s *LocalScheduler) SubmitTask(ctx context.Context, submit *TaskSubmit) (*TaskResult, error) {
	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	slog.Debug("local scheduler executing task",
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

func (s *LocalScheduler) Close() error { return nil }
