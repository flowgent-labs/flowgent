package flow

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
)

type FlowSQLiteStore struct{ conn *sql.DB }

func NewFlowSQLiteStore(conn *sql.DB) *FlowSQLiteStore { return &FlowSQLiteStore{conn: conn} }

// Get accepts a namespace-local name or stable flow ID and always returns the
// immutable revision with both identities populated.
func (s *FlowSQLiteStore) Get(ctx context.Context, namespace, key string) (*entities.FlowVersionInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow").SQLiteWhere()
	args := append([]any{namespace, key}, scopeArgs...)
	query := `SELECT ` + flowRevisionColumns() + ` FROM (
		SELECT r.*,f.name AS flow_name
		FROM (SELECT * FROM orh_flow WHERE namespace_id=?1 AND (id=?2 OR name=?2)
			AND status<>'DELETED' AND (` + scopeWhere + `)) f
		JOIN orh_flow_revision r ON r.id=f.current_revision_id AND r.flow_id=f.id
	) current_flow`
	return scanFlowVersion(s.conn.QueryRowContext(ctx, query, args...))
}

func (s *FlowSQLiteStore) Select(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.FlowVersionInfo], error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 {
		req.Size = 20
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow").SQLiteWhere()
	baseArgs := append([]any{namespace}, scopeArgs...)
	var total int64
	if err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM orh_flow WHERE namespace_id=?1 AND status<>'DELETED' AND (`+scopeWhere+`)`, baseArgs...).Scan(&total); err != nil {
		return nil, err
	}
	args := append(baseArgs, req.Size, (req.Page-1)*req.Size)
	query := `SELECT ` + flowRevisionColumns() + ` FROM (
		SELECT r.*,f.name AS flow_name
		FROM (SELECT * FROM orh_flow WHERE namespace_id=?1 AND status<>'DELETED' AND (` + scopeWhere + `)) f
		JOIN orh_flow_revision r ON r.id=f.current_revision_id AND r.flow_id=f.id
	) current_flow ORDER BY updated_at DESC LIMIT ? OFFSET ?`
	rows, err := s.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.FlowVersionInfo, 0)
	for rows.Next() {
		item, err := scanFlowVersion(rows)
		if err != nil {
			return nil, fmt.Errorf("scan flow revision: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entities.NewPage(items, total, req), nil
}

func (s *FlowSQLiteStore) Delete(ctx context.Context, namespace, key string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow").SQLiteWhere()
	args := append([]any{namespace, key}, scopeArgs...)
	result, err := s.conn.ExecContext(ctx, `UPDATE orh_flow SET status='DELETED',updated_by=NULL
		WHERE namespace_id=?1 AND (id=?2 OR name=?2) AND status<>'DELETED' AND (`+scopeWhere+`)`, args...)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("flow not found or outside authorization scope")
	}
	return nil
}

func (s *FlowSQLiteStore) GetVersion(ctx context.Context, namespace, key string, revision int64) (*entities.FlowVersionInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow").SQLiteWhere()
	args := append([]any{namespace, key, revision}, scopeArgs...)
	query := `SELECT ` + flowRevisionColumns() + ` FROM (
		SELECT r.*,f.name AS flow_name
		FROM (SELECT * FROM orh_flow WHERE namespace_id=?1 AND (id=?2 OR name=?2)
			AND status<>'DELETED' AND (` + scopeWhere + `)) f
		JOIN orh_flow_revision r ON r.flow_id=f.id AND r.namespace_id=f.namespace_id
		WHERE r.revision=?3
	) requested_flow`
	return scanFlowVersion(s.conn.QueryRowContext(ctx, query, args...))
}

func (s *FlowSQLiteStore) SaveSpec(ctx context.Context, spec *entities.FlowInfo, createdBy, comment string) error {
	if spec.Namespace == "" {
		spec.Namespace = defaultNamespace
	}
	spec.NormalizeIdentity()
	name := spec.ResourceName()
	if _, err := marshalSpec(spec); err != nil {
		return err
	}
	visible, err := s.isCandidateVisible(ctx, spec.Namespace, name)
	if err != nil {
		return err
	}
	if !visible {
		return storage.ErrFlowgentSqlScopeDenied
	}
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO orh_namespace
		(id,name,description,created_by,updated_by) VALUES (?,?,?,?,?)`,
		spec.Namespace, spec.Namespace, "Flowgent namespace", createdBy, createdBy); err != nil {
		return err
	}
	var flowID string
	var rowVersion int64
	err = tx.QueryRowContext(ctx, `SELECT id,row_version FROM orh_flow
		WHERE namespace_id=? AND (id=? OR lower(name)=lower(?))`, spec.Namespace, spec.ID, name).Scan(&flowID, &rowVersion)
	if errors.Is(err, sql.ErrNoRows) {
		flowID = uuid.NewString()
		if _, err = tx.ExecContext(ctx, `INSERT INTO orh_flow
			(id,namespace_id,name,description,status,created_by,updated_by)
			VALUES (?,?,?,?,'ACTIVE',?,?)`, flowID, spec.Namespace, name, spec.Description, createdBy, createdBy); err != nil {
			return err
		}
		rowVersion = 1
	} else if err != nil {
		return err
	}
	var nextRevision int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM orh_flow_revision WHERE flow_id=?`, flowID).Scan(&nextRevision); err != nil {
		return err
	}
	spec.ID = flowID
	spec.Name = name
	b, err := marshalSpec(spec)
	if err != nil {
		return err
	}
	revisionID := uuid.NewString()
	sum := sha256.Sum256(b)
	checksum := hex.EncodeToString(sum[:])
	if _, err = tx.ExecContext(ctx, `INSERT INTO orh_flow_revision
		(id,namespace_id,flow_id,revision,definition,summarize_enabled,checksum,comment,description,status,created_by,updated_by)
		VALUES (?,?,?,?,?,?,?,?,?,'PUBLISHED',?,?)`, revisionID, spec.Namespace, flowID, nextRevision,
		string(b), spec.SummarizeEnabled, checksum, comment, spec.Description, createdBy, createdBy); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE orh_flow SET current_revision_id=?,name=?,description=?,status='ACTIVE',updated_by=?
		WHERE id=? AND namespace_id=? AND row_version=?`, revisionID, name, spec.Description, createdBy, flowID, spec.Namespace, rowVersion)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("flow revision CAS conflict")
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	spec.Revision, spec.Version = nextRevision, nextRevision
	return nil
}

func (s *FlowSQLiteStore) CreateSpec(ctx context.Context, spec *entities.FlowInfo, createdBy, comment string) error {
	if spec.Namespace == "" {
		spec.Namespace = defaultNamespace
	}
	spec.NormalizeIdentity()
	var count int
	if err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM orh_flow WHERE namespace_id=? AND lower(name)=lower(?)`, spec.Namespace, spec.ResourceName()).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, spec.ResourceName())
	}
	err := s.SaveSpec(ctx, spec, createdBy, comment)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, spec.ResourceName())
	}
	return err
}

func (s *FlowSQLiteStore) isCandidateVisible(ctx context.Context, namespace, name string) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow").SQLiteWhere()
	args := append([]any{namespace, name}, scopeArgs...)
	var visible int
	err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
		SELECT CAST(? AS TEXT) AS namespace_id,CAST(? AS TEXT) AS name
	) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}

func (s *FlowSQLiteStore) GetSpec(ctx context.Context, namespace, key string) (*entities.FlowInfo, error) {
	version, err := s.Get(ctx, namespace, key)
	if err != nil {
		return nil, err
	}
	var spec entities.FlowInfo
	if err := json.Unmarshal(version.Definition, &spec); err != nil {
		return nil, err
	}
	hydrateSpec(&spec, version)
	return &spec, nil
}
