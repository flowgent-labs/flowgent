package executor

import (
	"context"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

type SkillExecutor struct{}

func NewSkillExecutor() *SkillExecutor            { return &SkillExecutor{} }
func (e *SkillExecutor) TaskType() entities.TaskType { return entities.TaskSkill }
func (e *SkillExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.TaskResult, error) {
	return &entities.TaskResult{Output: map[string]any{"status": "dispatched", "skill": plan.NodeSpec.Skill}}, nil
}
