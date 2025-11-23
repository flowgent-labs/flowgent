package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// MapRunner handles concurrent fan-out execution of map nodes.
type MapRunner struct {
	executor *Executor
	store    Store
	logger   *util.Logger
}

func newMapRunner(exec *Executor, store Store, logger *util.Logger) *MapRunner {
	return &MapRunner{executor: exec, store: store, logger: logger}
}

func (m *MapRunner) runMap(ctx context.Context, task *model.TaskRun, node *model.Node, scope map[string]map[string]any) error {
	ctx, span := otel.Tracer("flowgent/engine").Start(ctx, "map.run",
		trace.WithAttributes(
			attribute.String("node.id", node.ID),
			attribute.Int("concurrency", node.Concurrency),
		),
	)
	defer span.End()

	source := util.Resolve(node.Source, scope)
	items, ok := source.([]any)
	if !ok {
		return fmt.Errorf("map source is not an array: %v", source)
	}

	concurrency := node.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]map[string]any, len(items))
	var firstErr error

	for i, item := range items {
		wg.Add(1)
		go func(idx int, it any) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			innerScope := make(map[string]map[string]any)
			for k, v := range scope {
				innerScope[k] = v
			}
			innerScope["item"] = map[string]any{"item": it, "index": idx}

			innerTask := &model.TaskRun{
				AgentFlowRunID: task.AgentFlowRunID,
				NodeID:         fmt.Sprintf("%s[%d]", node.ID, idx),
				Status:         model.TaskPending,
				ExecID:         fmt.Sprintf("%s-%d", task.ExecID, idx),
			}
			m.store.CreateTaskRun(ctx, innerTask)

			if node.Node != nil {
				m.executor.executeNode(ctx, innerTask, node.Node, innerScope)
			}

			mu.Lock()
			if innerTask.Status == model.Success && innerTask.Output != nil {
				results[idx] = innerTask.Output
			}
			if innerTask.Error != "" && firstErr == nil {
				firstErr = errors.New(innerTask.Error)
			}
			mu.Unlock()
		}(i, item)
	}

	wg.Wait()
	task.Output = map[string]any{"results": results}
	if firstErr != nil {
		task.Error = firstErr.Error()
		task.Status = model.Failed
		return firstErr
	}
	task.Status = model.Success
	return nil
}
