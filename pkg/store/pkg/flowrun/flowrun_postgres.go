package flowrun

import (
	"context"
	"encoding/json"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FlowRunPostgresStore wraps store.PostgresGenericStore[entities.FlowRunInfo].
type FlowRunPostgresStore struct {
	inner *store.PostgresGenericStore[entities.FlowRunInfo]
}

func NewFlowRunPostgresStore(pool *pgxpool.Pool) *FlowRunPostgresStore {
	return &FlowRunPostgresStore{
		inner: &store.PostgresGenericStore[entities.FlowRunInfo]{
			Pool: pool, Table: "orh_flowrun", IDCol: "id",
		},
	}
}

func (s *FlowRunPostgresStore) Get(ctx context.Context, id string) (*entities.FlowRunInfo, error) {
	return s.inner.Get(ctx, id)
}
func (s *FlowRunPostgresStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.FlowRunInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *FlowRunPostgresStore) HasActiveForFlow(ctx context.Context, namespace, flowID string) (bool, error) {
	var active bool
	err := s.inner.Pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM orh_flowrun
		WHERE namespace=$1 AND agentflow_id=$2 AND del_flag=FALSE
		  AND status IN ('PENDING','RUNNING','PAUSED')
	)`, namespace, flowID).Scan(&active)
	return active, err
}
func (s *FlowRunPostgresStore) HasActiveForPool(ctx context.Context, namespace, poolID string) (bool, error) {
	var active bool
	err := s.inner.Pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM orh_flowrun
		WHERE namespace=$1 AND resource_pool_id=$2 AND del_flag=FALSE
		  AND status IN ('PENDING','RUNNING','PAUSED')
	)`, namespace, poolID).Scan(&active)
	return active, err
}
func (s *FlowRunPostgresStore) Save(ctx context.Context, e *entities.FlowRunInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *FlowRunPostgresStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

// Create generates a UUID and sets timestamps before inserting.
func (s *FlowRunPostgresStore) Create(ctx context.Context, e *entities.FlowRunInfo) error {
	e.ID = uuid.New().String()
	now := time.Now().UTC()
	e.CreatedAt = now
	e.UpdatedAt = now
	return s.inner.Save(ctx, e)
}

// Update performs a targeted update of mutable columns. Vars/Output are
// JSON-encoded explicitly (rather than relying on pgx's jsonb type
// inference) to mirror FlowRunSQLiteStore.Update and stay driver-agnostic.
func (s *FlowRunPostgresStore) Update(ctx context.Context, e *entities.FlowRunInfo) error {
	vars, err := json.Marshal(e.Vars)
	if err != nil {
		return err
	}
	output, err := json.Marshal(e.Output)
	if err != nil {
		return err
	}
	_, err = s.inner.Pool.Exec(ctx,
		`UPDATE orh_flowrun SET status=$1, vars=$2, output=$3, error=$4, started_at=$5, finished_at=$6, updated_at=NOW() WHERE id=$7`,
		e.Status, vars, output, e.Error, e.StartedAt, e.FinishedAt, e.ID)
	return err
}

// Cancel sets the run status to CANCELLED.
func (s *FlowRunPostgresStore) Cancel(ctx context.Context, id string) error {
	_, err := s.inner.Pool.Exec(ctx,
		`UPDATE orh_flowrun SET status='CANCELLED', updated_at=NOW() WHERE id=$1`, id)
	return err
}
