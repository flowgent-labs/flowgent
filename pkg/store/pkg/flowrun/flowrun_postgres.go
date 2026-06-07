package flowrun

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg"
)

// FlowRunPostgresStore wraps store.PostgresGenericStore[model.AgentFlowRun].
type FlowRunPostgresStore struct {
	inner *store.PostgresGenericStore[model.AgentFlowRun]
}

func NewFlowRunPostgresStore(pool *pgxpool.Pool) *FlowRunPostgresStore {
	return &FlowRunPostgresStore{
		inner: &store.PostgresGenericStore[model.AgentFlowRun]{
			Pool: pool, Table: "agentflow_runs", IDCol: "id",
		},
	}
}

func (s *FlowRunPostgresStore) Get(ctx context.Context, id string) (*model.AgentFlowRun, error) {
	return s.inner.Get(ctx, id)
}
func (s *FlowRunPostgresStore) Select(ctx context.Context, req model.PageRequest) (*model.Page[model.AgentFlowRun], error) {
	return s.inner.Select(ctx, req)
}
func (s *FlowRunPostgresStore) Save(ctx context.Context, e *model.AgentFlowRun) error {
	return s.inner.Save(ctx, e)
}
func (s *FlowRunPostgresStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

// Create generates a UUID and sets timestamps before inserting.
func (s *FlowRunPostgresStore) Create(ctx context.Context, e *model.AgentFlowRun) error {
	e.ID = uuid.New().String()
	now := time.Now().UTC()
	e.CreatedAt = now
	e.UpdatedAt = now
	return s.inner.Save(ctx, e)
}

// Update performs a targeted update of mutable columns.
func (s *FlowRunPostgresStore) Update(ctx context.Context, e *model.AgentFlowRun) error {
	_, err := s.inner.Pool.Exec(ctx,
		`UPDATE agentflow_runs SET status=$1, vars=$2, output=$3, error=$4, started_at=$5, finished_at=$6, shared_memory=$7, updated_at=NOW() WHERE id=$8`,
		e.Status, e.Vars, e.Output, e.Error, e.StartedAt, e.FinishedAt, e.SharedMemory, e.ID)
	return err
}

// Cancel sets the run status to CANCELLED.
func (s *FlowRunPostgresStore) Cancel(ctx context.Context, id string) error {
	_, err := s.inner.Pool.Exec(ctx,
		`UPDATE agentflow_runs SET status='CANCELLED', updated_at=NOW() WHERE id=$1`, id)
	return err
}
