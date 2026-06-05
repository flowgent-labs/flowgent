package approval

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/store/src"
)

// ApprovalPostgresStore wraps store.PostgresGenericStore[model.HumanApproval].
type ApprovalPostgresStore struct {
	inner *store.PostgresGenericStore[model.HumanApproval]
}

func NewApprovalPostgresStore(pool *pgxpool.Pool) *ApprovalPostgresStore {
	return &ApprovalPostgresStore{
		inner: &store.PostgresGenericStore[model.HumanApproval]{
			Pool: pool, Table: "human_approvals", IDCol: "token",
		},
	}
}

func (s *ApprovalPostgresStore) Get(ctx context.Context, token string) (*model.HumanApproval, error) {
	return s.inner.Get(ctx, token)
}
func (s *ApprovalPostgresStore) Select(ctx context.Context, offset, limit int) ([]*model.HumanApproval, error) {
	return s.inner.Select(ctx, offset, limit)
}
func (s *ApprovalPostgresStore) Save(ctx context.Context, e *model.HumanApproval) error {
	return s.inner.Save(ctx, e)
}
func (s *ApprovalPostgresStore) Delete(ctx context.Context, token string) error {
	return s.inner.Delete(ctx, token)
}

// CreateApproval generates a token and sets timestamps before inserting.
func (s *ApprovalPostgresStore) CreateApproval(ctx context.Context, e *model.HumanApproval) error {
	e.Token = uuid.New().String()
	now := time.Now().UTC()
	e.CreatedAt = now
	e.UpdatedAt = now
	return s.inner.Save(ctx, e)
}

// UpdateApproval performs a targeted update of mutable columns.
func (s *ApprovalPostgresStore) UpdateApproval(ctx context.Context, e *model.HumanApproval) error {
	_, err := s.inner.Pool.Exec(ctx,
		`UPDATE human_approvals SET status=$1, approved=$2, comment=$3, resolved_at=$4, updated_at=NOW() WHERE token=$5`,
		e.Status, e.Approved, e.Comment, e.ResolvedAt, e.Token)
	return err
}

// ListPending returns all approvals with status 'PENDING'.
func (s *ApprovalPostgresStore) ListPending(ctx context.Context) ([]*model.HumanApproval, error) {
	rows, err := s.inner.Pool.Query(ctx,
		"SELECT * FROM human_approvals WHERE status='PENDING' ORDER BY created_at DESC")
	if err != nil { return nil, err }
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[model.HumanApproval])
}
