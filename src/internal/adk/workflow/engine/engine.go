package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/cyberbot/cve-auto-fix/src/internal/adk/polling"
	"github.com/cyberbot/cve-auto-fix/src/internal/adk/workflow"
)

type Engine struct {
	config *workflow.WorkflowConfig
}

func NewEngine(cfg *workflow.WorkflowConfig) workflow.WorkflowRunner {
	return &Engine{config: cfg}
}

func (e *Engine) Run(ctx context.Context, workflowID string, input map[string]any) (string, error) {
	tasks, ok := e.config.Workflows[workflowID]
	if !ok {
		return "", fmt.Errorf("workflow %s not found", workflowID)
	}

	ctx, _ = polling.WithContext(ctx)
	handoff := ""

	for _, task := range tasks {
		result, err := e.executeTask(ctx, task, handoff)
		if err != nil {
			return handoff, err
		}
		handoff = result
	}
	return handoff, nil
}

func (e *Engine) executeTask(ctx context.Context, task workflow.WorkflowTask, handoff string) (string, error) {
	var output string
	var err error

	switch task.Type {
	case "agent":
		return e.callAgent(ctx, task.AgentRef, task.Params, handoff)
	case "sequential":
		output, err = e.runSequential(ctx, task.Tasks, handoff)
	case "parallel":
		output, err = e.runParallel(ctx, task.Tasks, handoff)
	case "supervisor":
		output, err = e.runSupervisor(ctx, task.AgentRef, task.Params, handoff)
	case "review_board":
		output, err = e.runReviewBoard(ctx, task.AgentRef, task.Params, handoff)
	default:
		err = fmt.Errorf("unknown task type: %s", task.Type)
	}
	return output, err
}

func (e *Engine) runSequential(ctx context.Context, tasks []workflow.WorkflowTask, handoff string) (string, error) {
	var result string
	var err error
	for _, t := range tasks {
		result, err = e.executeTask(ctx, t, handoff)
		if err != nil {
			return result, err
		}
		handoff = result
	}
	return result, nil
}

func (e *Engine) runParallel(ctx context.Context, tasks []workflow.WorkflowTask, handoff string) (string, error) {
	var wg sync.WaitGroup
	results := make([]string, len(tasks))
	errs := make([]error, len(tasks))

	for i, t := range tasks {
		wg.Add(1)
		go func(idx int, task workflow.WorkflowTask) {
			defer wg.Done()
			results[idx], errs[idx] = e.executeTask(ctx, task, handoff)
		}(i, t)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return "", err
		}
	}

	var res string
	for _, r := range results {
		res += r + "\n"
	}
	return res, nil
}

func (e *Engine) runSupervisor(ctx context.Context, agentRef string, params map[string]any, handoff string) (string, error) {
	tr := polling.NewTrace("supervisor", "dynamic_management", ctx)
	tr.Status = "running"

	out, err := e.callAgent(ctx, agentRef, params, handoff)
	if err != nil {
		tr.Status = "failed"
		return out, err
	}

	tr.Status = "completed"
	tr.HandoffData = out
	return out, nil
}

func (e *Engine) runReviewBoard(ctx context.Context, agentRef string, params map[string]any, handoff string) (string, error) {
	tr := polling.NewTrace("review_board", "tripartite_voting", ctx)
	votesCount := 3

	var acceptedCount int

	for i := 0; i < votesCount; i++ {
		out, err := e.callAgent(ctx, agentRef, params, handoff)
		if err != nil {
			return "", err
		}
		if out == "ACCEPT" {
			acceptedCount++
		}
	}

	if acceptedCount > votesCount/2 {
		tr.Status = "review_passed"
		return "APPROVED", nil
	}

	tr.Status = "review_failed"
	return "REJECTED: Majority vote failed", fmt.Errorf("review board rejected the output")
}

func (e *Engine) callAgent(ctx context.Context, agentRef string, params map[string]any, handoff string) (string, error) {
	agent := e.config.AgentSets[agentRef]
	if agent.ID == "" {
		return "", fmt.Errorf("agent %s not found", agentRef)
	}

	tr := polling.NewTrace(agent.ID, "execution", ctx)
	tr.Status = "running_" + agent.ID

	return fmt.Sprintf("Agent %s executed with handoff: %s", agent.ID, handoff), nil
}