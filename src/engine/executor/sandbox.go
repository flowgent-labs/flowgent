package executor

import (
	"context"
	"github.com/flowgent-labs/flowgent/src/model"
)

type SandboxExecutor struct{}

func NewSandboxExecutor() *SandboxExecutor { return &SandboxExecutor{} }
func (e *SandboxExecutor) TaskType() model.TaskType { return model.TaskSandbox }
func (e *SandboxExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	return &model.TaskResult{Output: map[string]any{
		"status": "sandbox_dispatched", "runtime": plan.NodeSpec.Runtime, "timeout": plan.NodeSpec.Timeout,
	}}, nil
}
