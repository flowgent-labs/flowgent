package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/google/uuid"
)

func (s *KnowledgeSQLiteStore) CreateCandidate(ctx context.Context, candidate *entities.KnowledgeCandidate) (*entities.ApprovalInfo, error) {
	if err := validateCandidate(candidate); err != nil {
		return nil, err
	}
	_, approvalType, _ := candidateType(candidate.Type)
	principal := candidatePrincipal(candidate)
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var sourceFlowID, sourceFlowName, runStatus string
	var summarizeEnabled bool
	if err = tx.QueryRowContext(ctx, `SELECT r.flow_id,f.name,r.status,r.summarize_enabled
		FROM orh_run r JOIN orh_flow f ON f.id=r.flow_id
		WHERE r.id=? AND r.namespace_id=?`, candidate.SourceRunID, candidate.Namespace).
		Scan(&sourceFlowID, &sourceFlowName, &runStatus, &summarizeEnabled); err != nil {
		return nil, fmt.Errorf("resolve candidate source run: %w", err)
	}
	if !strings.EqualFold(runStatus, string(entities.RunCompleted)) || !summarizeEnabled {
		return nil, fmt.Errorf("run must be completed with summarize_enabled=true")
	}
	if candidate.TargetScope == "flow" {
		flowKey := candidate.TargetFlowName
		if flowKey == "" {
			flowKey = candidate.TargetFlowID
		}
		if flowKey == "" {
			flowKey = sourceFlowID
		}
		if err = tx.QueryRowContext(ctx, `SELECT id,name FROM orh_flow WHERE namespace_id=?
			AND (id=? OR name=?) AND status<>'DELETED'`, candidate.Namespace, flowKey, flowKey).
			Scan(&candidate.TargetFlowID, &candidate.TargetFlowName); err != nil {
			return nil, fmt.Errorf("resolve candidate target flow: %w", err)
		}
		if candidate.TargetFlowID != sourceFlowID {
			return nil, fmt.Errorf("%w: a run may summarize only into its own flow", ErrPublicationDenied)
		}
	} else {
		candidate.TargetFlowID, candidate.TargetFlowName = "", ""
		if origin, _ := candidate.Provenance["origin"].(string); origin == "built_in_summarizer" {
			return nil, fmt.Errorf("%w: built-in summaries cannot write namespace knowledge", ErrPublicationDenied)
		}
	}

	actualRevision, err := candidateBaselineSQLite(ctx, tx, candidate)
	if err != nil {
		return nil, err
	}
	if actualRevision != candidate.ExpectedRevision {
		return nil, fmt.Errorf("%w: expected revision %d, current revision %d", ErrPublicationStale, candidate.ExpectedRevision, actualRevision)
	}

	existing, err := candidateByIdempotencySQLite(ctx, tx, candidate.Namespace, candidate.IdempotencyKey)
	if err == nil {
		if !candidateMatches(existing, candidate) {
			return nil, ErrCandidateImmutable
		}
		*candidate = *existing
		approval, loadErr := approvalByIDSQLite(ctx, tx, existing.ApprovalID)
		if loadErr != nil {
			return nil, loadErr
		}
		return approval, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	candidate.ID = uuid.NewString()
	approvalID := uuid.NewString()
	candidate.ApprovalID = approvalID
	candidate.Status = "unpublished"
	request := candidateRequest(candidate)
	requestHash := canonicalHash(request)
	approvalKey := "publication:" + candidate.IdempotencyKey
	if _, err = tx.ExecContext(ctx, `INSERT INTO orh_approval
		(id,namespace_id,run_id,type,subject_type,subject_id,request,request_hash,status,
		 idempotency_key,description,created_by,updated_by,metadata)
		VALUES(?,?,? ,?,'knowledge_candidate',?,?,?,'pending',?,?,?,?,'{}')`,
		approvalID, candidate.Namespace, candidate.SourceRunID, approvalType, candidate.ID,
		string(jsonBytes(request)), requestHash, approvalKey, candidate.Description, principal, principal); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO knw_candidate
		(id,namespace_id,source_run_id,target_scope,target_flow_id,target_document_id,type,content,
		 content_hash,provenance,expected_revision,approval_id,status,idempotency_key,description,
		 created_by,updated_by,metadata)
		VALUES(?,?,?,?,NULLIF(?,''),NULLIF(?,''),?,?,?,?,?,?,'unpublished',?,?,?,?,?)`,
		candidate.ID, candidate.Namespace, candidate.SourceRunID, candidate.TargetScope,
		candidate.TargetFlowID, candidate.TargetDocumentID, candidate.Type, candidate.Content,
		candidate.ContentHash, string(jsonBytes(candidate.Provenance)), candidate.ExpectedRevision,
		approvalID, candidate.IdempotencyKey, candidate.Description, principal, principal,
		jsonStringValue(candidate.Metadata)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	candidate.CreatedAt, candidate.UpdatedAt, candidate.RowVersion = now, now, 1
	return &entities.ApprovalInfo{
		BaseEntity: entities.BaseEntity{ID: approvalID, Namespace: candidate.Namespace, Status: "pending", CreatedAt: now, UpdatedAt: now, CreatedBy: principal, UpdatedBy: principal, RowVersion: 1},
		RunID:      candidate.SourceRunID, Type: approvalType, SubjectType: "knowledge_candidate",
		SubjectID: candidate.ID, Request: request, RequestHash: requestHash, Status: "pending",
		IdempotencyKey: approvalKey, Token: approvalID,
	}, nil
}

func (s *KnowledgeSQLiteStore) ResolveCandidateApproval(
	ctx context.Context,
	approvalID string,
	approved bool,
	actor string,
	decision map[string]any,
) (*entities.KnowledgeCandidate, error) {
	actor = defaultString(strings.TrimSpace(actor), "system:flowgent")
	if decision == nil {
		decision = map[string]any{}
	}
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	candidate, approvalStatus, approvalType, requestHash, expired, err := lockedCandidateSQLite(ctx, tx, approvalID)
	if err != nil {
		return nil, err
	}
	expectedType := "knowledge_publish"
	if candidate.Type == "instruction" {
		expectedType = "instruction_publish"
	}
	if approvalType != expectedType || candidate.ContentHash != hashText(candidate.Content) ||
		requestHash != canonicalHash(candidateRequest(candidate)) {
		return nil, fmt.Errorf("%w: approval request integrity check failed", ErrCandidateImmutable)
	}
	if approvalStatus != "pending" {
		if (approved && approvalStatus == "approved" && candidate.Status == "published") ||
			(!approved && approvalStatus == "rejected" && candidate.Status == "failed") {
			return candidate, tx.Commit()
		}
		if approvalStatus == "expired" {
			return nil, ErrApprovalExpired
		}
		return nil, fmt.Errorf("approval is already %s", approvalStatus)
	}
	now := time.Now().UTC()
	if expired {
		if err = failCandidateApprovalSQLite(ctx, tx, candidate, "expired", actor,
			map[string]any{"reason": "approval expired"}, now); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return nil, ErrApprovalExpired
	}
	if !approved {
		if len(decision) == 0 {
			decision["reason"] = "rejected"
		}
		if err = failCandidateApprovalSQLite(ctx, tx, candidate, "rejected", actor, decision, now); err != nil {
			return nil, err
		}
		candidate.Status = "failed"
		return candidate, tx.Commit()
	}

	actualRevision, baselineErr := candidateBaselineSQLite(ctx, tx, candidate)
	if baselineErr != nil {
		return nil, baselineErr
	}
	if actualRevision != candidate.ExpectedRevision {
		decision["publication_error"] = "baseline_changed"
		decision["expected_revision"] = candidate.ExpectedRevision
		decision["actual_revision"] = actualRevision
		if err = failCandidateApprovalSQLite(ctx, tx, candidate, "approved", actor, decision, now); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: expected revision %d, current revision %d", ErrPublicationStale, candidate.ExpectedRevision, actualRevision)
	}
	var publishedID string
	if candidate.Type == "instruction" {
		publishedID, err = publishInstructionSQLite(ctx, tx, candidate, actor)
	} else {
		publishedID, err = publishKnowledgeSQLite(ctx, tx, candidate, actor)
	}
	if err != nil {
		return nil, err
	}
	decision["published_id"] = publishedID
	decision["published_revision"] = candidate.ExpectedRevision + 1
	if _, err = tx.ExecContext(ctx, `UPDATE knw_candidate SET status='published',updated_by=?,
		metadata=json_set(COALESCE(metadata,'{}'),'$.published_id',?)
		WHERE id=? AND approval_id=? AND status='unpublished'`, actor, publishedID, candidate.ID, approvalID); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE orh_approval SET status='approved',decision=?,decided_by=?,
		decided_at=?,consumed_at=?,updated_by=? WHERE id=? AND status='pending'`,
		string(jsonBytes(decision)), actor, now, now, actor, approvalID)
	if err != nil {
		return nil, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return nil, fmt.Errorf("approval decision CAS conflict")
	}
	payload := map[string]any{
		"candidate_id": candidate.ID, "approval_id": approvalID,
		"type": candidate.Type, "published_id": publishedID,
		"revision": candidate.ExpectedRevision + 1,
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO orh_outbox
		(id,namespace_id,aggregate_type,aggregate_id,event_type,payload,status,idempotency_key,
		 created_by,updated_by,metadata)
		VALUES(?,?,'knowledge_candidate',?,'publication.completed',?,'pending',?,?,?,'{}')`,
		uuid.NewString(), candidate.Namespace, candidate.ID, string(jsonBytes(payload)),
		"publication:"+candidate.IdempotencyKey+":completed", actor, actor); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	candidate.Status = "published"
	if candidate.Metadata == nil {
		candidate.Metadata = map[string]any{}
	}
	candidate.Metadata["published_id"] = publishedID
	return candidate, nil
}

func candidateBaselineSQLite(ctx context.Context, tx *sql.Tx, candidate *entities.KnowledgeCandidate) (int64, error) {
	if candidate.Type == "instruction" {
		var revision int64
		if candidate.TargetScope == "namespace" {
			err := tx.QueryRowContext(ctx, `SELECT COALESCE(i.revision,0) FROM orh_namespace n
				LEFT JOIN llm_instruction i ON i.id=n.instruction_id WHERE n.id=?`, candidate.Namespace).Scan(&revision)
			return revision, err
		}
		err := tx.QueryRowContext(ctx, `SELECT COALESCE(i.revision,0) FROM orh_flow f
			LEFT JOIN llm_instruction i ON i.id=f.instruction_id
			WHERE f.id=? AND f.namespace_id=?`, candidate.TargetFlowID, candidate.Namespace).Scan(&revision)
		return revision, err
	}
	if candidate.TargetDocumentID == "" {
		return 0, nil
	}
	var revision int64
	var scope, flowID, aclRef string
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(r.revision,0),d.scope,COALESCE(d.flow_id,''),COALESCE(d.acl_ref,'')
		FROM knw_document d LEFT JOIN knw_document_revision r ON r.id=d.current_revision_id
		WHERE d.id=? AND d.namespace_id=? AND d.status<>'DELETED'`, candidate.TargetDocumentID,
		candidate.Namespace).Scan(&revision, &scope, &flowID, &aclRef)
	if err != nil {
		return 0, fmt.Errorf("resolve target document: %w", err)
	}
	if aclRef != "" || scope != candidate.TargetScope || flowID != candidate.TargetFlowID {
		return 0, fmt.Errorf("%w: target document scope or ACL does not permit publication", ErrPublicationDenied)
	}
	return revision, nil
}

func candidateByIdempotencySQLite(ctx context.Context, tx *sql.Tx, namespace, key string) (*entities.KnowledgeCandidate, error) {
	return scanCandidateSQLite(tx.QueryRowContext(ctx, `SELECT c.id,c.namespace_id,c.source_run_id,c.target_scope,
		COALESCE(c.target_flow_id,''),COALESCE(f.name,''),COALESCE(c.target_document_id,''),c.type,
		c.content,c.content_hash,c.provenance,c.expected_revision,c.approval_id,c.status,c.idempotency_key,
		COALESCE(c.description,''),COALESCE(c.metadata,'{}'),c.created_at,COALESCE(c.created_by,''),
		c.updated_at,COALESCE(c.updated_by,''),c.row_version
		FROM knw_candidate c LEFT JOIN orh_flow f ON f.id=c.target_flow_id
		WHERE c.namespace_id=? AND c.idempotency_key=?`, namespace, key))
}

func scanCandidateSQLite(row interface{ Scan(...any) error }) (*entities.KnowledgeCandidate, error) {
	var item entities.KnowledgeCandidate
	var provenance, metadata, createdAt, updatedAt string
	err := row.Scan(&item.ID, &item.Namespace, &item.SourceRunID, &item.TargetScope,
		&item.TargetFlowID, &item.TargetFlowName, &item.TargetDocumentID, &item.Type, &item.Content,
		&item.ContentHash, &provenance, &item.ExpectedRevision, &item.ApprovalID, &item.Status,
		&item.IdempotencyKey, &item.Description, &metadata, &createdAt, &item.CreatedBy, &updatedAt,
		&item.UpdatedBy, &item.RowVersion)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(provenance), &item.Provenance)
	_ = json.Unmarshal([]byte(metadata), &item.Metadata)
	item.CreatedAt, _ = utils.ParseTime(createdAt)
	item.UpdatedAt, _ = utils.ParseTime(updatedAt)
	return &item, nil
}

func approvalByIDSQLite(ctx context.Context, tx *sql.Tx, id string) (*entities.ApprovalInfo, error) {
	var item entities.ApprovalInfo
	var request, decision string
	var decidedBy sql.NullString
	var decidedAt, expiresAt, consumedAt sql.NullString
	var createdAt, updatedAt string
	err := tx.QueryRowContext(ctx, `SELECT id,namespace_id,COALESCE(run_id,''),COALESCE(node_run_id,''),
		type,subject_type,subject_id,request,request_hash,status,COALESCE(decision,'{}'),decided_by,
		decided_at,expires_at,consumed_at,idempotency_key,created_at,COALESCE(created_by,''),updated_at,
		COALESCE(updated_by,''),row_version FROM orh_approval WHERE id=?`, id).Scan(&item.ID,
		&item.Namespace, &item.RunID, &item.NodeRunID, &item.Type, &item.SubjectType, &item.SubjectID,
		&request, &item.RequestHash, &item.Status, &decision, &decidedBy, &decidedAt, &expiresAt,
		&consumedAt, &item.IdempotencyKey, &createdAt, &item.CreatedBy, &updatedAt, &item.UpdatedBy,
		&item.RowVersion)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(request), &item.Request)
	_ = json.Unmarshal([]byte(decision), &item.Decision)
	item.DecidedBy = decidedBy.String
	item.DecidedAt = sqliteOptionalTime(decidedAt)
	item.ExpiresAt = sqliteOptionalTime(expiresAt)
	item.ConsumedAt = sqliteOptionalTime(consumedAt)
	item.CreatedAt, _ = utils.ParseTime(createdAt)
	item.UpdatedAt, _ = utils.ParseTime(updatedAt)
	item.Token = item.ID
	return &item, nil
}

func sqliteOptionalTime(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed, err := utils.ParseTime(value.String)
	if err != nil {
		return nil
	}
	return &parsed
}

func lockedCandidateSQLite(ctx context.Context, tx *sql.Tx, approvalID string) (*entities.KnowledgeCandidate, string, string, string, bool, error) {
	row := tx.QueryRowContext(ctx, `SELECT c.id,c.namespace_id,c.source_run_id,c.target_scope,
		COALESCE(c.target_flow_id,''),COALESCE(f.name,''),COALESCE(c.target_document_id,''),c.type,
		c.content,c.content_hash,c.provenance,c.expected_revision,c.approval_id,c.status,c.idempotency_key,
		COALESCE(c.description,''),COALESCE(c.metadata,'{}'),c.created_at,COALESCE(c.created_by,''),
		c.updated_at,COALESCE(c.updated_by,''),c.row_version,a.status,a.type,a.request_hash,
		(a.expires_at IS NOT NULL AND datetime(a.expires_at)<=CURRENT_TIMESTAMP)
		FROM knw_candidate c JOIN orh_approval a ON a.id=c.approval_id AND a.namespace_id=c.namespace_id
		LEFT JOIN orh_flow f ON f.id=c.target_flow_id WHERE c.approval_id=?`, approvalID)
	var item entities.KnowledgeCandidate
	var provenance, metadata, createdAt, updatedAt string
	var approvalStatus, approvalType, requestHash string
	var expired bool
	err := row.Scan(&item.ID, &item.Namespace, &item.SourceRunID, &item.TargetScope,
		&item.TargetFlowID, &item.TargetFlowName, &item.TargetDocumentID, &item.Type, &item.Content,
		&item.ContentHash, &provenance, &item.ExpectedRevision, &item.ApprovalID, &item.Status,
		&item.IdempotencyKey, &item.Description, &metadata, &createdAt, &item.CreatedBy, &updatedAt,
		&item.UpdatedBy, &item.RowVersion, &approvalStatus, &approvalType, &requestHash, &expired)
	if err != nil {
		return nil, "", "", "", false, err
	}
	_ = json.Unmarshal([]byte(provenance), &item.Provenance)
	_ = json.Unmarshal([]byte(metadata), &item.Metadata)
	item.CreatedAt, _ = utils.ParseTime(createdAt)
	item.UpdatedAt, _ = utils.ParseTime(updatedAt)
	return &item, approvalStatus, approvalType, requestHash, expired, nil
}

func failCandidateApprovalSQLite(ctx context.Context, tx *sql.Tx, candidate *entities.KnowledgeCandidate,
	approvalStatus, actor string, decision map[string]any, now time.Time,
) error {
	consumedAt := any(nil)
	if approvalStatus == "approved" {
		consumedAt = now
	}
	result, err := tx.ExecContext(ctx, `UPDATE orh_approval SET status=?,decision=?,decided_by=?,
		decided_at=?,consumed_at=COALESCE(?,consumed_at),updated_by=? WHERE id=? AND status='pending'`,
		approvalStatus, string(jsonBytes(decision)), actor, now, consumedAt, actor, candidate.ApprovalID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("approval decision CAS conflict")
	}
	_, err = tx.ExecContext(ctx, `UPDATE knw_candidate SET status='failed',updated_by=?
		WHERE id=? AND status='unpublished'`, actor, candidate.ID)
	return err
}

func publishInstructionSQLite(ctx context.Context, tx *sql.Tx, candidate *entities.KnowledgeCandidate, actor string) (string, error) {
	instructionID := uuid.NewString()
	var flowID any
	if candidate.TargetScope == "flow" {
		flowID = candidate.TargetFlowID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO llm_instruction
		(id,namespace_id,scope,flow_id,revision,content,content_hash,status,approval_id,
		 description,created_by,updated_by,metadata)
		VALUES(?,?,?,?,?,?,?,'published',?,?,?,?,?)`, instructionID, candidate.Namespace,
		candidate.TargetScope, flowID, candidate.ExpectedRevision+1, candidate.Content,
		candidate.ContentHash, candidate.ApprovalID, candidate.Description, actor, actor,
		jsonStringValue(candidate.Provenance)); err != nil {
		return "", err
	}
	var result sql.Result
	var err error
	if candidate.TargetScope == "namespace" {
		result, err = tx.ExecContext(ctx, `UPDATE orh_namespace SET instruction_id=?,updated_by=?
			WHERE id=? AND COALESCE((SELECT revision FROM llm_instruction WHERE id=instruction_id),0)=?`,
			instructionID, actor, candidate.Namespace, candidate.ExpectedRevision)
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE orh_flow SET instruction_id=?,updated_by=?
			WHERE id=? AND namespace_id=?
			AND COALESCE((SELECT revision FROM llm_instruction WHERE id=instruction_id),0)=?`,
			instructionID, actor, candidate.TargetFlowID, candidate.Namespace, candidate.ExpectedRevision)
	}
	if err != nil {
		return "", err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return "", ErrPublicationStale
	}
	return instructionID, nil
}

func publishKnowledgeSQLite(ctx context.Context, tx *sql.Tx, candidate *entities.KnowledgeCandidate, actor string) (string, error) {
	var sourceID string
	err := tx.QueryRowContext(ctx, `SELECT id FROM knw_source
		WHERE namespace_id=? AND lower(name)='built-in-summarizer'`, candidate.Namespace).Scan(&sourceID)
	if errors.Is(err, sql.ErrNoRows) {
		sourceID = uuid.NewString()
		_, err = tx.ExecContext(ctx, `INSERT INTO knw_source
			(id,namespace_id,name,type,config,status,created_by,updated_by,metadata)
			VALUES(?,?,'built-in-summarizer','run_summary','{}','ACTIVE',?,?,'{}')`,
			sourceID, candidate.Namespace, actor, actor)
	}
	if err != nil {
		return "", err
	}
	documentID := candidate.TargetDocumentID
	newDocument := documentID == ""
	if newDocument {
		documentID = uuid.NewString()
		documentKey := "summary/" + candidate.SourceRunID + "/" + candidate.ID
		_, err = tx.ExecContext(ctx, `INSERT INTO knw_document
			(id,namespace_id,source_id,document_key,scope,flow_id,classification,status,
			 description,created_by,updated_by,metadata)
			VALUES(?,?,?,?,?,NULLIF(?,''),'internal','ACTIVE',?,?,?,?)`, documentID,
			candidate.Namespace, sourceID, documentKey, candidate.TargetScope, candidate.TargetFlowID,
			candidate.Description, actor, actor, jsonStringValue(candidate.Metadata))
	} else {
		var scope, flowID, aclRef string
		err = tx.QueryRowContext(ctx, `SELECT scope,COALESCE(flow_id,''),COALESCE(acl_ref,'')
			FROM knw_document WHERE id=? AND namespace_id=? AND status<>'DELETED'`, documentID,
			candidate.Namespace).Scan(&scope, &flowID, &aclRef)
		if err == nil && (scope != candidate.TargetScope || flowID != candidate.TargetFlowID || aclRef != "") {
			return "", ErrPublicationDenied
		}
	}
	if err != nil {
		return "", err
	}
	revisionID, contentID := uuid.NewString(), uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO knw_document_revision
		(id,namespace_id,document_id,revision,title,provenance,content_hash,status,approval_id,
		 description,created_by,updated_by,metadata)
		VALUES(?,?,?,?,?,?,?,'published',?,?,?,?, '{}')`, revisionID, candidate.Namespace,
		documentID, candidate.ExpectedRevision+1, candidateDocumentTitle(candidate),
		string(jsonBytes(candidate.Provenance)), candidate.ContentHash, candidate.ApprovalID,
		candidate.Description, actor, actor); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO knw_content
		(id,namespace_id,document_revision_id,type,ordinal,content,content_hash,created_by,updated_by,metadata)
		VALUES(?,?,?,'summary',0,?,?,?,?, '{}')`, contentID, candidate.Namespace, revisionID,
		candidate.Content, candidate.ContentHash, actor, actor); err != nil {
		return "", err
	}
	result, err := tx.ExecContext(ctx, `UPDATE knw_document SET current_revision_id=?,updated_by=?
		WHERE id=? AND namespace_id=?
		AND COALESCE((SELECT revision FROM knw_document_revision WHERE id=current_revision_id),0)=?`,
		revisionID, actor, documentID, candidate.Namespace, candidate.ExpectedRevision)
	if err != nil {
		return "", err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return "", ErrPublicationStale
	}
	candidate.TargetDocumentID = documentID
	return documentID, nil
}

func jsonStringValue(value any) any {
	if value == nil {
		return nil
	}
	return string(jsonBytes(value))
}
