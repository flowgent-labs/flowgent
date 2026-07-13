package approval

import (
	"context"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ApprovalPostgresStore wraps store.PostgresGenericStore[entities.ApprovalInfo].
type ApprovalPostgresStore struct {
	inner *store.PostgresGenericStore[entities.ApprovalInfo]
}

func NewApprovalPostgresStore(pool *pgxpool.Pool) *ApprovalPostgresStore {
	return &ApprovalPostgresStore{
		inner: &store.PostgresGenericStore[entities.ApprovalInfo]{
			Pool: pool, Table: "human_approvals", IDCol: "token",
		},
	}
}

func (s *ApprovalPostgresStore) Get(ctx context.Context, token string) (*entities.ApprovalInfo, error) {
	return s.inner.Get(ctx, token)
}
func (s *ApprovalPostgresStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.ApprovalInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *ApprovalPostgresStore) Save(ctx context.Context, e *entities.ApprovalInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *ApprovalPostgresStore) Delete(ctx context.Context, token string) error {
	return s.inner.Delete(ctx, token)
}

// CreateApproval generates id and sets timestamps before inserting.
// Token is only generated if not already provided by the caller.
func (s *ApprovalPostgresStore) CreateApproval(ctx context.Context, e *entities.ApprovalInfo) error {
	e.ID = uuid.New().String()
	if e.Token == "" {
		e.Token = uuid.New().String()
	}
	now := time.Now().UTC()
	e.CreatedAt = now
	e.UpdatedAt = now
	return s.inner.Save(ctx, e)
}

// UpdateApproval performs a targeted update of mutable columns.
func (s *ApprovalPostgresStore) UpdateApproval(ctx context.Context, e *entities.ApprovalInfo) error {
	_, err := s.inner.Pool.Exec(ctx,
		`UPDATE human_approvals SET status=$1, approved=$2, comment=$3, resolved_at=$4, updated_at=NOW() WHERE token=$5`,
		e.Status, e.Approved, e.Comment, e.ResolvedAt, e.Token)
	return err
}

// ListPending returns all approvals with status 'PENDING'.
func (s *ApprovalPostgresStore) ListPending(ctx context.Context) ([]*entities.ApprovalInfo, error) {
	cols := utils.Columns[entities.ApprovalInfo]()
	rows, err := s.inner.Pool.Query(ctx,
		fmt.Sprintf(`SELECT %s FROM human_approvals WHERE status='PENDING' ORDER BY created_at DESC`, cols))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*entities.ApprovalInfo
	for rows.Next() {
		e := new(entities.ApprovalInfo)
		if err := utils.ScanStruct(rows, e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
