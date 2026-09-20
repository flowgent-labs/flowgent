package flowrelease

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresRepository struct {
	db *pgxpool.Pool
}

func newPostgresRepository(db *pgxpool.Pool) *postgresRepository {
	return &postgresRepository{db: db}
}

func (s *postgresRepository) GetRelease(ctx context.Context, id string) (*entities.FlowRelease, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release").PostgresWhere(2)
	args := append([]any{id}, scopeArgs...)
	query := fmt.Sprintf(`SELECT %s FROM orh_flow_release WHERE id=$1 AND del_flag=false AND (%s)`, utils.Columns[entities.FlowRelease](), scopeWhere)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, fmt.Errorf("release not found")
	}
	var release entities.FlowRelease
	if err := utils.ScanStruct(rows, &release); err != nil {
		return nil, err
	}
	return &release, nil
}

func (s *postgresRepository) ListAccessibleReleases(ctx context.Context, namespace string) ([]*entities.FlowRelease, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release").PostgresWhere(2)
	query := fmt.Sprintf(`SELECT %s FROM orh_flow_release r
		WHERE r.del_flag=false AND r.status='ACTIVE' AND (
			r.namespace_id=$1 OR r.visibility='SHARED' OR EXISTS (
				SELECT 1 FROM orh_flow_release_grant g
				WHERE g.release_id=r.id AND g.consumer_namespace=$1 AND g.del_flag=false AND g.status='ACTIVE'
				  AND (g.expires_at IS NULL OR g.expires_at > NOW())
			)
		) AND (%s) ORDER BY r.published_at DESC`, utils.Columns[entities.FlowRelease](), scopeWhere)
	args := append([]any{namespace}, scopeArgs...)
	return scanPostgres[entities.FlowRelease](ctx, s.db, query, args...)
}

func (s *postgresRepository) SaveRelease(ctx context.Context, item *entities.FlowRelease) error {
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
	_, err = s.db.Exec(ctx, `INSERT INTO orh_flow_release
		(id,flow_id,flow_version,release_version,definition,checksum,visibility,published_at,description,
		 namespace_id,status,created_at,created_by,updated_at,updated_by,del_flag)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,false)`, item.ID, item.FlowID,
		item.FlowVersion, item.ReleaseVersion, definition, item.Checksum, item.Visibility, item.PublishedAt,
		item.Description, item.Namespace, item.Status, item.CreatedAt, item.CreatedBy, item.UpdatedAt, item.UpdatedBy)
	return err
}

func (s *postgresRepository) RevokeRelease(ctx context.Context, producerNamespace, releaseID, actor string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release").PostgresWhere(4)
	args := append([]any{actor, releaseID, producerNamespace}, scopeArgs...)
	command, err := s.db.Exec(ctx, `UPDATE orh_flow_release SET status='REVOKED',updated_at=NOW(),updated_by=$1
		WHERE id=$2 AND namespace_id=$3 AND del_flag=false AND status='ACTIVE' AND (`+scopeWhere+`)`, args...)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("release not found")
	}
	return nil
}

func (s *postgresRepository) CanAccessRelease(ctx context.Context, releaseID, namespace string) (bool, error) {
	var allowed bool
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release").PostgresWhere(3)
	args := append([]any{releaseID, namespace}, scopeArgs...)
	err := s.db.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM orh_flow_release r WHERE r.id=$1 AND r.del_flag=false AND r.status='ACTIVE' AND (
			r.namespace_id=$2 OR r.visibility='SHARED' OR EXISTS (
				SELECT 1 FROM orh_flow_release_grant g WHERE g.release_id=r.id
				AND g.consumer_namespace=$2 AND g.del_flag=false AND g.status='ACTIVE'
				AND (g.expires_at IS NULL OR g.expires_at > NOW()))) AND (`+scopeWhere+`))`, args...).Scan(&allowed)
	return allowed, err
}

func (s *postgresRepository) ListGrants(ctx context.Context, producerNamespace, releaseID string) ([]*entities.FlowReleaseGrant, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release_grant").PostgresWhere(3)
	query := fmt.Sprintf(`SELECT %s FROM orh_flow_release_grant
		WHERE namespace_id=$1 AND release_id=$2 AND del_flag=false AND (%s) ORDER BY created_at DESC`, utils.Columns[entities.FlowReleaseGrant](), scopeWhere)
	args := append([]any{producerNamespace, releaseID}, scopeArgs...)
	return scanPostgres[entities.FlowReleaseGrant](ctx, s.db, query, args...)
}

func (s *postgresRepository) SaveGrant(ctx context.Context, item *entities.FlowReleaseGrant) error {
	visible, err := s.grantCandidateVisible(ctx, item)
	if err != nil {
		return err
	}
	if !visible {
		return storage.ErrFlowgentSqlScopeDenied
	}
	_, err = s.db.Exec(ctx, `INSERT INTO orh_flow_release_grant
		(id,release_id,consumer_namespace,expires_at,description,namespace_id,status,created_at,created_by,updated_at,updated_by,del_flag)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,false)`, item.ID, item.ReleaseID,
		item.ConsumerNamespace, item.ExpiresAt, item.Description, item.Namespace, item.Status,
		item.CreatedAt, item.CreatedBy, item.UpdatedAt, item.UpdatedBy)
	return err
}

func (s *postgresRepository) DeleteGrant(ctx context.Context, producerNamespace, releaseID, grantID, actor string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release_grant").PostgresWhere(5)
	args := append([]any{actor, grantID, releaseID, producerNamespace}, scopeArgs...)
	command, err := s.db.Exec(ctx, `UPDATE orh_flow_release_grant
		SET status='REVOKED',del_flag=true,updated_at=NOW(),updated_by=$1
		WHERE id=$2 AND release_id=$3 AND namespace_id=$4 AND del_flag=false AND (`+scopeWhere+`)`, args...)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("grant not found")
	}
	return nil
}

func (s *postgresRepository) ListInstallations(ctx context.Context, namespace string) ([]*entities.FlowInstallation, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_installation").PostgresWhere(2)
	query := fmt.Sprintf(`SELECT %s FROM orh_flow_installation
		WHERE namespace_id=$1 AND del_flag=false AND (%s) ORDER BY installed_at DESC`, utils.Columns[entities.FlowInstallation](), scopeWhere)
	args := append([]any{namespace}, scopeArgs...)
	return scanPostgres[entities.FlowInstallation](ctx, s.db, query, args...)
}

func (s *postgresRepository) Install(ctx context.Context, definition *entities.FlowInfo, installation *entities.FlowInstallation, actor string) error {
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
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO orh_agentflow
		(id,agentflow_id,version,definition,created_by,comment,namespace_id,status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'ACTIVE')`, uuid.NewString(), definition.ID, 1, definitionJSON,
		actor, "Installed release "+installation.ReleaseVersion, definition.Namespace); err != nil {
		return fmt.Errorf("create installed flow: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO orh_flow_installation
		(id,release_id,release_version,producer_namespace,installed_flow_id,release_checksum,applied_checksum,
		 resource_bindings,installed_at,description,namespace_id,status,created_at,created_by,updated_at,updated_by,del_flag)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,false)`, installation.ID,
		installation.ReleaseID, installation.ReleaseVersion, installation.ProducerNamespace, installation.InstalledFlowID,
		installation.ReleaseChecksum, installation.AppliedChecksum, bindingsJSON, installation.InstalledAt,
		installation.Description, installation.Namespace, installation.Status, installation.CreatedAt,
		installation.CreatedBy, installation.UpdatedAt, installation.UpdatedBy); err != nil {
		return fmt.Errorf("record flow installation: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *postgresRepository) releaseCandidateVisible(ctx context.Context, item *entities.FlowRelease) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release").PostgresWhere(3)
	args := append([]any{item.ID, item.Namespace}, scopeArgs...)
	var visible int
	err := s.db.QueryRow(ctx, `SELECT COUNT(1) FROM (
		SELECT CAST($1 AS TEXT) AS id, CAST($2 AS TEXT) AS namespace_id
	) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}

func (s *postgresRepository) grantCandidateVisible(ctx context.Context, item *entities.FlowReleaseGrant) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_release_grant").PostgresWhere(4)
	args := append([]any{item.ID, item.ReleaseID, item.Namespace}, scopeArgs...)
	var visible int
	err := s.db.QueryRow(ctx, `SELECT COUNT(1) FROM (
		SELECT CAST($1 AS TEXT) AS id, CAST($2 AS TEXT) AS release_id, CAST($3 AS TEXT) AS namespace_id
	) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}

func (s *postgresRepository) installationCandidateVisible(ctx context.Context, item *entities.FlowInstallation) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_flow_installation").PostgresWhere(4)
	args := append([]any{item.ID, item.ReleaseID, item.Namespace}, scopeArgs...)
	var visible int
	err := s.db.QueryRow(ctx, `SELECT COUNT(1) FROM (
		SELECT CAST($1 AS TEXT) AS id, CAST($2 AS TEXT) AS release_id, CAST($3 AS TEXT) AS namespace_id
	) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}

func scanPostgres[T any](ctx context.Context, db *pgxpool.Pool, query string, args ...any) ([]*T, error) {
	rows, err := db.Query(ctx, query, args...)
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
