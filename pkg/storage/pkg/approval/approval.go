package approval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

type approvalRecord struct {
	RunID          *string        `db:"run_id"`
	NodeRunID      *string        `db:"node_run_id"`
	Type           string         `db:"type"`
	SubjectType    string         `db:"subject_type"`
	SubjectID      string         `db:"subject_id"`
	Request        map[string]any `db:"request"`
	RequestHash    string         `db:"request_hash"`
	Status         string         `db:"status"`
	Decision       map[string]any `db:"decision"`
	DecidedBy      *string        `db:"decided_by"`
	DecidedAt      *time.Time     `db:"decided_at"`
	ExpiresAt      *time.Time     `db:"expires_at"`
	ConsumedAt     *time.Time     `db:"consumed_at"`
	IdempotencyKey string         `db:"idempotency_key"`
	ID             string         `db:"id"`
	Description    *string        `db:"description"`
	Namespace      string         `db:"namespace_id"`
	CreatedAt      time.Time      `db:"created_at"`
	CreatedBy      *string        `db:"created_by"`
	UpdatedAt      time.Time      `db:"updated_at"`
	UpdatedBy      *string        `db:"updated_by"`
	RowVersion     int64          `db:"row_version"`
	Metadata       map[string]any `db:"metadata"`
}

func (r *approvalRecord) entity() *entities.ApprovalInfo {
	item := &entities.ApprovalInfo{BaseEntity: entities.BaseEntity{ID: r.ID, Description: approvalStringValue(r.Description), Namespace: r.Namespace, Status: r.Status, CreatedAt: r.CreatedAt, CreatedBy: approvalStringValue(r.CreatedBy), UpdatedAt: r.UpdatedAt, UpdatedBy: approvalStringValue(r.UpdatedBy), RowVersion: r.RowVersion, Metadata: r.Metadata}, RunID: approvalStringValue(r.RunID), NodeRunID: approvalStringValue(r.NodeRunID), Type: r.Type, SubjectType: r.SubjectType, SubjectID: r.SubjectID, Request: r.Request, RequestHash: r.RequestHash, Status: r.Status, Decision: r.Decision, DecidedBy: approvalStringValue(r.DecidedBy), DecidedAt: r.DecidedAt, ExpiresAt: r.ExpiresAt, ConsumedAt: r.ConsumedAt, IdempotencyKey: r.IdempotencyKey, AgentFlowRunID: approvalStringValue(r.RunID), TaskRunID: approvalStringValue(r.NodeRunID), Token: r.ID, ResolvedAt: r.DecidedAt}
	if r.Status == "approved" {
		value := true
		item.Approved = &value
	} else if r.Status == "rejected" {
		value := false
		item.Approved = &value
	}
	if value, ok := r.Decision["comment"].(string); ok {
		item.Comment = value
	}
	return item
}

func approvalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func approvalJSON(value any) []byte {
	if value == nil {
		return nil
	}
	data, _ := json.Marshal(value)
	return data
}
func approvalHash(value any) string {
	sum := sha256.Sum256(approvalJSON(value))
	return hex.EncodeToString(sum[:])
}
func approvalStatus(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

// IApprovalStore is the human approval entity store interface.
type IApprovalStore interface {
	Get(ctx context.Context, token string) (*entities.ApprovalInfo, error)
	Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.ApprovalInfo], error)
	Save(ctx context.Context, entity *entities.ApprovalInfo) error
	Delete(ctx context.Context, token string) error
	CreateApproval(ctx context.Context, entity *entities.ApprovalInfo) error
	UpdateApproval(ctx context.Context, entity *entities.ApprovalInfo) error
	ListPending(ctx context.Context, namespace string) ([]*entities.ApprovalInfo, error)
}
