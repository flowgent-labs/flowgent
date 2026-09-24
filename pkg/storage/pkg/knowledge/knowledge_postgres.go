package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type KnowledgePostgresStore struct{ pool *pgxpool.Pool }

func NewKnowledgePostgresStore(pool *pgxpool.Pool) *KnowledgePostgresStore {
	return &KnowledgePostgresStore{pool: pool}
}

const knowledgeProjectionPG = `d.id,dr.id,dr.revision,dr.title,c.id,COALESCE(c.parent_id,''),c.type,
	COALESCE(c.heading_path,''),c.page_start,c.page_end,c.content,s.name,COALESCE(d.source_uri,''),
	COALESCE(dr.source_revision,''),d.scope,COALESCE(d.flow_id,''),
	COALESCE((SELECT name FROM orh_flow WHERE id=d.flow_id),''),COALESCE(d.run_id,''),
	COALESCE(d.acl_ref,''),d.classification,dr.provenance,COALESCE(d.metadata,'{}'::jsonb),
	COALESCE(d.description,''),d.namespace_id,d.status,d.created_at,COALESCE(d.created_by,''),
	d.updated_at,COALESCE(d.updated_by,''),d.row_version`

func scanKnowledgePG(row interface{ Scan(...any) error }) (*entities.KnowledgeEntry, error) {
	var item entities.KnowledgeEntry
	var provenance, metadata []byte
	if err := row.Scan(&item.ID, &item.DocumentRevisionID, &item.Revision, &item.Title,
		&item.ContentID, &item.ParentContentID, &item.ContentKind, &item.HeadingPath,
		&item.PageStart, &item.PageEnd, &item.Content, &item.Source, &item.SourceRef,
		&item.SourceRevision, &item.Scope, &item.FlowID, &item.FlowName, &item.RunID, &item.ACLRef,
		&item.Classification, &provenance, &metadata, &item.Description, &item.Namespace,
		&item.Status, &item.CreatedAt, &item.CreatedBy, &item.UpdatedAt, &item.UpdatedBy,
		&item.RowVersion); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(provenance, &item.Provenance)
	_ = json.Unmarshal(metadata, &item.Metadata)
	item.Tags = stringSlice(item.Metadata["tags"])
	item.ContentType, _ = item.Metadata["content_type"].(string)
	if item.ContentType == "" {
		item.ContentType = "text/plain"
	}
	return &item, nil
}

func (s *KnowledgePostgresStore) Get(ctx context.Context, namespace, id string) (*entities.KnowledgeEntry, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "knw_document").PostgresWhere(3)
	args := append([]any{namespace, id}, scopeArgs...)
	query := `SELECT ` + knowledgeProjectionPG + ` FROM knw_document d
		JOIN knw_document_revision dr ON dr.id=d.current_revision_id
		JOIN LATERAL (SELECT * FROM knw_content value WHERE value.document_revision_id=dr.id
			ORDER BY CASE value.type WHEN 'summary' THEN 0 WHEN 'section' THEN 1 ELSE 2 END,value.ordinal,value.id LIMIT 1) c ON TRUE
		JOIN knw_source s ON s.id=d.source_id
		WHERE d.namespace_id=$1 AND d.id=$2 AND d.status<>'DELETED'
		AND d.id IN (SELECT id FROM knw_document WHERE (` + scopeWhere + `))`
	item, err := scanKnowledgePG(s.pool.QueryRow(ctx, query, args...))
	if err != nil {
		return nil, fmt.Errorf("knowledge document: %w", err)
	}
	return item, nil
}

func (s *KnowledgePostgresStore) List(ctx context.Context, filter ListFilter) (*entities.Page[entities.KnowledgeEntry], error) {
	page := normalizePage(filter.Page)
	clauses := []string{"d.namespace_id=$1", "d.status<>'DELETED'", "d.current_revision_id IS NOT NULL"}
	args := []any{filter.Namespace}
	if filter.Scope != "" {
		args = append(args, strings.ToLower(filter.Scope))
		clauses = append(clauses, fmt.Sprintf("d.scope=$%d", len(args)))
	}
	if len(filter.Tags) > 0 {
		placeholders := make([]string, 0, len(filter.Tags))
		for _, tag := range filter.Tags {
			args = append(args, tag)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		clauses = append(clauses, "COALESCE(d.metadata->'tags','[]'::jsonb) ?| ARRAY["+strings.Join(placeholders, ",")+"]")
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "knw_document").PostgresWhere(len(args) + 1)
	clauses = append(clauses, "d.id IN (SELECT id FROM knw_document WHERE ("+scopeWhere+"))")
	args = append(args, scopeArgs...)
	where := strings.Join(clauses, " AND ")
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM knw_document d WHERE `+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	limitPos := len(args) + 1
	args = append(args, page.Size, (page.Page-1)*page.Size)
	query := fmt.Sprintf(`SELECT %s FROM knw_document d
		JOIN knw_document_revision dr ON dr.id=d.current_revision_id
		JOIN LATERAL (SELECT * FROM knw_content value WHERE value.document_revision_id=dr.id
			ORDER BY CASE value.type WHEN 'summary' THEN 0 WHEN 'section' THEN 1 ELSE 2 END,value.ordinal,value.id LIMIT 1) c ON TRUE
		JOIN knw_source s ON s.id=d.source_id WHERE %s
		ORDER BY d.updated_at DESC,d.id LIMIT $%d OFFSET $%d`, knowledgeProjectionPG, where, limitPos, limitPos+1)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.KnowledgeEntry, 0)
	for rows.Next() {
		item, scanErr := scanKnowledgePG(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entities.NewPage(items, total, page), nil
}

func (s *KnowledgePostgresStore) Save(ctx context.Context, entry *entities.KnowledgeEntry) error {
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.Namespace == "" {
		entry.Namespace = "default"
	}
	scope, flowID, runID, err := normalizeKnowledgeScope(entry)
	if err != nil {
		return err
	}
	visible, err := knowledgeCandidateVisiblePG(ctx, s.pool, entry.Namespace, entry.ID)
	if err != nil {
		return err
	}
	if !visible {
		return storage.ErrFlowgentSqlScopeDenied
	}
	principal := entry.UpdatedBy
	if principal == "" {
		principal = entry.CreatedBy
	}
	if principal == "" {
		principal = "system:flowgent"
	}
	sourceName := strings.TrimSpace(entry.Source)
	if sourceName == "" {
		sourceName = "api-managed"
	}
	contentKind := normalizeContentKind(entry.ContentKind)
	contentHash := hashText(entry.Content)
	metadata := knowledgeMetadata(entry)
	provenance := entry.Provenance
	if provenance == nil {
		provenance = map[string]any{"source_uri": entry.SourceRef, "ingested_by": principal}
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO orh_namespace(id,name,description,created_by,updated_by)
		VALUES($1::varchar(64),$1::text,'Flowgent namespace',$2::varchar(255),$2::varchar(255))
		ON CONFLICT(id) DO NOTHING`, entry.Namespace, principal); err != nil {
		return err
	}
	flowName := ""
	if scope == "flow" {
		flowKey := flowID
		if err = tx.QueryRow(ctx, `SELECT id,name FROM orh_flow WHERE namespace_id=$1
			AND (id=$2 OR name=$2) AND status<>'DELETED' FOR SHARE`, entry.Namespace, flowKey).
			Scan(&flowID, &flowName); err != nil {
			return fmt.Errorf("resolve knowledge flow: %w", err)
		}
	} else if scope == "run" {
		flowKey := flowID
		if err = tx.QueryRow(ctx, `SELECT f.id,f.name FROM orh_flow f JOIN orh_run r
			ON r.flow_id=f.id AND r.namespace_id=f.namespace_id WHERE f.namespace_id=$1
			AND (f.id=$2 OR f.name=$2) AND r.id=$3 AND f.status<>'DELETED' FOR SHARE OF f,r`,
			entry.Namespace, flowKey, runID).Scan(&flowID, &flowName); err != nil {
			return fmt.Errorf("resolve run-scoped knowledge owner: %w", err)
		}
	}
	var sourceID string
	err = tx.QueryRow(ctx, `SELECT id FROM knw_source WHERE namespace_id=$1 AND lower(name)=lower($2) FOR UPDATE`, entry.Namespace, sourceName).Scan(&sourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		sourceID = uuid.NewString()
		_, err = tx.Exec(ctx, `INSERT INTO knw_source(id,namespace_id,name,type,config,status,created_by,updated_by)
			VALUES($1,$2,$3,$4,'{}'::jsonb,'ACTIVE',$5,$5)`, sourceID, entry.Namespace, sourceName,
			defaultString(entry.ContentType, "manual"), principal)
	}
	if err != nil {
		return err
	}
	var rowVersion, revision int64
	var existingSource, existingScope, existingFlow, existingRun string
	err = tx.QueryRow(ctx, `SELECT row_version,source_id,scope,COALESCE(flow_id,''),COALESCE(run_id,'')
		FROM knw_document WHERE namespace_id=$1 AND id=$2 FOR UPDATE`, entry.Namespace, entry.ID).
		Scan(&rowVersion, &existingSource, &existingScope, &existingFlow, &existingRun)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx, `INSERT INTO knw_document
			(id,namespace_id,source_id,document_key,scope,flow_id,run_id,source_uri,classification,
			 description,status,created_by,updated_by,metadata)
			VALUES($1,$2,$3,$1,$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),$8,$9,'ACTIVE',$10,$10,$11)`,
			entry.ID, entry.Namespace, sourceID, scope, flowID, runID, entry.SourceRef,
			defaultString(entry.Classification, "internal"), entry.Description, principal, jsonBytes(metadata))
		rowVersion, revision = 1, 1
	} else if err == nil {
		if existingSource != sourceID || existingScope != scope || existingFlow != flowID || existingRun != runID {
			return fmt.Errorf("knowledge document source and scope are immutable")
		}
		err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM knw_document_revision WHERE document_id=$1`, entry.ID).Scan(&revision)
	} else {
		return err
	}
	if err != nil {
		return err
	}
	revisionID, contentID := uuid.NewString(), uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO knw_document_revision
		(id,namespace_id,document_id,revision,source_revision,title,source_path,provenance,content_hash,
		 status,description,created_by,updated_by,metadata)
		VALUES($1,$2,$3,$4,NULLIF($5,''),$6,NULLIF($7,''),$8,$9,'draft',$10,$11,$11,'{}'::jsonb)`,
		revisionID, entry.Namespace, entry.ID, revision, entry.SourceRevision,
		defaultString(entry.Title, entry.ID), entry.SourceRef, jsonBytes(provenance), contentHash,
		entry.Description, principal)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO knw_content
		(id,namespace_id,document_revision_id,parent_id,type,ordinal,heading_path,page_start,page_end,
		 content,content_hash,created_by,updated_by,metadata)
		VALUES($1,$2,$3,NULL,$4,0,NULLIF($5,''),$6,$7,$8,$9,$10,$10,'{}'::jsonb)`, contentID,
		entry.Namespace, revisionID, contentKind, entry.HeadingPath, entry.PageStart, entry.PageEnd,
		entry.Content, contentHash, principal)
	if err != nil {
		return err
	}
	// A draft is not retrieval-visible. The publication transaction changes the
	// owner pointer only after content/index preparation and approval succeed.
	result, err := tx.Exec(ctx, `UPDATE knw_document SET description=$1,
		status='ACTIVE',updated_by=$2,metadata=$3 WHERE id=$4 AND namespace_id=$5 AND row_version=$6`,
		entry.Description, principal, jsonBytes(metadata), entry.ID, entry.Namespace, rowVersion)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("knowledge document revision CAS conflict")
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	entry.DocumentRevisionID, entry.Revision, entry.ContentID = revisionID, revision, contentID
	entry.Scope, entry.FlowID, entry.FlowName, entry.RunID = scope, flowID, flowName, runID
	entry.Status, entry.RowVersion = "draft", rowVersion+1
	return nil
}

func (s *KnowledgePostgresStore) Delete(ctx context.Context, namespace, id string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "knw_document").PostgresWhere(3)
	args := append([]any{namespace, id}, scopeArgs...)
	result, err := s.pool.Exec(ctx, `UPDATE knw_document SET status='DELETED'
		WHERE namespace_id=$1 AND id=$2 AND status<>'DELETED'
		AND id IN (SELECT id FROM knw_document WHERE (`+scopeWhere+`))`, args...)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("knowledge document not found")
	}
	return nil
}

func (s *KnowledgePostgresStore) Search(ctx context.Context, namespace string, req entities.KnowledgeSearchRequest) ([]*entities.KnowledgeEntry, error) {
	var err error
	req, err = s.resolveSearchScope(ctx, namespace, req)
	if err != nil {
		return nil, err
	}
	topK := normalizedTopK(req.TopK)
	if len(req.Embedding) > 0 {
		return s.searchVector(ctx, namespace, req, topK)
	}
	if strings.TrimSpace(req.Query) == "" {
		return []*entities.KnowledgeEntry{}, nil
	}
	ftsQuery := strings.Join(searchTerms(req.Query), " | ")
	clauses, args, err := searchGuardsPG(ctx, namespace, req, 3)
	if err != nil {
		return nil, err
	}
	args = append([]any{namespace, ftsQuery}, args...)
	limitPos := len(args) + 1
	args = append(args, topK)
	query := fmt.Sprintf(`SELECT %s FROM knw_content c
		JOIN knw_document_revision dr ON dr.id=c.document_revision_id
		JOIN knw_document d ON d.id=dr.document_id AND d.current_revision_id=dr.id
		JOIN knw_source s ON s.id=d.source_id
		WHERE d.namespace_id=$1 AND dr.status='published' AND c.search_vector @@ to_tsquery('simple',$2)
		AND %s ORDER BY ts_rank(c.search_vector,to_tsquery('simple',$2)) DESC,c.ordinal,c.id LIMIT $%d`,
		knowledgeProjectionPG, strings.Join(clauses, " AND "), limitPos)
	return s.queryKnowledge(ctx, query, args...)
}

func (s *KnowledgePostgresStore) resolveSearchScope(ctx context.Context, namespace string, req entities.KnowledgeSearchRequest) (entities.KnowledgeSearchRequest, error) {
	scope := strings.ToLower(defaultString(req.Scope, "namespace"))
	if scope == "namespace" {
		return req, nil
	}
	flowKey := req.FlowName
	if flowKey == "" {
		flowKey = req.FlowID
	}
	if flowKey == "" {
		return req, fmt.Errorf("flow_id or flow_name is required for %s knowledge search", scope)
	}
	if scope == "flow" {
		if err := s.pool.QueryRow(ctx, `SELECT id,name FROM orh_flow WHERE namespace_id=$1
			AND (id=$2 OR name=$2) AND status<>'DELETED'`, namespace, flowKey).
			Scan(&req.FlowID, &req.FlowName); err != nil {
			return req, fmt.Errorf("resolve knowledge flow: %w", err)
		}
		return req, nil
	}
	if scope == "run" {
		if req.RunID == "" {
			return req, fmt.Errorf("run_id is required for run knowledge search")
		}
		if err := s.pool.QueryRow(ctx, `SELECT f.id,f.name FROM orh_flow f JOIN orh_run r
			ON r.flow_id=f.id AND r.namespace_id=f.namespace_id WHERE f.namespace_id=$1
			AND (f.id=$2 OR f.name=$2) AND r.id=$3`, namespace, flowKey, req.RunID).
			Scan(&req.FlowID, &req.FlowName); err != nil {
			return req, fmt.Errorf("resolve run-scoped knowledge owner: %w", err)
		}
		return req, nil
	}
	return req, fmt.Errorf("invalid knowledge scope %q", req.Scope)
}

func (s *KnowledgePostgresStore) searchVector(ctx context.Context, namespace string, req entities.KnowledgeSearchRequest, topK int) ([]*entities.KnowledgeEntry, error) {
	if strings.TrimSpace(req.ProfileKey) == "" {
		return nil, fmt.Errorf("profile_key is required for vector search")
	}
	var dimensions int
	var metric string
	if err := s.pool.QueryRow(ctx, `SELECT dimensions,distance_metric FROM knw_embedding_profile
		WHERE namespace_id=$1 AND profile_key=$2 AND status='ACTIVE'`, namespace, req.ProfileKey).Scan(&dimensions, &metric); err != nil {
		return nil, fmt.Errorf("embedding profile: %w", err)
	}
	if len(req.Embedding) != dimensions {
		return nil, fmt.Errorf("embedding dimensions %d do not match profile dimensions %d", len(req.Embedding), dimensions)
	}
	operator := map[string]string{"cosine": "<=>", "l2": "<->", "l1": "<+>"}[metric]
	if operator == "" {
		return nil, fmt.Errorf("unsupported distance metric %q", metric)
	}
	clauses, guardArgs, err := searchGuardsPG(ctx, namespace, req, 4)
	if err != nil {
		return nil, err
	}
	args := []any{namespace, req.ProfileKey, vectorLiteral(req.Embedding)}
	args = append(args, guardArgs...)
	if strings.TrimSpace(req.Query) != "" {
		args = append(args, strings.Join(searchTerms(req.Query), " | "))
		clauses = append(clauses, fmt.Sprintf("c.search_vector @@ to_tsquery('simple',$%d)", len(args)))
	}
	limitPos := len(args) + 1
	args = append(args, topK)
	distanceExpression := fmt.Sprintf("e.embedding %s $3::vector", operator)
	if dimensions <= 2000 {
		distanceExpression = fmt.Sprintf("e.embedding::vector(%d) %s $3::vector(%d)", dimensions, operator, dimensions)
	} else if dimensions <= 4000 {
		distanceExpression = fmt.Sprintf("e.embedding::halfvec(%d) %s $3::halfvec(%d)", dimensions, operator, dimensions)
	}
	query := fmt.Sprintf(`SELECT %s FROM knw_embedding e
		JOIN knw_content c ON c.id=e.content_id
		JOIN knw_document_revision dr ON dr.id=c.document_revision_id
		JOIN knw_document d ON d.id=dr.document_id AND d.current_revision_id=dr.id
		JOIN knw_source s ON s.id=d.source_id
		WHERE d.namespace_id=$1 AND e.profile_key=$2 AND dr.status='published' AND %s
		ORDER BY %s,c.id LIMIT $%d`, knowledgeProjectionPG,
		strings.Join(clauses, " AND "), distanceExpression, limitPos)
	items, err := s.queryKnowledge(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		item.ProfileKey = req.ProfileKey
	}
	return items, nil
}

func searchGuardsPG(ctx context.Context, namespace string, req entities.KnowledgeSearchRequest, firstParameter int) ([]string, []any, error) {
	_ = namespace
	clauses := []string{"d.status='ACTIVE'", "COALESCE(d.acl_ref,'')=''"}
	args := make([]any, 0)
	scopeClause, scopeArgs, err := knowledgeScopePredicatePG(req, firstParameter)
	if err != nil {
		return nil, nil, err
	}
	clauses = append(clauses, scopeClause)
	args = append(args, scopeArgs...)
	if len(req.Tags) > 0 {
		placeholders := make([]string, 0, len(req.Tags))
		for _, tag := range req.Tags {
			placeholders = append(placeholders, fmt.Sprintf("$%d", firstParameter+len(args)))
			args = append(args, tag)
		}
		clauses = append(clauses, "COALESCE(d.metadata->'tags','[]'::jsonb) ?| ARRAY["+strings.Join(placeholders, ",")+"]")
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "knw_document").PostgresWhere(firstParameter + len(args))
	clauses = append(clauses, "d.id IN (SELECT id FROM knw_document WHERE ("+scopeWhere+"))")
	args = append(args, scopeArgs...)
	return clauses, args, nil
}

func knowledgeScopePredicatePG(req entities.KnowledgeSearchRequest, first int) (string, []any, error) {
	switch strings.ToLower(defaultString(req.Scope, "namespace")) {
	case "namespace":
		return "d.scope='namespace'", nil, nil
	case "flow":
		if req.FlowID == "" {
			return "", nil, fmt.Errorf("flow_id is required for flow knowledge search")
		}
		return fmt.Sprintf("(d.scope='namespace' OR (d.scope='flow' AND d.flow_id=$%d))", first), []any{req.FlowID}, nil
	case "run":
		if req.FlowID == "" || req.RunID == "" {
			return "", nil, fmt.Errorf("flow_id and run_id are required for run knowledge search")
		}
		return fmt.Sprintf("(d.scope='namespace' OR (d.scope='flow' AND d.flow_id=$%d) OR (d.scope='run' AND d.flow_id=$%d AND d.run_id=$%d))", first, first, first+1), []any{req.FlowID, req.RunID}, nil
	default:
		return "", nil, fmt.Errorf("invalid knowledge scope %q", req.Scope)
	}
}

func (s *KnowledgePostgresStore) queryKnowledge(ctx context.Context, query string, args ...any) ([]*entities.KnowledgeEntry, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("knowledge search: %w", err)
	}
	defer rows.Close()
	items := make([]*entities.KnowledgeEntry, 0)
	for rows.Next() {
		item, scanErr := scanKnowledgePG(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if expandErr := s.expandContext(ctx, item); expandErr != nil {
			return nil, expandErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *KnowledgePostgresStore) expandContext(ctx context.Context, item *entities.KnowledgeEntry) error {
	rows, err := s.pool.Query(ctx, `SELECT id,type,ordinal,content FROM knw_content
		WHERE document_revision_id=$1 AND (id=$2 OR id=$3 OR ordinal IN (
			SELECT ordinal-1 FROM knw_content WHERE id=$2 UNION SELECT ordinal+1 FROM knw_content WHERE id=$2
		)) ORDER BY ordinal,id`, item.DocumentRevisionID, item.ContentID, nilIfEmpty(item.ParentContentID))
	if err != nil {
		return err
	}
	defer rows.Close()
	expanded := make([]map[string]any, 0, 4)
	for rows.Next() {
		var id, kind, content string
		var ordinal int
		if err := rows.Scan(&id, &kind, &ordinal, &content); err != nil {
			return err
		}
		expanded = append(expanded, map[string]any{"content_id": id, "type": kind, "ordinal": ordinal, "content": content})
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	item.Metadata["context_expansion"] = expanded
	return rows.Err()
}

func (s *KnowledgePostgresStore) ListTags(ctx context.Context, namespace, scope string) ([]string, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "knw_document").PostgresWhere(3)
	args := []any{namespace, strings.ToLower(scope)}
	query := `SELECT DISTINCT tag.value FROM knw_document d
		CROSS JOIN LATERAL jsonb_array_elements_text(COALESCE(d.metadata->'tags','[]'::jsonb)) tag(value)
		WHERE d.namespace_id=$1 AND d.status<>'DELETED' AND ($2='' OR d.scope=$2)
		AND d.id IN (SELECT id FROM knw_document WHERE (` + scopeWhere + `)) ORDER BY tag.value`
	args = append(args, scopeArgs...)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (s *KnowledgePostgresStore) CreateEmbeddingProfile(ctx context.Context, profile *entities.EmbeddingProfile) error {
	if err := validateEmbeddingProfile(profile); err != nil {
		return err
	}
	if profile.Dimensions > postgresVectorMaxDimensions {
		return fmt.Errorf("pgvector supports at most %d dimensions", postgresVectorMaxDimensions)
	}
	if profile.ID == "" {
		profile.ID = uuid.NewString()
	}
	principal := defaultString(profile.CreatedBy, "system:flowgent")
	if _, err := s.pool.Exec(ctx, `INSERT INTO orh_namespace(id,name,description,created_by,updated_by)
		VALUES($1::varchar(64),$1::text,'Flowgent namespace',$2::varchar(255),$2::varchar(255))
		ON CONFLICT(id) DO NOTHING`, profile.Namespace, principal); err != nil {
		return err
	}
	metadata, _ := json.Marshal(profile.Metadata)
	_, err := s.pool.Exec(ctx, `INSERT INTO knw_embedding_profile
		(id,namespace_id,profile_key,provider_type,model,model_revision,dimensions,distance_metric,
		 status,created_by,updated_by,metadata)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,'ACTIVE',$9,$9,$10)`, profile.ID, profile.Namespace,
		profile.ProfileKey, profile.ProviderType, profile.Model, profile.ModelRevision, profile.Dimensions,
		profile.DistanceMetric, principal, metadata)
	return err
}

func (s *KnowledgePostgresStore) PutEmbedding(ctx context.Context, embedding *entities.KnowledgeEmbedding) error {
	if embedding.ID == "" {
		embedding.ID = uuid.NewString()
	}
	var dimensions int
	var metric string
	if err := s.pool.QueryRow(ctx, `SELECT dimensions,distance_metric FROM knw_embedding_profile
		WHERE namespace_id=$1 AND profile_key=$2 AND status='ACTIVE'`, embedding.Namespace, embedding.ProfileKey).Scan(&dimensions, &metric); err != nil {
		return err
	}
	if len(embedding.Embedding) != dimensions {
		return fmt.Errorf("embedding dimensions %d do not match profile dimensions %d", len(embedding.Embedding), dimensions)
	}
	if embedding.InputHash == "" {
		embedding.InputHash = hashText(vectorLiteral(embedding.Embedding))
	}
	embedding.Dimensions, embedding.DistanceMetric = dimensions, metric
	principal := defaultString(embedding.CreatedBy, "system:flowgent")
	metadata, _ := json.Marshal(embedding.Metadata)
	result, err := s.pool.Exec(ctx, `INSERT INTO knw_embedding
		(id,namespace_id,content_id,profile_key,dimensions,distance_metric,input_hash,embedding,
		 created_by,updated_by,metadata)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8::vector,$9,$9,$10)
		ON CONFLICT(content_id,profile_key) DO NOTHING`, embedding.ID, embedding.Namespace, embedding.ContentID,
		embedding.ProfileKey, dimensions, metric, embedding.InputHash, vectorLiteral(embedding.Embedding),
		principal, metadata)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	var existingID, inputHash string
	if err = s.pool.QueryRow(ctx, `SELECT id,input_hash FROM knw_embedding
		WHERE content_id=$1 AND profile_key=$2`, embedding.ContentID, embedding.ProfileKey).Scan(&existingID, &inputHash); err != nil {
		return err
	}
	if inputHash != embedding.InputHash {
		return fmt.Errorf("embedding is immutable for content/profile; create a new content revision")
	}
	embedding.ID = existingID
	return nil
}

func knowledgeCandidateVisiblePG(ctx context.Context, pool *pgxpool.Pool, namespace, id string) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "knw_document").PostgresWhere(3)
	args := append([]any{namespace, id}, scopeArgs...)
	var visible int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM (SELECT CAST($1 AS TEXT) namespace_id,CAST($2 AS TEXT) id) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}

func vectorLiteral(values []float32) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = strconv.FormatFloat(float64(value), 'g', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func jsonBytes(value any) []byte { data, _ := json.Marshal(value); return data }

func hashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

var _ = time.Time{}
