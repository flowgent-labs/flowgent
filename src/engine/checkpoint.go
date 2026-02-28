package engine

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/src/model"
)

// Checkpointer persists ExecutionPlan checkpoints.
// Default strategy: CheckpointPerTask — persist at task boundary.
// AgentExecutor can call Save in its inner loop for incremental checkpoints.
type Checkpointer struct {
	store Store
}

func NewCheckpointer(store Store) *Checkpointer {
	return &Checkpointer{store: store}
}

// Save persists the current checkpoint from the plan.
func (c *Checkpointer) Save(ctx context.Context, plan *model.ExecutionPlan) error {
	if plan.Checkpoint == nil {
		plan.Checkpoint = &model.TaskCheckpoint{}
	}
	plan.Checkpoint.CheckpointedAt = time.Now()
	return c.store.SaveCheckpoint(ctx, plan.PlanID, plan.Checkpoint)
}

// Load retrieves the checkpoint for a given plan, or nil if none exists.
func (c *Checkpointer) Load(ctx context.Context, planID string) (*model.TaskCheckpoint, error) {
	return c.store.LoadCheckpoint(ctx, planID)
}

// Restore restores a plan's checkpoint from the store.
func (c *Checkpointer) Restore(ctx context.Context, plan *model.ExecutionPlan) error {
	cp, err := c.store.LoadCheckpoint(ctx, plan.PlanID)
	if err != nil {
		return err
	}
	if cp != nil {
		plan.Checkpoint = cp
	}
	return nil
}
