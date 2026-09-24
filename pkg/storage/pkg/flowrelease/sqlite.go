package flowrelease

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
)

type sqliteRepository struct{ db *sql.DB }

func newSQLiteRepository(db *sql.DB) *sqliteRepository { return &sqliteRepository{db: db} }

const releaseColumnsSQLite = `id,flow_id,flow_revision_id,flow_revision,release_version,definition,
	checksum,visibility,published_at,COALESCE(description,''),namespace_id,status,created_at,
	COALESCE(created_by,''),updated_at,COALESCE(updated_by,''),row_version,COALESCE(metadata,'{}')`

func scanReleaseSQLite(row interface{ Scan(...any) error }) (*entities.FlowRelease, error) {
	var item entities.FlowRelease
	var definition, metadata, publishedAt, createdAt, updatedAt string
	if err := row.Scan(&item.ID, &item.FlowID, &item.FlowRevisionID, &item.FlowRevision,
		&item.ReleaseVersion, &definition, &item.Checksum, &item.Visibility, &publishedAt,
		&item.Description, &item.Namespace, &item.Status, &createdAt, &item.CreatedBy,
		&updatedAt, &item.UpdatedBy, &item.RowVersion, &metadata); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(definition), &item.Definition); err != nil {
		return nil, fmt.Errorf("decode release definition: %w", err)
	}
	item.FlowName = item.Definition.ResourceName()
	_ = json.Unmarshal([]byte(metadata), &item.Metadata)
	item.PublishedAt, _ = utils.ParseTime(publishedAt)
	item.CreatedAt, _ = utils.ParseTime(createdAt)
	item.UpdatedAt, _ = utils.ParseTime(updatedAt)
	item.NormalizeAliases()
	return &item, nil
}

func (s *sqliteRepository) GetRelease(ctx context.Context, id string) (*entities.FlowRelease, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release").SQLiteWhere()
	args := append([]any{id}, scopeArgs...)
	return scanReleaseSQLite(s.db.QueryRowContext(ctx, `SELECT `+releaseColumnsSQLite+` FROM orh_flow_release
		WHERE id=? AND status<>'DELETED' AND (`+scopeWhere+`)`, args...))
}

func (s *sqliteRepository) ListAccessibleReleases(ctx context.Context, namespace string) ([]*entities.FlowRelease, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release").SQLiteWhere()
	rows, err := s.db.QueryContext(ctx, `SELECT `+releaseColumnsSQLite+` FROM orh_flow_release r
		WHERE r.status='ACTIVE' AND (
			r.namespace_id=? OR r.visibility='SHARED' OR EXISTS (
				SELECT 1 FROM orh_flow_release_grant g
				WHERE g.release_id=r.id AND g.consumer_namespace_id=? AND g.status='ACTIVE'
				  AND (g.expires_at IS NULL OR g.expires_at > CURRENT_TIMESTAMP)
			)
		) AND (`+scopeWhere+`) ORDER BY r.published_at DESC`, append([]any{namespace, namespace}, scopeArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.FlowRelease, 0)
	for rows.Next() {
		item, scanErr := scanReleaseSQLite(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqliteRepository) SaveRelease(ctx context.Context, item *entities.FlowRelease) error {
	item.NormalizeAliases()
	visible, err := s.releaseCandidateVisible(ctx, item)
	if err != nil {
		return err
	}
	if !visible {
		return storage.ErrFlowgentSqlScopeDenied
	}
	flowKey := item.FlowName
	if flowKey == "" {
		flowKey = item.FlowID
	}
	if item.FlowRevision > 0 {
		err = s.db.QueryRowContext(ctx, `SELECT f.id,f.name,r.id FROM orh_flow f
			JOIN orh_flow_revision r ON r.flow_id=f.id AND r.namespace_id=f.namespace_id
			WHERE f.namespace_id=? AND (f.id=? OR f.name=?) AND r.revision=? AND f.status<>'DELETED'`,
			item.Namespace, flowKey, flowKey, item.FlowRevision).Scan(&item.FlowID, &item.FlowName, &item.FlowRevisionID)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT f.id,f.name,r.id,r.revision FROM orh_flow f JOIN orh_flow_revision r ON r.id=f.current_revision_id
			WHERE f.namespace_id=? AND (f.id=? OR f.name=?) AND f.status<>'DELETED'`, item.Namespace, flowKey, flowKey).
			Scan(&item.FlowID, &item.FlowName, &item.FlowRevisionID, &item.FlowRevision)
	}
	if err != nil {
		return fmt.Errorf("resolve flow revision: %w", err)
	}
	item.Definition.ID = item.FlowID
	item.Definition.Name = item.FlowName
	definition, err := json.Marshal(item.Definition)
	if err != nil {
		return err
	}
	metadata, _ := json.Marshal(item.Metadata)
	_, err = s.db.ExecContext(ctx, `INSERT INTO orh_flow_release
		(id,namespace_id,flow_id,flow_revision_id,flow_revision,release_version,definition,checksum,
		 visibility,published_at,description,status,created_at,created_by,updated_at,updated_by,row_version,metadata)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,NULLIF(?,''),?,NULLIF(?,''),?,?)`, item.ID, item.Namespace,
		item.FlowID, item.FlowRevisionID, item.FlowRevision, item.ReleaseVersion, string(definition), item.Checksum,
		item.Visibility, formatSQLiteTime(item.PublishedAt), item.Description, item.Status,
		formatSQLiteTime(item.CreatedAt), item.CreatedBy, formatSQLiteTime(item.UpdatedAt), item.UpdatedBy,
		item.RowVersion, string(metadata))
	return err
}

func (s *sqliteRepository) RevokeRelease(ctx context.Context, producerNamespace, releaseID, actor string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release").SQLiteWhere()
	args := append([]any{actor, releaseID, producerNamespace}, scopeArgs...)
	result, err := s.db.ExecContext(ctx, `UPDATE orh_flow_release SET status='REVOKED',updated_by=?
		WHERE id=? AND namespace_id=? AND status='ACTIVE' AND (`+scopeWhere+`)`, args...)
	return requireAffected(result, err, "release")
}

func (s *sqliteRepository) CanAccessRelease(ctx context.Context, releaseID, namespace string) (bool, error) {
	var allowed int
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release").SQLiteWhere()
	args := append([]any{releaseID, namespace, namespace}, scopeArgs...)
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM orh_flow_release r
		WHERE r.id=? AND r.status='ACTIVE' AND (
			r.namespace_id=? OR r.visibility='SHARED' OR EXISTS (
				SELECT 1 FROM orh_flow_release_grant g WHERE g.release_id=r.id
				AND g.consumer_namespace_id=? AND g.status='ACTIVE'
				AND (g.expires_at IS NULL OR g.expires_at > CURRENT_TIMESTAMP)
			)
		) AND (`+scopeWhere+`))`, args...).Scan(&allowed)
	return allowed == 1, err
}

const grantColumnsSQLite = `id,release_id,consumer_namespace_id,expires_at,COALESCE(description,''),
	namespace_id,status,created_at,COALESCE(created_by,''),updated_at,COALESCE(updated_by,''),
	row_version,COALESCE(metadata,'{}')`

func scanGrantSQLite(row interface{ Scan(...any) error }) (*entities.FlowReleaseGrant, error) {
	var item entities.FlowReleaseGrant
	var expiresAt, createdAt, updatedAt sql.NullString
	var metadata string
	if err := row.Scan(&item.ID, &item.ReleaseID, &item.ConsumerNamespaceID, &expiresAt,
		&item.Description, &item.Namespace, &item.Status, &createdAt, &item.CreatedBy,
		&updatedAt, &item.UpdatedBy, &item.RowVersion, &metadata); err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		value, parseErr := utils.ParseTime(expiresAt.String)
		if parseErr != nil {
			return nil, parseErr
		}
		item.ExpiresAt = &value
	}
	item.CreatedAt, _ = utils.ParseTime(createdAt.String)
	item.UpdatedAt, _ = utils.ParseTime(updatedAt.String)
	_ = json.Unmarshal([]byte(metadata), &item.Metadata)
	item.NormalizeAliases()
	return &item, nil
}

func (s *sqliteRepository) ListGrants(ctx context.Context, producerNamespace, releaseID string) ([]*entities.FlowReleaseGrant, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release_grant").SQLiteWhere()
	args := append([]any{producerNamespace, releaseID}, scopeArgs...)
	rows, err := s.db.QueryContext(ctx, `SELECT `+grantColumnsSQLite+` FROM orh_flow_release_grant
		WHERE namespace_id=? AND release_id=? AND status<>'DELETED' AND (`+scopeWhere+`) ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.FlowReleaseGrant, 0)
	for rows.Next() {
		item, scanErr := scanGrantSQLite(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqliteRepository) SaveGrant(ctx context.Context, item *entities.FlowReleaseGrant) error {
	item.NormalizeAliases()
	visible, err := s.grantCandidateVisible(ctx, item)
	if err != nil {
		return err
	}
	if !visible {
		return storage.ErrFlowgentSqlScopeDenied
	}
	metadata, _ := json.Marshal(item.Metadata)
	_, err = s.db.ExecContext(ctx, `INSERT INTO orh_flow_release_grant
		(id,namespace_id,release_id,consumer_namespace_id,expires_at,description,status,created_at,
		 created_by,updated_at,updated_by,row_version,metadata)
		VALUES (?,?,?,?,?,?,?,?,NULLIF(?,''),?,NULLIF(?,''),?,?)`, item.ID, item.Namespace, item.ReleaseID,
		item.ConsumerNamespaceID, sqliteTimePtr(item.ExpiresAt), item.Description, item.Status,
		formatSQLiteTime(item.CreatedAt), item.CreatedBy, formatSQLiteTime(item.UpdatedAt), item.UpdatedBy,
		item.RowVersion, string(metadata))
	return err
}

func (s *sqliteRepository) DeleteGrant(ctx context.Context, producerNamespace, releaseID, grantID, actor string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release_grant").SQLiteWhere()
	args := append([]any{actor, grantID, releaseID, producerNamespace}, scopeArgs...)
	result, err := s.db.ExecContext(ctx, `UPDATE orh_flow_release_grant SET status='REVOKED',updated_by=?
		WHERE id=? AND release_id=? AND namespace_id=? AND status='ACTIVE' AND (`+scopeWhere+`)`, args...)
	return requireAffected(result, err, "grant")
}

const installationColumnsSQLite = `id,release_id,release_version,producer_namespace_id,installed_flow_id,
	release_checksum,applied_checksum,resource_bindings,installed_at,COALESCE(description,''),namespace_id,
	status,created_at,COALESCE(created_by,''),updated_at,COALESCE(updated_by,''),row_version,
	COALESCE(metadata,'{}')`

func scanInstallationSQLite(row interface{ Scan(...any) error }) (*entities.FlowInstallation, error) {
	var item entities.FlowInstallation
	var bindings, metadata, installedAt, createdAt, updatedAt string
	if err := row.Scan(&item.ID, &item.ReleaseID, &item.ReleaseVersion, &item.ProducerNamespaceID,
		&item.InstalledFlowID, &item.ReleaseChecksum, &item.AppliedChecksum, &bindings, &installedAt,
		&item.Description, &item.Namespace, &item.Status, &createdAt, &item.CreatedBy, &updatedAt,
		&item.UpdatedBy, &item.RowVersion, &metadata); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(bindings), &item.ResourceBindings)
	_ = json.Unmarshal([]byte(metadata), &item.Metadata)
	item.InstalledAt, _ = utils.ParseTime(installedAt)
	item.CreatedAt, _ = utils.ParseTime(createdAt)
	item.UpdatedAt, _ = utils.ParseTime(updatedAt)
	item.NormalizeAliases()
	return &item, nil
}

func (s *sqliteRepository) ListInstallations(ctx context.Context, namespace string) ([]*entities.FlowInstallation, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_installation").SQLiteWhere()
	args := append([]any{namespace}, scopeArgs...)
	rows, err := s.db.QueryContext(ctx, `SELECT `+installationColumnsSQLite+` FROM orh_flow_installation
		WHERE namespace_id=? AND status<>'DELETED' AND (`+scopeWhere+`) ORDER BY installed_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.FlowInstallation, 0)
	for rows.Next() {
		item, scanErr := scanInstallationSQLite(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqliteRepository) Install(ctx context.Context, definition *entities.FlowInfo, installation *entities.FlowInstallation, actor string) error {
	installation.NormalizeAliases()
	visible, err := s.installationCandidateVisible(ctx, installation)
	if err != nil {
		return err
	}
	if !visible {
		return storage.ErrFlowgentSqlScopeDenied
	}
	flowName := installation.InstalledFlowName
	if flowName == "" {
		flowName = installation.InstalledFlowID
	}
	if flowName == "" {
		flowName = definition.ResourceName()
	}
	flowID := uuid.NewString()
	persisted := *definition
	persisted.ID = flowID
	persisted.Name = flowName
	persisted.Revision, persisted.Version = 0, 0
	definitionJSON, err := json.Marshal(&persisted)
	if err != nil {
		return fmt.Errorf("marshal installed flow: %w", err)
	}
	bindingsJSON, err := json.Marshal(installation.ResourceBindings)
	if err != nil {
		return fmt.Errorf("marshal resource bindings: %w", err)
	}
	metadata, _ := json.Marshal(installation.Metadata)
	sum := sha256.Sum256(definitionJSON)
	revisionID := uuid.NewString()
	installation.InstalledFlowID = flowID
	installation.InstalledFlowName = flowName
	installation.AppliedChecksum = hex.EncodeToString(sum[:])

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT INTO orh_flow
		(id,namespace_id,name,description,status,created_by,updated_by)
		VALUES (?,?,?,?,'ACTIVE',?,?)`, flowID, definition.Namespace, flowName,
		definition.Description, actor, actor); err != nil {
		return fmt.Errorf("create installed flow: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO orh_flow_revision
		(id,namespace_id,flow_id,revision,definition,summarize_enabled,checksum,comment,description,status,created_by,updated_by)
		VALUES (?,?,?,1,?,?,?,?,?,'PUBLISHED',?,?)`, revisionID, definition.Namespace, flowID,
		string(definitionJSON), definition.SummarizeEnabled, hex.EncodeToString(sum[:]),
		"Installed release "+installation.ReleaseVersion, definition.Description, actor, actor); err != nil {
		return fmt.Errorf("create installed flow revision: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE orh_flow SET current_revision_id=?,updated_by=? WHERE id=? AND namespace_id=?`,
		revisionID, actor, flowID, definition.Namespace); err != nil {
		return fmt.Errorf("activate installed flow revision: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO orh_flow_installation
		(id,namespace_id,release_id,release_version,producer_namespace_id,installed_flow_id,release_checksum,
		 applied_checksum,resource_bindings,installed_at,description,status,created_at,created_by,updated_at,
		 updated_by,row_version,metadata)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,NULLIF(?,''),?,NULLIF(?,''),?,?)`, installation.ID,
		installation.Namespace, installation.ReleaseID, installation.ReleaseVersion,
		installation.ProducerNamespaceID, installation.InstalledFlowID, installation.ReleaseChecksum,
		installation.AppliedChecksum, string(bindingsJSON), formatSQLiteTime(installation.InstalledAt),
		installation.Description, installation.Status, formatSQLiteTime(installation.CreatedAt),
		installation.CreatedBy, formatSQLiteTime(installation.UpdatedAt), installation.UpdatedBy,
		installation.RowVersion, string(metadata)); err != nil {
		return fmt.Errorf("record flow installation: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	definition.ID = flowID
	definition.Name = flowName
	return nil
}

func (s *sqliteRepository) releaseCandidateVisible(ctx context.Context, item *entities.FlowRelease) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release").SQLiteWhere()
	args := append([]any{item.ID, item.Namespace}, scopeArgs...)
	var visible int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT CAST(? AS TEXT) id,CAST(? AS TEXT) namespace_id) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}

func (s *sqliteRepository) grantCandidateVisible(ctx context.Context, item *entities.FlowReleaseGrant) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release_grant").SQLiteWhere()
	args := append([]any{item.ID, item.ReleaseID, item.Namespace}, scopeArgs...)
	var visible int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT CAST(? AS TEXT) id,CAST(? AS TEXT) release_id,CAST(? AS TEXT) namespace_id) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}

func (s *sqliteRepository) installationCandidateVisible(ctx context.Context, item *entities.FlowInstallation) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_installation").SQLiteWhere()
	args := append([]any{item.ID, item.ReleaseID, item.Namespace}, scopeArgs...)
	var visible int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT CAST(? AS TEXT) id,CAST(? AS TEXT) release_id,CAST(? AS TEXT) namespace_id) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
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

func formatSQLiteTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func sqliteTimePtr(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatSQLiteTime(*value)
}
