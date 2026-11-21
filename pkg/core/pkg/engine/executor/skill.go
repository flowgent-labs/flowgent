package executor

import (
	"context"
	"log"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

type SkillExecutor struct{}

func NewSkillExecutor() *SkillExecutor               { return &SkillExecutor{} }
func (e *SkillExecutor) NodeKind() entities.NodeKind { return entities.SkillNode }
func (e *SkillExecutor) Execute(ctx context.Context, plan *entities.ExecutionPlan, scope map[string]map[string]any) (*entities.NodeResult, error) {
	log.Printf("WARNING: SkillExecutor is a stub — skill node %q will not execute sub-graphs", plan.Node.Skill)
	return &entities.NodeResult{Output: map[string]any{"status": "dispatched", "skill": plan.Node.Skill}}, nil
}
