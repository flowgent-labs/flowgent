package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/flowgent-labs/flowgent/src/engine"
	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/queue"
	"github.com/flowgent-labs/flowgent/src/util"
)

// Worker is a distributed node executor. It dequeues tasks from the queue,
// executes a single node, and publishes results. Multiple workers can run
// concurrently across different machines, all consuming from the same MQTT
// topic (for k8s distributed mode) or in-memory channel (for local mode).
type Worker struct {
	id         string
	store      engine.Store
	executor   *engine.Executor
	queue      queue.Queue
	logger     *util.Logger
	agentFlows map[string]*model.AgentFlowSpec

	mu       sync.Mutex
	stopCh   chan struct{}
	stopped  bool
}

// Config holds worker configuration.
type Config struct {
	ID       string
	Store    engine.Store
	Executor *engine.Executor
	Queue    queue.Queue
	Logger   *util.Logger
	Flows    map[string]*model.AgentFlowSpec
}

// New creates a new worker.
func New(cfg *Config) *Worker {
	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("worker-%d", time.Now().UnixNano())
	}
	return &Worker{
		id:         cfg.ID,
		store:      cfg.Store,
		executor:   cfg.Executor,
		queue:      cfg.Queue,
		logger:     cfg.Logger,
		agentFlows: cfg.Flows,
		stopCh:     make(chan struct{}),
	}
}

// Start begins the dequeue-execute-publish loop.
func (w *Worker) Start(ctx context.Context) error {
	slog.Info("worker started", "id", w.id)
	defer slog.Info("worker stopped", "id", w.id)

	for {
		select {
		case <-w.stopCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Dequeue a task (blocking)
		msg, err := w.queue.Dequeue(ctx, "workers")
		if err != nil {
			w.logger.Error("worker dequeue error", "id", w.id, "error", err)
			time.Sleep(time.Second)
			continue
		}
		if msg == nil {
			continue
		}

		slog.Debug("worker processing task", "worker_id", w.id, "task_run_id", msg.TaskRunID, "node", msg.NodeID)

		// Execute the task
		if err := w.processTask(ctx, msg); err != nil {
			w.logger.Error("worker task failed", "task", msg.ID, "error", err)
			w.queue.Nack(ctx, msg.ID)
		} else {
			w.queue.Ack(ctx, msg.ID)
		}
	}
}

func (w *Worker) processTask(ctx context.Context, msg *queue.Message) error {
	// Load the task
	task, err := w.store.GetTaskRun(ctx, msg.TaskRunID)
	if err != nil || task == nil {
		return fmt.Errorf("task %s not found", msg.TaskRunID)
	}

	// Load the agentflow run
	run, err := w.store.GetAgentFlowRun(ctx, task.AgentFlowRunID)
	if err != nil || run == nil {
		return fmt.Errorf("run %s not found", task.AgentFlowRunID)
	}

	// Load the agentflow spec
	spec := w.agentFlows[run.AgentFlowID]
	if spec == nil {
		return fmt.Errorf("agentflow spec %s not found", run.AgentFlowID)
	}

	// Find the node
	var node *model.Node
	for i := range spec.Nodes {
		if spec.Nodes[i].ID == task.NodeID {
			node = &spec.Nodes[i]
			break
		}
	}
	if node == nil {
		return fmt.Errorf("node %s not found in spec", task.NodeID)
	}

	// Build scope from task input and run vars
	scope := make(map[string]map[string]any)
	if run.Vars != nil {
		scope["vars"] = run.Vars
	}
	if task.Input != nil {
		scope["input"] = task.Input
	}

	// Execute the node via the executor
	w.executor.SetAgentFlowContext(spec.Description)
	runtime := engine.NewAgentFlowRuntime(w.store, w.logger)
	runtime.SetExecutor(w.executor)

	// Create a single-node spec for isolated execution
	singleSpec := &model.AgentFlowSpec{
		ID:          spec.ID,
		Description: spec.Description,
		Vars:        run.Vars,
		Nodes:       []model.Node{*node},
		Edges:       nil,
	}

	if err := runtime.Execute(ctx, run, singleSpec); err != nil {
		return fmt.Errorf("execute node %s: %w", node.ID, err)
	}

	return nil
}

// Stop signals the worker to stop after completing the current task.
func (w *Worker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.stopped {
		w.stopped = true
		close(w.stopCh)
	}
}

// ID returns the worker identifier.
func (w *Worker) ID() string { return w.id }

// Pool manages multiple concurrent workers.
type Pool struct {
	workers []*Worker
	mu      sync.Mutex
}

// NewPool creates a worker pool.
func NewPool(count int, cfg *Config) *Pool {
	pool := &Pool{}
	for i := 0; i < count; i++ {
		wCfg := *cfg
		wCfg.ID = fmt.Sprintf("worker-%d", i+1)
		pool.workers = append(pool.workers, New(&wCfg))
	}
	return pool
}

// Start launches all workers as goroutines.
func (p *Pool) Start(ctx context.Context) {
	for _, w := range p.workers {
		go w.Start(ctx)
	}
}

// Stop stops all workers.
func (p *Pool) Stop() {
	for _, w := range p.workers {
		w.Stop()
	}
}
