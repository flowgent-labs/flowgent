package executor

import (
	"context"
	"github.com/flowgent-labs/flowgent/model/src"
)

type SkillExecutor struct{}

func NewSkillExecutor() *SkillExecutor            { return &SkillExecutor{} }
func (e *SkillExecutor) TaskType() model.TaskType { return model.TaskSkill }
func (e *SkillExecutor) Execute(ctx context.Context, plan *model.ExecutionPlan, scope map[string]map[string]any) (*model.TaskResult, error) {
	return &model.TaskResult{Output: map[string]any{"status": "dispatched", "skill": plan.NodeSpec.Skill}}, nil
}
