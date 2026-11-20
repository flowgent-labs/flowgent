package flow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FlowPostgresStore struct {
	pool *pgxpool.Pool
}

func NewFlowPostgresStore(pool *pgxpool.Pool) *FlowPostgresStore {
	return &FlowPostgresStore{pool: pool}
}

func flowRevisionColumns() string { return utils.Columns[entities.FlowVersionInfo]() }

func scanFlowVersion(row interface{ Scan(...any) error }) (*entities.FlowVersionInfo, error) {
	var item entities.FlowVersionInfo
	if err := utils.ScanStruct(row, &item); err != nil {
		return nil, err
	}
	item.Version = item.Revision
	return &item, nil
}

// Get accepts either the namespace-local flow name used by HTTP routes or the
// stable flow identity. The returned FlowID is always the stable identity.
func (s *FlowPostgresStore) Get(ctx context.Context, namespace, key string) (*entities.FlowVersionInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow").PostgresWhere(3)
	args := append([]any{namespace, key}, scopeArgs...)
	query := `SELECT ` + flowRevisionColumns() + ` FROM (
		SELECT r.*,f.name AS flow_name
		FROM (SELECT * FROM orh_flow WHERE namespace_id=$1 AND (id=$2 OR name=$2)
			AND status<>'DELETED' AND (` + scopeWhere + `)) f
		JOIN orh_flow_revision r ON r.id=f.current_revision_id AND r.flow_id=f.id
	) current_flow`
	return scanFlowVersion(s.pool.QueryRow(ctx, query, args...))
}

func (s *FlowPostgresStore) Select(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.FlowVersionInfo], error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 {
		req.Size = 20
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow").PostgresWhere(2)
	baseArgs := append([]any{namespace}, scopeArgs...)
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM orh_flow WHERE namespace_id=$1 AND status<>'DELETED' AND (`+scopeWhere+`)`, baseArgs...).Scan(&total); err != nil {
		return nil, err
	}
	limitPos := len(baseArgs) + 1
	args := append(baseArgs, req.Size, (req.Page-1)*req.Size)
	query := fmt.Sprintf(`SELECT %s FROM (
		SELECT r.*,f.name AS flow_name
		FROM (SELECT * FROM orh_flow WHERE namespace_id=$1 AND status<>'DELETED' AND (%s)) f
		JOIN orh_flow_revision r ON r.id=f.current_revision_id AND r.flow_id=f.id
	) current_flow ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, flowRevisionColumns(), scopeWhere, limitPos, limitPos+1)
	rows, err := s.pool.Query(ctx, query, args...)
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

func (s *FlowPostgresStore) Delete(ctx context.Context, namespace, key string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow").PostgresWhere(3)
	args := append([]any{namespace, key}, scopeArgs...)
	result, err := s.pool.Exec(ctx, `UPDATE orh_flow SET status='DELETED',updated_by=NULL
		WHERE namespace_id=$1 AND (id=$2 OR name=$2) AND status<>'DELETED' AND (`+scopeWhere+`)`, args...)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("flow not found or outside authorization scope")
	}
	return nil
}

func (s *FlowPostgresStore) GetVersion(ctx context.Context, namespace, key string, revision int64) (*entities.FlowVersionInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow").PostgresWhere(4)
	args := append([]any{namespace, key, revision}, scopeArgs...)
	query := `SELECT ` + flowRevisionColumns() + ` FROM (
		SELECT r.*,f.name AS flow_name
		FROM (SELECT * FROM orh_flow WHERE namespace_id=$1 AND (id=$2 OR name=$2)
			AND status<>'DELETED' AND (` + scopeWhere + `)) f
		JOIN orh_flow_revision r ON r.flow_id=f.id AND r.namespace_id=f.namespace_id
		WHERE r.revision=$3
	) requested_flow`
	return scanFlowVersion(s.pool.QueryRow(ctx, query, args...))
}

func (s *FlowPostgresStore) SaveSpec(ctx context.Context, spec *entities.FlowInfo, createdBy, comment string) error {
	if spec.Namespace == "" {
		spec.Namespace = defaultNamespace
	}
	spec.NormalizeIdentity()
	name := spec.ResourceName()
	// Validate before beginning a transaction; marshalSpec also removes runtime
	// revision aliases from the immutable definition.
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

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureNamespacePG(ctx, tx, spec.Namespace, createdBy); err != nil {
		return err
	}

	var flowID string
	var rowVersion int64
	err = tx.QueryRow(ctx, `SELECT id,row_version FROM orh_flow
		WHERE namespace_id=$1 AND (id=$2 OR lower(name)=lower($3)) FOR UPDATE`,
		spec.Namespace, spec.ID, name).Scan(&flowID, &rowVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		flowID = uuid.NewString()
		if _, err = tx.Exec(ctx, `INSERT INTO orh_flow
			(id,namespace_id,name,description,status,created_by,updated_by)
			VALUES ($1,$2,$3,$4,'ACTIVE',$5,$5)`, flowID, spec.Namespace, name, spec.Description, createdBy); err != nil {
			return err
		}
		rowVersion = 1
	} else if err != nil {
		return err
	}

	var nextRevision int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM orh_flow_revision WHERE flow_id=$1`, flowID).Scan(&nextRevision); err != nil {
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
	if _, err = tx.Exec(ctx, `INSERT INTO orh_flow_revision
		(id,namespace_id,flow_id,revision,definition,summarize_enabled,checksum,comment,description,status,created_by,updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'PUBLISHED',$10,$10)`,
		revisionID, spec.Namespace, flowID, nextRevision, b, spec.SummarizeEnabled, checksum, comment, spec.Description, createdBy); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE orh_flow SET current_revision_id=$1,name=$2,description=$3,status='ACTIVE',updated_by=$4
		WHERE id=$5 AND namespace_id=$6 AND row_version=$7`, revisionID, name, spec.Description, createdBy, flowID, spec.Namespace, rowVersion)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("flow revision CAS conflict")
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	spec.Revision, spec.Version = nextRevision, nextRevision
	return nil
}

func (s *FlowPostgresStore) CreateSpec(ctx context.Context, spec *entities.FlowInfo, createdBy, comment string) error {
	if spec.Namespace == "" {
		spec.Namespace = defaultNamespace
	}
	spec.NormalizeIdentity()
	if _, err := s.Get(ctx, spec.Namespace, spec.ResourceName()); err == nil {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, spec.ResourceName())
	}
	err := s.SaveSpec(ctx, spec, createdBy, comment)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, spec.ResourceName())
	}
	return err
}

func ensureNamespacePG(ctx context.Context, tx pgx.Tx, namespace, principal string) error {
	if namespace == "" {
		namespace = defaultNamespace
	}
	_, err := tx.Exec(ctx, `INSERT INTO orh_namespace (id,name,description,created_by,updated_by)
		VALUES ($1::varchar(64),$1::text,'Flowgent namespace',$2::varchar(255),$2::varchar(255))
		ON CONFLICT (id) DO NOTHING`, namespace, principal)
	return err
}

func (s *FlowPostgresStore) isCandidateVisible(ctx context.Context, namespace, name string) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow").PostgresWhere(3)
	args := append([]any{namespace, name}, scopeArgs...)
	var visible int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM (
		SELECT CAST($1 AS TEXT) AS namespace_id,CAST($2 AS TEXT) AS name
	) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}

func (s *FlowPostgresStore) GetSpec(ctx context.Context, namespace, key string) (*entities.FlowInfo, error) {
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
