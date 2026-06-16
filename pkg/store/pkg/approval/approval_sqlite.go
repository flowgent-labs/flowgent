package approval

import (
	"context"
	"database/sql"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/google/uuid"
)

// ApprovalSQLiteStore wraps store.SQLiteGenericStore[entities.ApprovalInfo].
type ApprovalSQLiteStore struct {
	inner *store.SQLiteGenericStore[entities.ApprovalInfo]
}

func NewApprovalSQLiteStore(conn *sql.DB) *ApprovalSQLiteStore {
	return &ApprovalSQLiteStore{
		inner: &store.SQLiteGenericStore[entities.ApprovalInfo]{
			Conn: conn, Table: "human_approvals", IDCol: "token",
		},
	}
}

func (s *ApprovalSQLiteStore) Get(ctx context.Context, token string) (*entities.ApprovalInfo, error) {
	return s.inner.Get(ctx, token)
}
func (s *ApprovalSQLiteStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.ApprovalInfo], error) {
	return s.inner.Select(ctx, req)
}
func (s *ApprovalSQLiteStore) Save(ctx context.Context, e *entities.ApprovalInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *ApprovalSQLiteStore) Delete(ctx context.Context, token string) error {
	return s.inner.Delete(ctx, token)
}

// CreateApproval generates id/token and sets timestamps before inserting.
func (s *ApprovalSQLiteStore) CreateApproval(ctx context.Context, e *entities.ApprovalInfo) error {
	e.ID = uuid.New().String()
	e.Token = uuid.New().String()
	now := time.Now().UTC()
	e.CreatedAt = now
	e.UpdatedAt = now
	return s.inner.Save(ctx, e)
}

// UpdateApproval performs a targeted update of mutable columns.
func (s *ApprovalSQLiteStore) UpdateApproval(ctx context.Context, e *entities.ApprovalInfo) error {
	_, err := s.inner.Conn.ExecContext(ctx,
		`UPDATE human_approvals SET status=?1, approved=?2, comment=?3, resolved_at=?4, updated_at=CURRENT_TIMESTAMP WHERE token=?5`,
		e.Status, e.Approved, e.Comment, e.ResolvedAt, e.Token)
	return err
}

// ListPending returns all approvals with status 'PENDING'.
func (s *ApprovalSQLiteStore) ListPending(ctx context.Context) ([]*entities.ApprovalInfo, error) {
	rows, err := s.inner.Conn.QueryContext(ctx,
		"SELECT * FROM human_approvals WHERE status='PENDING' ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*entities.ApprovalInfo
	for rows.Next() {
		e, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scanApproval(s scanner) (*entities.ApprovalInfo, error) {
	var e entities.ApprovalInfo
	var expiresAt, resolvedAt sql.NullTime
	err := s.Scan(
		&e.ID, &e.Token, &e.AgentFlowRunID, &e.TaskRunID, &e.Status,
		&e.Approved, &e.Comment, &e.Timeout,
		&expiresAt, &resolvedAt,
		&e.Description, &e.TenantID,
		&e.CreatedAt, &e.CreatedBy, &e.UpdatedAt, &e.UpdatedBy, &e.DelFlag,
	)
	if err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		e.ExpiresAt = &expiresAt.Time
	}
	if resolvedAt.Valid {
		e.ResolvedAt = &resolvedAt.Time
	}
	return &e, nil
}
