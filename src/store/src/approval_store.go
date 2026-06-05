package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/flowgent-labs/flowgent/model/src"
)

// IApprovalStore manages human approvals.
type IApprovalStore interface {
	CreateApproval(ctx context.Context, approval *model.HumanApproval) error
	GetApproval(ctx context.Context, token string) (*model.HumanApproval, error)
	UpdateApproval(ctx context.Context, approval *model.HumanApproval) error
	ListPendingApprovals(ctx context.Context) ([]model.HumanApproval, error)
}

// ─── PostgresStore methods — approval ─────────────────────────

func (s *PostgresStore) CreateApproval(ctx context.Context, approval *model.HumanApproval) error {
	approval.Token = newUUID()
	approval.CreatedAt = time.Now()
	approval.UpdatedAt = approval.CreatedAt
	if approval.Timeout > 0 {
		exp := approval.CreatedAt.Add(approval.Timeout)
		approval.ExpiresAt = &exp
	}
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO human_approvals (task_run_id,token,status,timeout_seconds,created_at,updated_at,expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		approval.TaskRunID, approval.Token, approval.Status, int(approval.Timeout.Seconds()), approval.CreatedAt, approval.UpdatedAt, approval.ExpiresAt)
	return err
}

func (s *PostgresStore) GetApproval(ctx context.Context, token string) (*model.HumanApproval, error) {
	row := s.Pool.QueryRow(ctx, `SELECT task_run_id,token,status,(approved_at IS NOT NULL) as approved,comment,EXTRACT(EPOCH FROM GREATEST(timeout_at - NOW(), '0s'::interval))::int as timeout_seconds,created_at,updated_at,timeout_at as expires_at,approved_at as resolved_at FROM human_approvals WHERE token=$1`, token)
	return scanHumanApproval(row)
}

func (s *PostgresStore) UpdateApproval(ctx context.Context, approval *model.HumanApproval) error {
	now := time.Now()
	approval.UpdatedAt = now
	if approval.Approved != nil {
		approval.ResolvedAt = &now
	}
	_, err := s.Pool.Exec(ctx, `UPDATE human_approvals SET status=$1,approved_at=CASE WHEN $2::boolean THEN NOW() ELSE approved_at END,rejected_at=CASE WHEN $2::boolean IS NOT NULL AND NOT $2::boolean THEN NOW() ELSE rejected_at END,comment=$3,updated_at=$4 WHERE token=$5`,
		approval.Status, approval.Approved, approval.Comment, approval.UpdatedAt, approval.Token)
	return err
}

func (s *PostgresStore) ListPendingApprovals(ctx context.Context) ([]model.HumanApproval, error) {
	rows, err := s.Pool.Query(ctx, `SELECT task_run_id,token,status,(approved_at IS NOT NULL) as approved,comment,EXTRACT(EPOCH FROM GREATEST(timeout_at - NOW(), '0s'::interval))::int as timeout_seconds,created_at,updated_at,timeout_at as expires_at,approved_at as resolved_at FROM human_approvals WHERE status='PENDING' AND (expires_at IS NULL OR expires_at > NOW())`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var approvals []model.HumanApproval
	for rows.Next() {
		a, err := scanHumanApprovalRow(rows)
		if err != nil {
			continue
		}
		approvals = append(approvals, *a)
	}
	return approvals, nil
}

// ─── SQLiteStore methods — approval ───────────────────────────

func (s *SQLiteStore) CreateApproval(ctx context.Context, approval *model.HumanApproval) error {
	approval.Token = uuid.New().String()
	approval.CreatedAt = time.Now()
	approval.UpdatedAt = approval.CreatedAt
	if approval.Timeout > 0 {
		expires := approval.CreatedAt.Add(approval.Timeout)
		approval.ExpiresAt = &expires
	}
	_, err := s.Conn.ExecContext(ctx,
		`INSERT INTO human_approvals (task_run_id,token,status,timeout_seconds,created_at,updated_at,expires_at)
		 VALUES (?1,?2,?3,?4,?5,?6,?7)`,
		approval.TaskRunID, approval.Token, approval.Status, int(approval.Timeout.Seconds()), approval.CreatedAt, approval.UpdatedAt, approval.ExpiresAt)
	return err
}

func (s *SQLiteStore) GetApproval(ctx context.Context, token string) (*model.HumanApproval, error) {
	row := s.Conn.QueryRowContext(ctx,
		`SELECT task_run_id,token,status,approved,comment,timeout_seconds,created_at,updated_at,expires_at,resolved_at
		 FROM human_approvals WHERE token=?1`, token)
	return scanHumanApproval(row)
}

func (s *SQLiteStore) UpdateApproval(ctx context.Context, approval *model.HumanApproval) error {
	approval.UpdatedAt = time.Now()
	if approval.Approved != nil {
		now := time.Now()
		approval.ResolvedAt = &now
	}
	_, err := s.Conn.ExecContext(ctx,
		`UPDATE human_approvals SET status=?1,approved=?2,comment=?3,updated_at=?4,resolved_at=?5 WHERE token=?6`,
		approval.Status, approval.Approved, approval.Comment, approval.UpdatedAt, approval.ResolvedAt, approval.Token)
	return err
}

func (s *SQLiteStore) ListPendingApprovals(ctx context.Context) ([]model.HumanApproval, error) {
	rows, err := s.Conn.QueryContext(ctx,
		`SELECT task_run_id,token,status,approved,comment,timeout_seconds,created_at,updated_at,expires_at,resolved_at
		 FROM human_approvals WHERE status='PENDING' AND (expires_at IS NULL OR expires_at > datetime('now'))`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var approvals []model.HumanApproval
	for rows.Next() {
		a, err := scanHumanApprovalRow(rows)
		if err != nil {
			return nil, err
		}
		approvals = append(approvals, *a)
	}
	return approvals, nil
}
