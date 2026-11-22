package executor

import (
	"context"
	"fmt"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"sync"
	"time"
)

// ─── Map Executor ──────────────────────────────────────

type MapExecutor struct{}

func (e *MapExecutor) NodeKind() entities.NodeKind { return entities.MapNode }

func (e *MapExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.NodeResult, error) {
	// Map execution: the JM creates child ExecutionPlans for each item
	// and dispatches them. The MapExecutor just marks the map plan as
	// complete; the actual fan-out is handled by the JM.
	return &entities.NodeResult{Output: map[string]any{"status": "dispatched"}}, nil
}

// ─── MapRunner (kept for inline fan-out within map nodes) ─

type MapRunner struct {
	mu sync.Mutex
}

func newMapRunner() *MapRunner {
	return &MapRunner{}
}

func (m *MapRunner) runMap(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any, router *TaskExecutorRouter) error {
	source := plan.Input["source"]
	items, ok := source.([]any)
	if !ok {
		return fmt.Errorf("map source is not an array")
	}

	concurrency := plan.Node.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	results := make([]any, len(items))
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

			innerPlan := &entities.ExecutionPlan{
				PlanID:         fmt.Sprintf("%s-%d", plan.PlanID, idx),
				GraphRunID:     plan.GraphRunID,
				NodeKind:       entities.AgentNode,
				NodeInstanceID: fmt.Sprintf("%s[%d]", plan.NodeInstanceID, idx),
				Input:          map[string]any{"item": it, "index": idx},
				Node:           plan.Node.Node,
				CreatedAt:      time.Now(),
			}

			result, err := router.Execute(ctx, innerPlan, innerScope)
			if err != nil && firstErr == nil {
				firstErr = err
			}
			if result != nil {
				results[idx] = result.Output
			}
		}(i, item)
	}

	wg.Wait()
	plan.Result = &entities.NodeResult{Output: map[string]any{"results": results}}
	if firstErr != nil {
		return firstErr
	}
	return nil
}
