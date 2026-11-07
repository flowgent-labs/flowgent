package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
)

type KnowledgeSQLiteStore struct{ conn *sql.DB }

func NewKnowledgeSQLiteStore(conn *sql.DB) *KnowledgeSQLiteStore {
	return &KnowledgeSQLiteStore{conn: conn}
}

const knowledgeProjectionSQLite = `d.id,dr.id,dr.revision,dr.title,c.id,COALESCE(c.parent_id,''),c.type,
	COALESCE(c.heading_path,''),c.page_start,c.page_end,c.content,s.name,COALESCE(d.source_uri,''),
	COALESCE(dr.source_revision,''),d.scope,COALESCE(d.flow_id,''),
	COALESCE((SELECT name FROM orh_flow WHERE id=d.flow_id),''),COALESCE(d.run_id,''),
	COALESCE(d.acl_ref,''),d.classification,dr.provenance,COALESCE(d.metadata,'{}'),
	COALESCE(d.description,''),d.namespace_id,d.status,d.created_at,COALESCE(d.created_by,''),
	d.updated_at,COALESCE(d.updated_by,''),d.row_version`

func scanKnowledgeSQLite(row interface{ Scan(...any) error }) (*entities.KnowledgeEntry, error) {
	var item entities.KnowledgeEntry
	var provenance, metadata, createdAt, updatedAt string
	if err := row.Scan(&item.ID, &item.DocumentRevisionID, &item.Revision, &item.Title,
		&item.ContentID, &item.ParentContentID, &item.ContentKind, &item.HeadingPath,
		&item.PageStart, &item.PageEnd, &item.Content, &item.Source, &item.SourceRef,
		&item.SourceRevision, &item.Scope, &item.FlowID, &item.FlowName, &item.RunID, &item.ACLRef,
		&item.Classification, &provenance, &metadata, &item.Description, &item.Namespace,
		&item.Status, &createdAt, &item.CreatedBy, &updatedAt, &item.UpdatedBy,
		&item.RowVersion); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(provenance), &item.Provenance)
	_ = json.Unmarshal([]byte(metadata), &item.Metadata)
	item.Tags = stringSlice(item.Metadata["tags"])
	item.ContentType, _ = item.Metadata["content_type"].(string)
	if item.ContentType == "" {
		item.ContentType = "text/plain"
	}
	item.CreatedAt, _ = utils.ParseTime(createdAt)
	item.UpdatedAt, _ = utils.ParseTime(updatedAt)
	return &item, nil
}

func (s *KnowledgeSQLiteStore) Get(ctx context.Context, namespace, id string) (*entities.KnowledgeEntry, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "knw_document").SQLiteWhere()
	args := append([]any{namespace, id}, scopeArgs...)
	query := `SELECT ` + knowledgeProjectionSQLite + ` FROM knw_document d
		JOIN knw_document_revision dr ON dr.id=d.current_revision_id
		JOIN knw_content c ON c.id=(SELECT value.id FROM knw_content value
			WHERE value.document_revision_id=dr.id
			ORDER BY CASE value.type WHEN 'summary' THEN 0 WHEN 'section' THEN 1 ELSE 2 END,value.ordinal,value.id LIMIT 1)
		JOIN knw_source s ON s.id=d.source_id
		WHERE d.namespace_id=? AND d.id=? AND d.status<>'DELETED'
		AND d.id IN (SELECT id FROM knw_document WHERE (` + scopeWhere + `))`
	item, err := scanKnowledgeSQLite(s.conn.QueryRowContext(ctx, query, args...))
	if err != nil {
		return nil, fmt.Errorf("knowledge document: %w", err)
	}
	return item, nil
}

func (s *KnowledgeSQLiteStore) List(ctx context.Context, filter ListFilter) (*entities.Page[entities.KnowledgeEntry], error) {
	page := normalizePage(filter.Page)
	clauses := []string{"d.namespace_id=?", "d.status<>'DELETED'", "d.current_revision_id IS NOT NULL"}
	args := []any{filter.Namespace}
	if filter.Scope != "" {
		clauses = append(clauses, "d.scope=?")
		args = append(args, strings.ToLower(filter.Scope))
	}
	if len(filter.Tags) > 0 {
		placeholders := make([]string, len(filter.Tags))
		for index, tag := range filter.Tags {
			placeholders[index] = "?"
			args = append(args, tag)
		}
		clauses = append(clauses, "EXISTS (SELECT 1 FROM json_each(COALESCE(d.metadata,'{}'),'$.tags') WHERE value IN ("+strings.Join(placeholders, ",")+"))")
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "knw_document").SQLiteWhere()
	clauses = append(clauses, "d.id IN (SELECT id FROM knw_document WHERE ("+scopeWhere+"))")
	args = append(args, scopeArgs...)
	where := strings.Join(clauses, " AND ")
	var total int64
	if err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM knw_document d WHERE `+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	queryArgs := append(append([]any{}, args...), page.Size, (page.Page-1)*page.Size)
	query := `SELECT ` + knowledgeProjectionSQLite + ` FROM knw_document d
		JOIN knw_document_revision dr ON dr.id=d.current_revision_id
		JOIN knw_content c ON c.id=(SELECT value.id FROM knw_content value
			WHERE value.document_revision_id=dr.id
			ORDER BY CASE value.type WHEN 'summary' THEN 0 WHEN 'section' THEN 1 ELSE 2 END,value.ordinal,value.id LIMIT 1)
		JOIN knw_source s ON s.id=d.source_id WHERE ` + where + `
		ORDER BY d.updated_at DESC,d.id LIMIT ? OFFSET ?`
	rows, err := s.conn.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.KnowledgeEntry, 0)
	for rows.Next() {
		item, scanErr := scanKnowledgeSQLite(rows)
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

func (s *KnowledgeSQLiteStore) Save(ctx context.Context, entry *entities.KnowledgeEntry) error {
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
	visible, err := knowledgeCandidateVisibleSQLite(ctx, s.conn, entry.Namespace, entry.ID)
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

	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO orh_namespace
		(id,name,description,created_by,updated_by) VALUES(?,?,?, ?,?)`, entry.Namespace, entry.Namespace,
		"Flowgent namespace", principal, principal); err != nil {
		return err
	}
	flowName := ""
	if scope == "flow" {
		flowKey := flowID
		if err = tx.QueryRowContext(ctx, `SELECT id,name FROM orh_flow WHERE namespace_id=?
			AND (id=? OR name=?) AND status<>'DELETED'`, entry.Namespace, flowKey, flowKey).
			Scan(&flowID, &flowName); err != nil {
			return fmt.Errorf("resolve knowledge flow: %w", err)
		}
	} else if scope == "run" {
		flowKey := flowID
		if err = tx.QueryRowContext(ctx, `SELECT f.id,f.name FROM orh_flow f JOIN orh_run r
			ON r.flow_id=f.id AND r.namespace_id=f.namespace_id WHERE f.namespace_id=?
			AND (f.id=? OR f.name=?) AND r.id=? AND f.status<>'DELETED'`, entry.Namespace, flowKey,
			flowKey, runID).Scan(&flowID, &flowName); err != nil {
			return fmt.Errorf("resolve run-scoped knowledge owner: %w", err)
		}
	}
	var sourceID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM knw_source WHERE namespace_id=? AND lower(name)=lower(?)`, entry.Namespace, sourceName).Scan(&sourceID)
	if errors.Is(err, sql.ErrNoRows) {
		sourceID = uuid.NewString()
		_, err = tx.ExecContext(ctx, `INSERT INTO knw_source
			(id,namespace_id,name,type,config,status,created_by,updated_by)
			VALUES(?,?,?,?,'{}','ACTIVE',?,?)`, sourceID, entry.Namespace, sourceName,
			defaultString(entry.ContentType, "manual"), principal, principal)
	}
	if err != nil {
		return err
	}
	var rowVersion, revision int64
	var existingSource, existingScope, existingFlow, existingRun string
	err = tx.QueryRowContext(ctx, `SELECT row_version,source_id,scope,COALESCE(flow_id,''),COALESCE(run_id,'')
		FROM knw_document WHERE namespace_id=? AND id=?`, entry.Namespace, entry.ID).
		Scan(&rowVersion, &existingSource, &existingScope, &existingFlow, &existingRun)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO knw_document
			(id,namespace_id,source_id,document_key,scope,flow_id,run_id,source_uri,classification,
			 description,status,created_by,updated_by,metadata)
			VALUES(?,?,?,?,?,NULLIF(?,''),NULLIF(?,''),NULLIF(?,''),?,?,'ACTIVE',?,?,?)`, entry.ID,
			entry.Namespace, sourceID, entry.ID, scope, flowID, runID, entry.SourceRef,
			defaultString(entry.Classification, "internal"), entry.Description, principal, principal,
			string(jsonBytes(metadata)))
		rowVersion, revision = 1, 1
	} else if err == nil {
		if existingSource != sourceID || existingScope != scope || existingFlow != flowID || existingRun != runID {
			return fmt.Errorf("knowledge document source and scope are immutable")
		}
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM knw_document_revision WHERE document_id=?`, entry.ID).Scan(&revision)
	} else {
		return err
	}
	if err != nil {
		return err
	}
	revisionID, contentID := uuid.NewString(), uuid.NewString()
	_, err = tx.ExecContext(ctx, `INSERT INTO knw_document_revision
		(id,namespace_id,document_id,revision,source_revision,title,source_path,provenance,content_hash,
		 status,description,created_by,updated_by,metadata)
		VALUES(?,?,?,?,NULLIF(?,''),?,NULLIF(?,''),?,?,'draft',?,?,?,'{}')`, revisionID,
		entry.Namespace, entry.ID, revision, entry.SourceRevision, defaultString(entry.Title, entry.ID),
		entry.SourceRef, string(jsonBytes(provenance)), contentHash, entry.Description, principal, principal)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO knw_content
		(id,namespace_id,document_revision_id,parent_id,type,ordinal,heading_path,page_start,page_end,
		 content,content_hash,created_by,updated_by,metadata)
		VALUES(?,?,?,NULL,?,0,NULLIF(?,''),?,?,?,?,?,?,'{}')`, contentID, entry.Namespace, revisionID,
		contentKind, entry.HeadingPath, entry.PageStart, entry.PageEnd, entry.Content, contentHash,
		principal, principal)
	if err != nil {
		return err
	}
	// A draft is not retrieval-visible. The publication transaction changes the
	// owner pointer only after content/index preparation and approval succeed.
	result, err := tx.ExecContext(ctx, `UPDATE knw_document SET description=?,
		status='ACTIVE',updated_by=?,metadata=? WHERE id=? AND namespace_id=? AND row_version=?`,
		entry.Description, principal, string(jsonBytes(metadata)), entry.ID, entry.Namespace, rowVersion)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("knowledge document revision CAS conflict")
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	entry.DocumentRevisionID, entry.Revision, entry.ContentID = revisionID, revision, contentID
	entry.Scope, entry.FlowID, entry.FlowName, entry.RunID = scope, flowID, flowName, runID
	entry.Status, entry.RowVersion = "draft", rowVersion+1
	return nil
}

func (s *KnowledgeSQLiteStore) Delete(ctx context.Context, namespace, id string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "knw_document").SQLiteWhere()
	args := append([]any{namespace, id}, scopeArgs...)
	result, err := s.conn.ExecContext(ctx, `UPDATE knw_document SET status='DELETED'
		WHERE namespace_id=? AND id=? AND status<>'DELETED'
		AND id IN (SELECT id FROM knw_document WHERE (`+scopeWhere+`))`, args...)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("knowledge document not found")
	}
	return nil
}

func (s *KnowledgeSQLiteStore) Search(ctx context.Context, namespace string, req entities.KnowledgeSearchRequest) ([]*entities.KnowledgeEntry, error) {
	var err error
	req, err = s.resolveSearchScope(ctx, namespace, req)
	if err != nil {
		return nil, err
	}
	topK := normalizedTopK(req.TopK)
	if len(req.Embedding) > 0 {
		return s.searchVector(ctx, namespace, req, topK)
	}
	terms := searchTerms(req.Query)
	if len(terms) == 0 {
		return []*entities.KnowledgeEntry{}, nil
	}
	guards, guardArgs, err := searchGuardsSQLite(ctx, req)
	if err != nil {
		return nil, err
	}
	ftsQuery := strings.Join(terms, " OR ")
	args := []any{namespace, ftsQuery}
	args = append(args, guardArgs...)
	args = append(args, topK)
	query := `SELECT ` + knowledgeProjectionSQLite + ` FROM knw_content_fts
		JOIN knw_content c ON c.rowid=knw_content_fts.rowid
		JOIN knw_document_revision dr ON dr.id=c.document_revision_id
		JOIN knw_document d ON d.id=dr.document_id AND d.current_revision_id=dr.id
		JOIN knw_source s ON s.id=d.source_id
		WHERE d.namespace_id=? AND dr.status='published' AND knw_content_fts MATCH ? AND ` + strings.Join(guards, " AND ") + `
		ORDER BY bm25(knw_content_fts),c.ordinal,c.id LIMIT ?`
	return s.queryKnowledge(ctx, query, args...)
}

func (s *KnowledgeSQLiteStore) resolveSearchScope(ctx context.Context, namespace string, req entities.KnowledgeSearchRequest) (entities.KnowledgeSearchRequest, error) {
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
		if err := s.conn.QueryRowContext(ctx, `SELECT id,name FROM orh_flow WHERE namespace_id=?
			AND (id=? OR name=?) AND status<>'DELETED'`, namespace, flowKey, flowKey).
			Scan(&req.FlowID, &req.FlowName); err != nil {
			return req, fmt.Errorf("resolve knowledge flow: %w", err)
		}
		return req, nil
	}
	if scope == "run" {
		if req.RunID == "" {
			return req, fmt.Errorf("run_id is required for run knowledge search")
		}
		if err := s.conn.QueryRowContext(ctx, `SELECT f.id,f.name FROM orh_flow f JOIN orh_run r
			ON r.flow_id=f.id AND r.namespace_id=f.namespace_id WHERE f.namespace_id=?
			AND (f.id=? OR f.name=?) AND r.id=?`, namespace, flowKey, flowKey, req.RunID).
			Scan(&req.FlowID, &req.FlowName); err != nil {
			return req, fmt.Errorf("resolve run-scoped knowledge owner: %w", err)
		}
		return req, nil
	}
	return req, fmt.Errorf("invalid knowledge scope %q", req.Scope)
}

type sqliteVectorHit struct {
	contentID string
	distance  float64
}

func (s *KnowledgeSQLiteStore) searchVector(ctx context.Context, namespace string, req entities.KnowledgeSearchRequest, topK int) ([]*entities.KnowledgeEntry, error) {
	if strings.TrimSpace(req.ProfileKey) == "" {
		return nil, fmt.Errorf("profile_key is required for vector search")
	}
	var dimensions int
	var metric, table string
	if err := s.conn.QueryRowContext(ctx, `SELECT dimensions,distance_metric,vector_table FROM knw_embedding_profile
		WHERE namespace_id=? AND profile_key=? AND status='ACTIVE'`, namespace, req.ProfileKey).Scan(&dimensions, &metric, &table); err != nil {
		return nil, fmt.Errorf("embedding profile: %w", err)
	}
	if len(req.Embedding) != dimensions {
		return nil, fmt.Errorf("embedding dimensions %d do not match profile dimensions %d", len(req.Embedding), dimensions)
	}
	if table != sqliteVectorTable(namespace, req.ProfileKey) {
		return nil, fmt.Errorf("embedding profile vector table mismatch")
	}
	vector, err := sqlitevec.SerializeFloat32(req.Embedding)
	if err != nil {
		return nil, err
	}
	candidateK := topK * 10
	if candidateK > 1000 {
		candidateK = 1000
	}
	rows, err := s.conn.QueryContext(ctx, `SELECT e.content_id,v.distance FROM `+table+` v
		JOIN knw_embedding e ON e.vector_table=? AND e.vector_rowid=v.rowid
		WHERE v.embedding MATCH ? AND k=? ORDER BY v.distance`, table, vector, candidateK)
	if err != nil {
		return nil, fmt.Errorf("sqlite vector search: %w", err)
	}
	hits := make([]sqliteVectorHit, 0, candidateK)
	for rows.Next() {
		var hit sqliteVectorHit
		if err := rows.Scan(&hit.contentID, &hit.distance); err != nil {
			rows.Close()
			return nil, err
		}
		hits = append(hits, hit)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	guards, guardArgs, err := searchGuardsSQLite(ctx, req)
	if err != nil {
		return nil, err
	}
	ftsQuery := strings.Join(searchTerms(req.Query), " OR ")
	if ftsQuery != "" {
		guards = append(guards, "c.rowid IN (SELECT rowid FROM knw_content_fts WHERE knw_content_fts MATCH ?)")
		guardArgs = append(guardArgs, ftsQuery)
	}
	items := make([]*entities.KnowledgeEntry, 0, topK)
	for _, hit := range hits {
		args := []any{namespace, hit.contentID}
		args = append(args, guardArgs...)
		query := `SELECT ` + knowledgeProjectionSQLite + ` FROM knw_content c
			JOIN knw_document_revision dr ON dr.id=c.document_revision_id
			JOIN knw_document d ON d.id=dr.document_id AND d.current_revision_id=dr.id
			JOIN knw_source s ON s.id=d.source_id
			WHERE d.namespace_id=? AND c.id=? AND dr.status='published' AND ` + strings.Join(guards, " AND ")
		item, scanErr := scanKnowledgeSQLite(s.conn.QueryRowContext(ctx, query, args...))
		if errors.Is(scanErr, sql.ErrNoRows) {
			continue
		}
		if scanErr != nil {
			return nil, scanErr
		}
		item.ProfileKey = req.ProfileKey
		item.Distance = &hit.distance
		if expandErr := s.expandContext(ctx, item); expandErr != nil {
			return nil, expandErr
		}
		items = append(items, item)
		if len(items) == topK {
			break
		}
	}
	return items, nil
}

func searchGuardsSQLite(ctx context.Context, req entities.KnowledgeSearchRequest) ([]string, []any, error) {
	clauses := []string{"d.status='ACTIVE'", "COALESCE(d.acl_ref,'')=''"}
	args := make([]any, 0)
	switch strings.ToLower(defaultString(req.Scope, "namespace")) {
	case "namespace":
		clauses = append(clauses, "d.scope='namespace'")
	case "flow":
		if req.FlowID == "" {
			return nil, nil, fmt.Errorf("flow_id is required for flow knowledge search")
		}
		clauses = append(clauses, "(d.scope='namespace' OR (d.scope='flow' AND d.flow_id=?))")
		args = append(args, req.FlowID)
	case "run":
		if req.FlowID == "" || req.RunID == "" {
			return nil, nil, fmt.Errorf("flow_id and run_id are required for run knowledge search")
		}
		clauses = append(clauses, "(d.scope='namespace' OR (d.scope='flow' AND d.flow_id=?) OR (d.scope='run' AND d.flow_id=? AND d.run_id=?))")
		args = append(args, req.FlowID, req.FlowID, req.RunID)
	default:
		return nil, nil, fmt.Errorf("invalid knowledge scope %q", req.Scope)
	}
	if len(req.Tags) > 0 {
		placeholders := make([]string, len(req.Tags))
		for index, tag := range req.Tags {
			placeholders[index] = "?"
			args = append(args, tag)
		}
		clauses = append(clauses, "EXISTS (SELECT 1 FROM json_each(COALESCE(d.metadata,'{}'),'$.tags') WHERE value IN ("+strings.Join(placeholders, ",")+"))")
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "knw_document").SQLiteWhere()
	clauses = append(clauses, "d.id IN (SELECT id FROM knw_document WHERE ("+scopeWhere+"))")
	args = append(args, scopeArgs...)
	return clauses, args, nil
}

func (s *KnowledgeSQLiteStore) queryKnowledge(ctx context.Context, query string, args ...any) ([]*entities.KnowledgeEntry, error) {
	rows, err := s.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("knowledge search: %w", err)
	}
	items := make([]*entities.KnowledgeEntry, 0)
	for rows.Next() {
		item, scanErr := scanKnowledgeSQLite(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	// SQLite is intentionally configured with one connection. Release the
	// result set before issuing context-expansion queries on that connection.
	if err = rows.Close(); err != nil {
		return nil, err
	}
	for _, item := range items {
		if err = s.expandContext(ctx, item); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *KnowledgeSQLiteStore) expandContext(ctx context.Context, item *entities.KnowledgeEntry) error {
	rows, err := s.conn.QueryContext(ctx, `SELECT id,type,ordinal,content FROM knw_content
		WHERE document_revision_id=? AND (id=? OR id=? OR ordinal IN (
			SELECT ordinal-1 FROM knw_content WHERE id=? UNION SELECT ordinal+1 FROM knw_content WHERE id=?
		)) ORDER BY ordinal,id`, item.DocumentRevisionID, item.ContentID, nilIfEmpty(item.ParentContentID), item.ContentID, item.ContentID)
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

func (s *KnowledgeSQLiteStore) ListTags(ctx context.Context, namespace, scope string) ([]string, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "knw_document").SQLiteWhere()
	query := `SELECT DISTINCT tag.value FROM knw_document d
		JOIN json_each(COALESCE(d.metadata,'{}'),'$.tags') tag
		WHERE d.namespace_id=? AND d.status<>'DELETED' AND (?='' OR d.scope=?)
		AND d.id IN (SELECT id FROM knw_document WHERE (` + scopeWhere + `)) ORDER BY tag.value`
	args := []any{namespace, strings.ToLower(scope), strings.ToLower(scope)}
	args = append(args, scopeArgs...)
	rows, err := s.conn.QueryContext(ctx, query, args...)
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

func (s *KnowledgeSQLiteStore) CreateEmbeddingProfile(ctx context.Context, profile *entities.EmbeddingProfile) error {
	if err := validateEmbeddingProfile(profile); err != nil {
		return err
	}
	if profile.Dimensions > sqliteVecMaxDimensions {
		return fmt.Errorf("sqlite-vec supports at most %d dimensions", sqliteVecMaxDimensions)
	}
	if profile.ID == "" {
		profile.ID = uuid.NewString()
	}
	table := sqliteVectorTable(profile.Namespace, profile.ProfileKey)
	principal := defaultString(profile.CreatedBy, "system:flowgent")
	metadata, _ := json.Marshal(profile.Metadata)
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO orh_namespace
		(id,name,description,created_by,updated_by) VALUES(?,?,?, ?,?)`, profile.Namespace, profile.Namespace,
		"Flowgent namespace", principal, principal); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO knw_embedding_profile
		(id,namespace_id,profile_key,provider_type,model,model_revision,dimensions,distance_metric,
		 vector_table,status,created_by,updated_by,metadata)
		VALUES(?,?,?,?,?,?,?,?,?,'ACTIVE',?,?,?)`, profile.ID, profile.Namespace, profile.ProfileKey,
		profile.ProviderType, profile.Model, profile.ModelRevision, profile.Dimensions, profile.DistanceMetric,
		table, principal, principal, string(metadata)); err != nil {
		return err
	}
	create := fmt.Sprintf(`CREATE VIRTUAL TABLE %s USING vec0(embedding float[%d] distance_metric=%s)`,
		table, profile.Dimensions, profile.DistanceMetric)
	if _, err = tx.ExecContext(ctx, create); err != nil {
		return fmt.Errorf("create sqlite vector index: %w", err)
	}
	return tx.Commit()
}

func (s *KnowledgeSQLiteStore) PutEmbedding(ctx context.Context, embedding *entities.KnowledgeEmbedding) error {
	if embedding.ID == "" {
		embedding.ID = uuid.NewString()
	}
	var dimensions int
	var metric, table string
	if err := s.conn.QueryRowContext(ctx, `SELECT dimensions,distance_metric,vector_table FROM knw_embedding_profile
		WHERE namespace_id=? AND profile_key=? AND status='ACTIVE'`, embedding.Namespace, embedding.ProfileKey).Scan(&dimensions, &metric, &table); err != nil {
		return err
	}
	if table != sqliteVectorTable(embedding.Namespace, embedding.ProfileKey) {
		return fmt.Errorf("embedding profile vector table mismatch")
	}
	if len(embedding.Embedding) != dimensions {
		return fmt.Errorf("embedding dimensions %d do not match profile dimensions %d", len(embedding.Embedding), dimensions)
	}
	vector, err := sqlitevec.SerializeFloat32(embedding.Embedding)
	if err != nil {
		return err
	}
	if embedding.InputHash == "" {
		embedding.InputHash = hashText(vectorLiteral(embedding.Embedding))
	}
	embedding.Dimensions, embedding.DistanceMetric = dimensions, metric
	principal := defaultString(embedding.CreatedBy, "system:flowgent")
	metadata, _ := json.Marshal(embedding.Metadata)
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var existingID string
	var rowID int64
	err = tx.QueryRowContext(ctx, `SELECT id,vector_rowid FROM knw_embedding
		WHERE content_id=? AND profile_key=?`, embedding.ContentID, embedding.ProfileKey).Scan(&existingID, &rowID)
	if errors.Is(err, sql.ErrNoRows) {
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(vector_rowid),0)+1 FROM knw_embedding WHERE vector_table=?`, table).Scan(&rowID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO `+table+`(rowid,embedding) VALUES(?,?)`, rowID, vector); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO knw_embedding
			(id,namespace_id,content_id,profile_key,dimensions,distance_metric,input_hash,vector_table,
			 vector_rowid,created_by,updated_by,metadata)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, embedding.ID, embedding.Namespace, embedding.ContentID,
			embedding.ProfileKey, dimensions, metric, embedding.InputHash, table, rowID, principal, principal,
			string(metadata))
	} else if err == nil {
		embedding.ID = existingID
		var inputHash string
		if err = tx.QueryRowContext(ctx, `SELECT input_hash FROM knw_embedding WHERE id=?`, existingID).Scan(&inputHash); err != nil {
			return err
		}
		if inputHash != embedding.InputHash {
			return fmt.Errorf("embedding is immutable for content/profile; create a new content revision")
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func knowledgeCandidateVisibleSQLite(ctx context.Context, db *sql.DB, namespace, id string) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "knw_document").SQLiteWhere()
	args := append([]any{namespace, id}, scopeArgs...)
	var visible int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT CAST(? AS TEXT) namespace_id,CAST(? AS TEXT) id) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}
