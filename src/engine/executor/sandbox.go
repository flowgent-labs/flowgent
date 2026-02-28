package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/queue"
)

// SandboxExecutor dispatches ExecutionPlans to sandbox workers via a queue
// and waits for results. The JM/TM pushes the plan to flowgent/sandbox/exec
// and blocks on flowgent/sandbox/result/{planID}.
//
// Communication is always through the queue (local memory or MQTT) — never inline.
type SandboxExecutor struct {
	queue  queue.Queue
	policy *model.SandboxPolicy
}

func NewSandboxExecutor(q queue.Queue, policy *model.SandboxPolicy) *SandboxExecutor {
	return &SandboxExecutor{queue: q, policy: policy}
}

func (e *SandboxExecutor) TaskType() model.TaskType { return model.TaskSandbox }

func (e *SandboxExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	if e.policy != nil && plan.NodeSpec != nil && plan.NodeSpec.NetworkPolicy == nil {
		plan.NodeSpec.NetworkPolicy = &e.policy.Network
	}

	payload, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("sandbox marshal plan: %w", err)
	}

	msg := &queue.Message{
		ID:      plan.PlanID,
		Topic:   "flowgent/sandbox/exec",
		Payload: payload,
	}
	if err := e.queue.Push(ctx, msg); err != nil {
		return nil, fmt.Errorf("sandbox push: %w", err)
	}

	resultTopic := "flowgent/sandbox/result/" + plan.PlanID
	deadline := 10 * time.Minute
	waitCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	for {
		select {
		case <-waitCtx.Done():
			return &model.TaskResult{Error: "sandbox execution timeout waiting for result"}, nil
		default:
		}

		resultMsg, err := e.queue.Pop(waitCtx, 2*time.Second)
		if err != nil || resultMsg == nil {
			continue
		}
		if resultMsg.Topic != resultTopic {
			continue
		}

		var result model.TaskResult
		if err := json.Unmarshal(resultMsg.Payload, &result); err != nil {
			continue
		}
		_ = e.queue.Ack(waitCtx, resultMsg.ID)
		return &result, nil
	}
}
