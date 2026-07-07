package flowrun

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/google/uuid"
)

// FlowRunSQLiteStore wraps store.SQLiteGenericStore[entities.FlowRunInfo].
type FlowRunSQLiteStore struct {
	inner *store.SQLiteGenericStore[entities.FlowRunInfo]
}

func NewFlowRunSQLiteStore(conn *sql.DB) *FlowRunSQLiteStore {
	return &FlowRunSQLiteStore{
		inner: &store.SQLiteGenericStore[entities.FlowRunInfo]{
			Conn: conn, Table: "orh_flowrun", IDCol: "id",
		},
	}
}
func (s *FlowRunSQLiteStore) Get(ctx context.Context, id string) (*entities.FlowRunInfo, error) {
	return s.inner.Get(ctx, id)
}
func (s *FlowRunSQLiteStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.FlowRunInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *FlowRunSQLiteStore) Save(ctx context.Context, e *entities.FlowRunInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *FlowRunSQLiteStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

// Create generates a UUID and sets timestamps before inserting.
func (s *FlowRunSQLiteStore) Create(ctx context.Context, e *entities.FlowRunInfo) error {
	e.ID = uuid.New().String()
	now := time.Now().UTC()
	e.CreatedAt = now
	e.UpdatedAt = now
	return s.inner.Save(ctx, e)
}

// Update performs a targeted update of mutable columns. Vars/Output are
// JSON-encoded before binding — database/sql (unlike pgx) has no built-in
// support for map[string]any parameters.
func (s *FlowRunSQLiteStore) Update(ctx context.Context, e *entities.FlowRunInfo) error {
	vars, err := json.Marshal(e.Vars)
	if err != nil {
		return err
	}
	output, err := json.Marshal(e.Output)
	if err != nil {
		return err
	}
	_, err = s.inner.Conn.ExecContext(ctx,
		`UPDATE orh_flowrun SET status=?1, vars=?2, output=?3, error=?4, started_at=?5, finished_at=?6, updated_at=CURRENT_TIMESTAMP WHERE id=?7`,
		string(e.Status), vars, output, e.Error, e.StartedAt, e.FinishedAt, e.ID)
	return err
}

// Cancel sets the run status to CANCELLED.
func (s *FlowRunSQLiteStore) Cancel(ctx context.Context, id string) error {
	_, err := s.inner.Conn.ExecContext(ctx,
		`UPDATE orh_flowrun SET status='CANCELLED', updated_at=CURRENT_TIMESTAMP WHERE id=?1`, id)
	return err
}
