package approval

import (
	"context"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ApprovalPostgresStore struct{ pool *pgxpool.Pool }

func NewApprovalPostgresStore(pool *pgxpool.Pool) *ApprovalPostgresStore {
	return &ApprovalPostgresStore{pool: pool}
}
func (s *ApprovalPostgresStore) scan(row interface{ Scan(...any) error }) (*entities.ApprovalInfo, error) {
	var record approvalRecord
	if err := utils.ScanStruct(row, &record); err != nil {
		return nil, err
	}
	return record.entity(), nil
}
func (s *ApprovalPostgresStore) Get(ctx context.Context, id string) (*entities.ApprovalInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_approval").PostgresWhere(2)
	args := append([]any{id}, scopeArgs...)
	return s.scan(s.pool.QueryRow(ctx, `SELECT `+utils.Columns[approvalRecord]()+` FROM orh_approval WHERE id=$1 AND (`+scopeWhere+`)`, args...))
}
func (s *ApprovalPostgresStore) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.ApprovalInfo], error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 {
		req.Size = 20
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_approval").PostgresWhere(1)
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM orh_approval WHERE (`+scopeWhere+`)`, scopeArgs...).Scan(&total); err != nil {
		return nil, err
	}
	pos := len(scopeArgs) + 1
	args := append(scopeArgs, req.Size, (req.Page-1)*req.Size)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT %s FROM orh_approval WHERE (%s) ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, utils.Columns[approvalRecord](), scopeWhere, pos, pos+1), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.ApprovalInfo, 0)
	for rows.Next() {
		item, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return entities.NewPage(items, total, req), rows.Err()
}
func (s *ApprovalPostgresStore) Save(ctx context.Context, item *entities.ApprovalInfo) error {
	item.NormalizeAliases()
	if item.CreatedAt.IsZero() {
		return s.CreateApproval(ctx, item)
	}
	return s.UpdateApproval(ctx, item)
}
func (s *ApprovalPostgresStore) CreateApproval(ctx context.Context, item *entities.ApprovalInfo) error {
	item.NormalizeAliases()
	if item.ID == "" {
		item.ID = uuid.NewString()
		item.Token = item.ID
	}
	if item.Type == "" {
		item.Type = "human_gate"
	}
	if item.SubjectType == "" {
		if item.NodeRunID != "" {
			item.SubjectType = "node_run"
			item.SubjectID = item.NodeRunID
		} else {
			item.SubjectType = "run"
			item.SubjectID = item.RunID
		}
	}
	if item.Request == nil {
		item.Request = map[string]any{"run_id": item.RunID, "node_run_id": item.NodeRunID}
	}
	item.RequestHash = approvalHash(item.Request)
	if item.IdempotencyKey == "" {
		item.IdempotencyKey = item.ID
	}
	item.Status = "pending"
	if item.Timeout > 0 && item.ExpiresAt == nil {
		value := time.Now().UTC().Add(item.Timeout)
		item.ExpiresAt = &value
	}
	var namespace string
	if err := s.pool.QueryRow(ctx, `SELECT namespace_id FROM orh_run WHERE id=$1`, item.RunID).Scan(&namespace); err != nil {
		return err
	}
	if item.Namespace != "" && item.Namespace != namespace {
		return fmt.Errorf("approval namespace mismatch")
	}
	item.Namespace = namespace
	principal := item.CreatedBy
	if principal == "" {
		principal = "system:flowgent"
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO orh_approval(id,namespace_id,run_id,node_run_id,type,subject_type,subject_id,request,request_hash,status,expires_at,idempotency_key,description,created_by,updated_by,metadata) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,$9,'pending',$10,$11,$12,$13,$13,$14)`, item.ID, item.Namespace, item.RunID, item.NodeRunID, item.Type, item.SubjectType, item.SubjectID, approvalJSON(item.Request), item.RequestHash, item.ExpiresAt, item.IdempotencyKey, item.Description, principal, approvalJSON(item.Metadata))
	if err == nil {
		now := time.Now().UTC()
		item.CreatedAt = now
		item.UpdatedAt = now
		item.RowVersion = 1
	}
	return err
}
func (s *ApprovalPostgresStore) UpdateApproval(ctx context.Context, item *entities.ApprovalInfo) error {
	item.NormalizeAliases()
	if item.RequestHash == "" || approvalHash(item.Request) != item.RequestHash {
		return fmt.Errorf("approval request hash mismatch")
	}
	status := approvalStatus(item.Status)
	if status == "" || status == "pending" {
		return fmt.Errorf("approval decision must be terminal")
	}
	if item.Decision == nil {
		item.Decision = map[string]any{"comment": item.Comment}
		if item.Approved != nil {
			item.Decision["approved"] = *item.Approved
		}
	}
	decidedAt := item.DecidedAt
	if decidedAt == nil {
		now := time.Now().UTC()
		decidedAt = &now
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_approval").PostgresWhere(8)
	args := append([]any{status, approvalJSON(item.Decision), item.DecidedBy, decidedAt, item.ID, item.RowVersion, item.RequestHash}, scopeArgs...)
	query := `UPDATE orh_approval SET status=$1,decision=$2,decided_by=NULLIF($3,''),decided_at=$4
		WHERE id=$5 AND status='pending' AND ($6=0 OR row_version=$6) AND request_hash=$7
		AND (expires_at IS NULL OR expires_at>NOW()) AND (` + scopeWhere + `)`
	result, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		_, _ = s.pool.Exec(ctx, `UPDATE orh_approval SET status='expired',decision='{"reason":"expired"}'::jsonb,
			decided_at=NOW() WHERE id=$1 AND status='pending' AND request_hash=$2
			AND expires_at IS NOT NULL AND expires_at<=NOW()`, item.ID, item.RequestHash)
		return fmt.Errorf("approval is no longer pending or CAS conflict")
	}
	item.Status = status
	item.DecidedAt = decidedAt
	item.ResolvedAt = decidedAt
	return nil
}
func (s *ApprovalPostgresStore) ListPending(ctx context.Context, namespace string) ([]*entities.ApprovalInfo, error) {
	_, _ = s.pool.Exec(ctx, `UPDATE orh_approval SET status='expired',decided_at=NOW(),decision='{"reason":"expired"}'::jsonb WHERE namespace_id=$1 AND status='pending' AND expires_at IS NOT NULL AND expires_at<=NOW()`, namespace)
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_approval").PostgresWhere(2)
	args := append([]any{namespace}, scopeArgs...)
	rows, err := s.pool.Query(ctx, `SELECT `+utils.Columns[approvalRecord]()+` FROM orh_approval WHERE namespace_id=$1 AND status='pending' AND (`+scopeWhere+`) ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.ApprovalInfo, 0)
	for rows.Next() {
		item, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *ApprovalPostgresStore) Delete(ctx context.Context, id string) error {
	item, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	item.Status = "cancelled"
	item.Decision = map[string]any{"reason": "cancelled"}
	return s.UpdateApproval(ctx, item)
}
