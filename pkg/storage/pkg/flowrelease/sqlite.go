package flowrelease

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
)

type sqliteRepository struct {
	db *sql.DB
}

func newSQLiteRepository(db *sql.DB) *sqliteRepository {
	return &sqliteRepository{db: db}
}

func (s *sqliteRepository) GetRelease(ctx context.Context, id string) (*entities.FlowRelease, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release").SQLiteWhere()
	args := append([]any{id}, scopeArgs...)
	query := fmt.Sprintf(`SELECT %s FROM orh_flow_release WHERE id=? AND del_flag=0 AND (%s)`, utils.Columns[entities.FlowRelease](), scopeWhere)
	row := s.db.QueryRowContext(ctx, query, args...)
	var release entities.FlowRelease
	if err := utils.ScanStruct(row, &release); err != nil {
		return nil, err
	}
	return &release, nil
}

func (s *sqliteRepository) ListAccessibleReleases(ctx context.Context, namespace string) ([]*entities.FlowRelease, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release").SQLiteWhere()
	query := fmt.Sprintf(`SELECT %s FROM orh_flow_release r
		WHERE r.del_flag=0 AND r.status='ACTIVE' AND (
			r.namespace_id=? OR r.visibility='SHARED' OR EXISTS (
				SELECT 1 FROM orh_flow_release_grant g
				WHERE g.release_id=r.id AND g.consumer_namespace=? AND g.del_flag=0 AND g.status='ACTIVE'
				  AND (g.expires_at IS NULL OR g.expires_at > datetime('now'))
			)
		) AND (%s) ORDER BY r.published_at DESC`, utils.Columns[entities.FlowRelease](), scopeWhere)
	args := append([]any{namespace, namespace}, scopeArgs...)
	return scanSQLite[entities.FlowRelease](ctx, s.db, query, args...)
}

func (s *sqliteRepository) SaveRelease(ctx context.Context, item *entities.FlowRelease) error {
	definition, err := json.Marshal(item.Definition)
	if err != nil {
		return err
	}
	visible, err := s.releaseCandidateVisible(ctx, item)
	if err != nil {
		return err
	}
	if !visible {
		return storage.ErrFlowgentSqlScopeDenied
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO orh_flow_release
		(id,flow_id,flow_version,release_version,definition,checksum,visibility,published_at,description,
		 namespace_id,status,created_at,created_by,updated_at,updated_by,del_flag)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0)`, item.ID, item.FlowID, item.FlowVersion, item.ReleaseVersion,
		definition, item.Checksum, item.Visibility, formatSQLiteTime(item.PublishedAt), item.Description,
		item.Namespace, item.Status, formatSQLiteTime(item.CreatedAt), item.CreatedBy,
		formatSQLiteTime(item.UpdatedAt), item.UpdatedBy)
	return err
}

func (s *sqliteRepository) RevokeRelease(ctx context.Context, producerNamespace, releaseID, actor string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release").SQLiteWhere()
	args := append([]any{actor, releaseID, producerNamespace}, scopeArgs...)
	result, err := s.db.ExecContext(ctx, `UPDATE orh_flow_release
		SET status='REVOKED',updated_at=datetime('now'),updated_by=?
		WHERE id=? AND namespace_id=? AND del_flag=0 AND status='ACTIVE' AND (`+scopeWhere+`)`, args...)
	return requireAffected(result, err, "release")
}

func (s *sqliteRepository) CanAccessRelease(ctx context.Context, releaseID, namespace string) (bool, error) {
	var allowed int
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release").SQLiteWhere()
	args := append([]any{releaseID, namespace, namespace}, scopeArgs...)
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM orh_flow_release r WHERE r.id=? AND r.del_flag=0 AND r.status='ACTIVE' AND (
			r.namespace_id=? OR r.visibility='SHARED' OR EXISTS (
				SELECT 1 FROM orh_flow_release_grant g WHERE g.release_id=r.id
				AND g.consumer_namespace=? AND g.del_flag=0 AND g.status='ACTIVE'
				AND (g.expires_at IS NULL OR g.expires_at > datetime('now')))) AND (`+scopeWhere+`))`,
		args...).Scan(&allowed)
	return allowed == 1, err
}

func (s *sqliteRepository) ListGrants(ctx context.Context, producerNamespace, releaseID string) ([]*entities.FlowReleaseGrant, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release_grant").SQLiteWhere()
	query := fmt.Sprintf(`SELECT %s FROM orh_flow_release_grant
		WHERE namespace_id=? AND release_id=? AND del_flag=0 AND (%s) ORDER BY created_at DESC`, utils.Columns[entities.FlowReleaseGrant](), scopeWhere)
	args := append([]any{producerNamespace, releaseID}, scopeArgs...)
	return scanSQLite[entities.FlowReleaseGrant](ctx, s.db, query, args...)
}

func (s *sqliteRepository) SaveGrant(ctx context.Context, item *entities.FlowReleaseGrant) error {
	visible, err := s.grantCandidateVisible(ctx, item)
	if err != nil {
		return err
	}
	if !visible {
		return storage.ErrFlowgentSqlScopeDenied
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO orh_flow_release_grant
		(id,release_id,consumer_namespace,expires_at,description,namespace_id,status,created_at,created_by,updated_at,updated_by,del_flag)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,0)`, item.ID, item.ReleaseID, item.ConsumerNamespace, item.ExpiresAt,
		item.Description, item.Namespace, item.Status, formatSQLiteTime(item.CreatedAt), item.CreatedBy,
		formatSQLiteTime(item.UpdatedAt), item.UpdatedBy)
	return err
}

func (s *sqliteRepository) DeleteGrant(ctx context.Context, producerNamespace, releaseID, grantID, actor string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release_grant").SQLiteWhere()
	args := append([]any{actor, grantID, releaseID, producerNamespace}, scopeArgs...)
	result, err := s.db.ExecContext(ctx, `UPDATE orh_flow_release_grant
		SET status='REVOKED',del_flag=1,updated_at=datetime('now'),updated_by=?
		WHERE id=? AND release_id=? AND namespace_id=? AND del_flag=0 AND (`+scopeWhere+`)`, args...)
	return requireAffected(result, err, "grant")
}

func (s *sqliteRepository) ListInstallations(ctx context.Context, namespace string) ([]*entities.FlowInstallation, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_installation").SQLiteWhere()
	query := fmt.Sprintf(`SELECT %s FROM orh_flow_installation
		WHERE namespace_id=? AND del_flag=0 AND (%s) ORDER BY installed_at DESC`, utils.Columns[entities.FlowInstallation](), scopeWhere)
	args := append([]any{namespace}, scopeArgs...)
	return scanSQLite[entities.FlowInstallation](ctx, s.db, query, args...)
}

func (s *sqliteRepository) Install(ctx context.Context, definition *entities.FlowInfo, installation *entities.FlowInstallation, actor string) error {
	definitionJSON, err := json.Marshal(definition)
	if err != nil {
		return fmt.Errorf("marshal installed flow: %w", err)
	}
	bindingsJSON, err := json.Marshal(installation.ResourceBindings)
	if err != nil {
		return fmt.Errorf("marshal resource bindings: %w", err)
	}
	visible, err := s.installationCandidateVisible(ctx, installation)
	if err != nil {
		return err
	}
	if !visible {
		return storage.ErrFlowgentSqlScopeDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO orh_agentflow
		(id,agentflow_id,version,definition,created_by,comment,namespace_id,status)
		VALUES (?,?,?,?,?,?,?, 'ACTIVE')`, uuid.NewString(), definition.ID, 1, definitionJSON,
		actor, "Installed release "+installation.ReleaseVersion, definition.Namespace); err != nil {
		return fmt.Errorf("create installed flow: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO orh_flow_installation
		(id,release_id,release_version,producer_namespace,installed_flow_id,release_checksum,applied_checksum,
		 resource_bindings,installed_at,description,namespace_id,status,created_at,created_by,updated_at,updated_by,del_flag)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0)`, installation.ID, installation.ReleaseID,
		installation.ReleaseVersion, installation.ProducerNamespace, installation.InstalledFlowID,
		installation.ReleaseChecksum, installation.AppliedChecksum, bindingsJSON, formatSQLiteTime(installation.InstalledAt),
		installation.Description, installation.Namespace, installation.Status, formatSQLiteTime(installation.CreatedAt),
		installation.CreatedBy, formatSQLiteTime(installation.UpdatedAt), installation.UpdatedBy); err != nil {
		return fmt.Errorf("record flow installation: %w", err)
	}
	return tx.Commit()
}

func (s *sqliteRepository) releaseCandidateVisible(ctx context.Context, item *entities.FlowRelease) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release").SQLiteWhere()
	args := append([]any{item.ID, item.Namespace}, scopeArgs...)
	var visible int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM (
		SELECT CAST(? AS TEXT) AS id, CAST(? AS TEXT) AS namespace_id
	) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}

func (s *sqliteRepository) grantCandidateVisible(ctx context.Context, item *entities.FlowReleaseGrant) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release_grant").SQLiteWhere()
	args := append([]any{item.ID, item.ReleaseID, item.Namespace}, scopeArgs...)
	var visible int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM (
		SELECT CAST(? AS TEXT) AS id, CAST(? AS TEXT) AS release_id, CAST(? AS TEXT) AS namespace_id
	) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}

func (s *sqliteRepository) installationCandidateVisible(ctx context.Context, item *entities.FlowInstallation) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_installation").SQLiteWhere()
	args := append([]any{item.ID, item.ReleaseID, item.Namespace}, scopeArgs...)
	var visible int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM (
		SELECT CAST(? AS TEXT) AS id, CAST(? AS TEXT) AS release_id, CAST(? AS TEXT) AS namespace_id
	) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}

func scanSQLite[T any](ctx context.Context, db *sql.DB, query string, args ...any) ([]*T, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*T, 0)
	for rows.Next() {
		item := new(T)
		if err := utils.ScanStruct(rows, item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func requireAffected(result sql.Result, err error, resource string) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("%s not found", resource)
	}
	return nil
}

func formatSQLiteTime(value time.Time) string { return value.UTC().Format("2006-01-02 15:04:05") }
