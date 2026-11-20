package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *KnowledgePostgresStore) CreateCandidate(ctx context.Context, candidate *entities.KnowledgeCandidate) (*entities.ApprovalInfo, error) {
	if err := validateCandidate(candidate); err != nil {
		return nil, err
	}
	_, approvalType, _ := candidateType(candidate.Type)
	principal := candidatePrincipal(candidate)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sourceFlowID, sourceFlowName, runStatus string
	var summarizeEnabled bool
	if err = tx.QueryRow(ctx, `SELECT r.flow_id,f.name,r.status,r.summarize_enabled
		FROM orh_run r JOIN orh_flow f ON f.id=r.flow_id
		WHERE r.id=$1 AND r.namespace_id=$2 FOR SHARE OF r,f`, candidate.SourceRunID, candidate.Namespace).
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
		if err = tx.QueryRow(ctx, `SELECT id,name FROM orh_flow WHERE namespace_id=$1
			AND (id=$2 OR name=$2) AND status<>'DELETED' FOR SHARE`, candidate.Namespace, flowKey).
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

	actualRevision, err := candidateBaselinePG(ctx, tx, candidate)
	if err != nil {
		return nil, err
	}
	if actualRevision != candidate.ExpectedRevision {
		return nil, fmt.Errorf("%w: expected revision %d, current revision %d", ErrPublicationStale, candidate.ExpectedRevision, actualRevision)
	}

	var existing entities.KnowledgeCandidate
	var existingProvenance []byte
	err = tx.QueryRow(ctx, `SELECT id,source_run_id,target_scope,COALESCE(target_flow_id,''),
		COALESCE(target_document_id,''),type,content,content_hash,provenance,expected_revision,
		approval_id,status,idempotency_key,namespace_id FROM knw_candidate
		WHERE namespace_id=$1 AND idempotency_key=$2`, candidate.Namespace, candidate.IdempotencyKey).
		Scan(&existing.ID, &existing.SourceRunID, &existing.TargetScope, &existing.TargetFlowID,
			&existing.TargetDocumentID, &existing.Type, &existing.Content, &existing.ContentHash,
			&existingProvenance, &existing.ExpectedRevision, &existing.ApprovalID, &existing.Status,
			&existing.IdempotencyKey, &existing.Namespace)
	if err == nil {
		_ = json.Unmarshal(existingProvenance, &existing.Provenance)
		if !candidateMatches(&existing, candidate) {
			return nil, ErrCandidateImmutable
		}
		*candidate = existing
		approval, loadErr := approvalByIDPG(ctx, tx, existing.ApprovalID)
		if loadErr != nil {
			return nil, loadErr
		}
		return approval, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	candidate.ID = uuid.NewString()
	approvalID := uuid.NewString()
	candidate.ApprovalID = approvalID
	candidate.Status = "unpublished"
	request := candidateRequest(candidate)
	requestHash := canonicalHash(request)
	approvalKey := "publication:" + candidate.IdempotencyKey
	if _, err = tx.Exec(ctx, `INSERT INTO orh_approval
		(id,namespace_id,run_id,type,subject_type,subject_id,request,request_hash,status,
		 idempotency_key,description,created_by,updated_by,metadata)
		VALUES($1,$2,$3,$4,'knowledge_candidate',$5,$6,$7,'pending',$8,$9,$10,$10,'{}'::jsonb)`,
		approvalID, candidate.Namespace, candidate.SourceRunID, approvalType, candidate.ID,
		jsonBytes(request), requestHash, approvalKey, candidate.Description, principal); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO knw_candidate
		(id,namespace_id,source_run_id,target_scope,target_flow_id,target_document_id,type,content,
		 content_hash,provenance,expected_revision,approval_id,status,idempotency_key,description,
		 created_by,updated_by,metadata)
		VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$8,$9,$10,$11,$12,'unpublished',$13,$14,$15,$15,$16)`,
		candidate.ID, candidate.Namespace, candidate.SourceRunID, candidate.TargetScope,
		candidate.TargetFlowID, candidate.TargetDocumentID, candidate.Type, candidate.Content,
		candidate.ContentHash, jsonBytes(candidate.Provenance), candidate.ExpectedRevision,
		approvalID, candidate.IdempotencyKey, candidate.Description, principal, jsonBytes(candidate.Metadata)); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
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

func candidateBaselinePG(ctx context.Context, tx pgx.Tx, candidate *entities.KnowledgeCandidate) (int64, error) {
	if candidate.Type == "instruction" {
		var revision int64
		if candidate.TargetScope == "namespace" {
			err := tx.QueryRow(ctx, `SELECT COALESCE(i.revision,0) FROM orh_namespace n
				LEFT JOIN llm_instruction i ON i.id=n.instruction_id
				WHERE n.id=$1 FOR SHARE OF n`, candidate.Namespace).Scan(&revision)
			return revision, err
		}
		err := tx.QueryRow(ctx, `SELECT COALESCE(i.revision,0) FROM orh_flow f
			LEFT JOIN llm_instruction i ON i.id=f.instruction_id
			WHERE f.id=$1 AND f.namespace_id=$2 FOR SHARE OF f`, candidate.TargetFlowID, candidate.Namespace).Scan(&revision)
		return revision, err
	}
	if candidate.TargetDocumentID == "" {
		return 0, nil
	}
	var revision int64
	var scope, flowID, aclRef string
	err := tx.QueryRow(ctx, `SELECT COALESCE(r.revision,0),d.scope,COALESCE(d.flow_id,''),COALESCE(d.acl_ref,'')
		FROM knw_document d LEFT JOIN knw_document_revision r ON r.id=d.current_revision_id
		WHERE d.id=$1 AND d.namespace_id=$2 AND d.status<>'DELETED' FOR SHARE OF d`,
		candidate.TargetDocumentID, candidate.Namespace).Scan(&revision, &scope, &flowID, &aclRef)
	if err != nil {
		return 0, fmt.Errorf("resolve target document: %w", err)
	}
	if aclRef != "" || scope != candidate.TargetScope || flowID != candidate.TargetFlowID {
		return 0, fmt.Errorf("%w: target document scope or ACL does not permit publication", ErrPublicationDenied)
	}
	return revision, nil
}

func approvalByIDPG(ctx context.Context, tx pgx.Tx, id string) (*entities.ApprovalInfo, error) {
	var item entities.ApprovalInfo
	var request, decision []byte
	err := tx.QueryRow(ctx, `SELECT id,namespace_id,COALESCE(run_id,''),COALESCE(node_run_id,''),type,
		subject_type,subject_id,request,request_hash,status,COALESCE(decision,'{}'::jsonb),
		COALESCE(decided_by,''),decided_at,expires_at,consumed_at,idempotency_key,created_at,
		COALESCE(created_by,''),updated_at,COALESCE(updated_by,''),row_version
		FROM orh_approval WHERE id=$1`, id).Scan(&item.ID, &item.Namespace, &item.RunID,
		&item.NodeRunID, &item.Type, &item.SubjectType, &item.SubjectID, &request, &item.RequestHash,
		&item.Status, &decision, &item.DecidedBy, &item.DecidedAt, &item.ExpiresAt, &item.ConsumedAt,
		&item.IdempotencyKey, &item.CreatedAt, &item.CreatedBy, &item.UpdatedAt, &item.UpdatedBy,
		&item.RowVersion)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(request, &item.Request)
	_ = json.Unmarshal(decision, &item.Decision)
	item.Token = item.ID
	return &item, nil
}

// ResolveCandidateApproval records the human decision and, when approved,
// publishes the exact immutable request in the same serializable transaction.
// A successful approval is deliberately distinct from a successful publish:
// stale baselines leave an approved approval and a failed candidate so callers
// must create a new request against the new baseline.
func (s *KnowledgePostgresStore) ResolveCandidateApproval(
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
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	candidate, approvalStatus, approvalType, requestHash, expired, err := lockedCandidatePG(ctx, tx, approvalID)
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
			return candidate, tx.Commit(ctx)
		}
		if approvalStatus == "expired" {
			return nil, ErrApprovalExpired
		}
		return nil, fmt.Errorf("approval is already %s", approvalStatus)
	}
	now := time.Now().UTC()
	if expired {
		if err = failCandidateApprovalPG(ctx, tx, candidate, "expired", actor,
			map[string]any{"reason": "approval expired"}, now); err != nil {
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, ErrApprovalExpired
	}
	if !approved {
		if len(decision) == 0 {
			decision["reason"] = "rejected"
		}
		if err = failCandidateApprovalPG(ctx, tx, candidate, "rejected", actor, decision, now); err != nil {
			return nil, err
		}
		candidate.Status = "failed"
		return candidate, tx.Commit(ctx)
	}

	actualRevision, baselineErr := candidateBaselinePG(ctx, tx, candidate)
	if baselineErr != nil {
		return nil, baselineErr
	}
	if actualRevision != candidate.ExpectedRevision {
		decision["publication_error"] = "baseline_changed"
		decision["expected_revision"] = candidate.ExpectedRevision
		decision["actual_revision"] = actualRevision
		if err = failCandidateApprovalPG(ctx, tx, candidate, "approved", actor, decision, now); err != nil {
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: expected revision %d, current revision %d", ErrPublicationStale, candidate.ExpectedRevision, actualRevision)
	}

	var publishedID string
	if candidate.Type == "instruction" {
		publishedID, err = publishInstructionPG(ctx, tx, candidate, actor)
	} else {
		publishedID, err = publishKnowledgePG(ctx, tx, candidate, actor)
	}
	if err != nil {
		return nil, err
	}
	decision["published_id"] = publishedID
	decision["published_revision"] = candidate.ExpectedRevision + 1
	if _, err = tx.Exec(ctx, `UPDATE knw_candidate SET status='published',updated_by=$1,
		metadata=COALESCE(metadata,'{}'::jsonb) || jsonb_build_object('published_id',$2::text)
		WHERE id=$3 AND approval_id=$4 AND status='unpublished'`, actor, publishedID, candidate.ID, approvalID); err != nil {
		return nil, err
	}
	result, err := tx.Exec(ctx, `UPDATE orh_approval SET status='approved',decision=$1,decided_by=$2,
		decided_at=$3,consumed_at=$3,updated_by=$2 WHERE id=$4 AND status='pending'`,
		jsonBytes(decision), actor, now, approvalID)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() != 1 {
		return nil, fmt.Errorf("approval decision CAS conflict")
	}
	payload := map[string]any{
		"candidate_id": candidate.ID, "approval_id": approvalID,
		"type": candidate.Type, "published_id": publishedID,
		"revision": candidate.ExpectedRevision + 1,
	}
	if _, err = tx.Exec(ctx, `INSERT INTO orh_outbox
		(id,namespace_id,aggregate_type,aggregate_id,event_type,payload,status,idempotency_key,
		 created_by,updated_by,metadata)
		VALUES($1,$2,'knowledge_candidate',$3,'publication.completed',$4,'pending',$5,$6,$6,'{}'::jsonb)
		ON CONFLICT(namespace_id,idempotency_key) DO NOTHING`, uuid.NewString(), candidate.Namespace,
		candidate.ID, jsonBytes(payload), "publication:"+candidate.IdempotencyKey+":completed", actor); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	candidate.Status = "published"
	if candidate.Metadata == nil {
		candidate.Metadata = map[string]any{}
	}
	candidate.Metadata["published_id"] = publishedID
	return candidate, nil
}

func lockedCandidatePG(ctx context.Context, tx pgx.Tx, approvalID string) (*entities.KnowledgeCandidate, string, string, string, bool, error) {
	var item entities.KnowledgeCandidate
	var provenance, metadata []byte
	var approvalStatus, approvalType, requestHash string
	var expired bool
	err := tx.QueryRow(ctx, `SELECT c.id,c.namespace_id,c.source_run_id,c.target_scope,
		COALESCE(c.target_flow_id,''),COALESCE(f.name,''),COALESCE(c.target_document_id,''),
		c.type,c.content,c.content_hash,c.provenance,c.expected_revision,c.approval_id,c.status,
		c.idempotency_key,COALESCE(c.description,''),COALESCE(c.metadata,'{}'::jsonb),
		c.created_at,COALESCE(c.created_by,''),c.updated_at,COALESCE(c.updated_by,''),c.row_version,
		a.status,a.type,a.request_hash,(a.expires_at IS NOT NULL AND a.expires_at<=NOW())
		FROM knw_candidate c JOIN orh_approval a ON a.id=c.approval_id AND a.namespace_id=c.namespace_id
		LEFT JOIN orh_flow f ON f.id=c.target_flow_id
		WHERE c.approval_id=$1 FOR UPDATE OF c,a`, approvalID).Scan(
		&item.ID, &item.Namespace, &item.SourceRunID, &item.TargetScope, &item.TargetFlowID,
		&item.TargetFlowName, &item.TargetDocumentID, &item.Type, &item.Content, &item.ContentHash,
		&provenance, &item.ExpectedRevision, &item.ApprovalID, &item.Status, &item.IdempotencyKey,
		&item.Description, &metadata, &item.CreatedAt, &item.CreatedBy, &item.UpdatedAt,
		&item.UpdatedBy, &item.RowVersion, &approvalStatus, &approvalType, &requestHash, &expired)
	if err != nil {
		return nil, "", "", "", false, err
	}
	_ = json.Unmarshal(provenance, &item.Provenance)
	_ = json.Unmarshal(metadata, &item.Metadata)
	return &item, approvalStatus, approvalType, requestHash, expired, nil
}

func failCandidateApprovalPG(ctx context.Context, tx pgx.Tx, candidate *entities.KnowledgeCandidate,
	approvalStatus, actor string, decision map[string]any, now time.Time,
) error {
	result, err := tx.Exec(ctx, `UPDATE orh_approval SET status=$1,decision=$2,decided_by=$3,
		decided_at=$4,consumed_at=CASE WHEN $1='approved' THEN $4 ELSE consumed_at END,updated_by=$3
		WHERE id=$5 AND status='pending'`, approvalStatus, jsonBytes(decision), actor, now, candidate.ApprovalID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("approval decision CAS conflict")
	}
	_, err = tx.Exec(ctx, `UPDATE knw_candidate SET status='failed',updated_by=$1
		WHERE id=$2 AND status='unpublished'`, actor, candidate.ID)
	return err
}

func publishInstructionPG(ctx context.Context, tx pgx.Tx, candidate *entities.KnowledgeCandidate, actor string) (string, error) {
	instructionID := uuid.NewString()
	flowID := any(nil)
	if candidate.TargetScope == "flow" {
		flowID = candidate.TargetFlowID
	}
	if _, err := tx.Exec(ctx, `INSERT INTO llm_instruction
		(id,namespace_id,scope,flow_id,revision,content,content_hash,status,approval_id,
		 description,created_by,updated_by,metadata)
		VALUES($1,$2,$3,$4,$5,$6,$7,'published',$8,$9,$10,$10,$11)`, instructionID,
		candidate.Namespace, candidate.TargetScope, flowID, candidate.ExpectedRevision+1,
		candidate.Content, candidate.ContentHash, candidate.ApprovalID, candidate.Description,
		actor, jsonBytes(candidate.Provenance)); err != nil {
		return "", err
	}
	var result pgconn.CommandTag
	var err error
	if candidate.TargetScope == "namespace" {
		result, err = tx.Exec(ctx, `UPDATE orh_namespace SET instruction_id=$1,updated_by=$2
			WHERE id=$3 AND COALESCE((SELECT revision FROM llm_instruction WHERE id=instruction_id),0)=$4`,
			instructionID, actor, candidate.Namespace, candidate.ExpectedRevision)
	} else {
		result, err = tx.Exec(ctx, `UPDATE orh_flow SET instruction_id=$1,updated_by=$2
			WHERE id=$3 AND namespace_id=$4
			AND COALESCE((SELECT revision FROM llm_instruction WHERE id=instruction_id),0)=$5`,
			instructionID, actor, candidate.TargetFlowID, candidate.Namespace, candidate.ExpectedRevision)
	}
	if err != nil {
		return "", err
	}
	if result.RowsAffected() != 1 {
		return "", ErrPublicationStale
	}
	return instructionID, nil
}

func publishKnowledgePG(ctx context.Context, tx pgx.Tx, candidate *entities.KnowledgeCandidate, actor string) (string, error) {
	var sourceID string
	err := tx.QueryRow(ctx, `SELECT id FROM knw_source WHERE namespace_id=$1 AND lower(name)='built-in-summarizer'
		FOR UPDATE`, candidate.Namespace).Scan(&sourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		sourceID = uuid.NewString()
		_, err = tx.Exec(ctx, `INSERT INTO knw_source
			(id,namespace_id,name,type,config,status,created_by,updated_by,metadata)
			VALUES($1,$2,'built-in-summarizer','run_summary','{}'::jsonb,'ACTIVE',$3,$3,'{}'::jsonb)`,
			sourceID, candidate.Namespace, actor)
	}
	if err != nil {
		return "", err
	}
	documentID := candidate.TargetDocumentID
	newDocument := documentID == ""
	if newDocument {
		documentID = uuid.NewString()
		documentKey := "summary/" + candidate.SourceRunID + "/" + candidate.ID
		_, err = tx.Exec(ctx, `INSERT INTO knw_document
			(id,namespace_id,source_id,document_key,scope,flow_id,classification,status,
			 description,created_by,updated_by,metadata)
			VALUES($1,$2,$3,$4,$5,NULLIF($6,''),'internal','ACTIVE',$7,$8,$8,$9)`,
			documentID, candidate.Namespace, sourceID, documentKey, candidate.TargetScope,
			candidate.TargetFlowID, candidate.Description, actor, jsonBytes(candidate.Metadata))
	} else {
		var scope, flowID, aclRef string
		err = tx.QueryRow(ctx, `SELECT scope,COALESCE(flow_id,''),COALESCE(acl_ref,'')
			FROM knw_document WHERE id=$1 AND namespace_id=$2 AND status<>'DELETED' FOR UPDATE`,
			documentID, candidate.Namespace).Scan(&scope, &flowID, &aclRef)
		if err == nil && (scope != candidate.TargetScope || flowID != candidate.TargetFlowID || aclRef != "") {
			return "", ErrPublicationDenied
		}
	}
	if err != nil {
		return "", err
	}
	revisionID, contentID := uuid.NewString(), uuid.NewString()
	if _, err = tx.Exec(ctx, `INSERT INTO knw_document_revision
		(id,namespace_id,document_id,revision,title,provenance,content_hash,status,approval_id,
		 description,created_by,updated_by,metadata)
		VALUES($1,$2,$3,$4,$5,$6,$7,'published',$8,$9,$10,$10,'{}'::jsonb)`, revisionID,
		candidate.Namespace, documentID, candidate.ExpectedRevision+1,
		candidateDocumentTitle(candidate), jsonBytes(candidate.Provenance), candidate.ContentHash,
		candidate.ApprovalID, candidate.Description, actor); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO knw_content
		(id,namespace_id,document_revision_id,type,ordinal,content,content_hash,created_by,updated_by,metadata)
		VALUES($1,$2,$3,'summary',0,$4,$5,$6,$6,'{}'::jsonb)`, contentID,
		candidate.Namespace, revisionID, candidate.Content, candidate.ContentHash, actor); err != nil {
		return "", err
	}
	result, err := tx.Exec(ctx, `UPDATE knw_document SET current_revision_id=$1,updated_by=$2
		WHERE id=$3 AND namespace_id=$4
		AND COALESCE((SELECT revision FROM knw_document_revision WHERE id=current_revision_id),0)=$5`,
		revisionID, actor, documentID, candidate.Namespace, candidate.ExpectedRevision)
	if err != nil {
		return "", err
	}
	if result.RowsAffected() != 1 {
		return "", ErrPublicationStale
	}
	candidate.TargetDocumentID = documentID
	return documentID, nil
}
