package graphrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresRepository struct{ db *pgxpool.Pool }

func newPostgresRepository(db *pgxpool.Pool) *postgresRepository { return &postgresRepository{db: db} }

const releaseColumnsPG = `id,agent_graph_id,agent_graph_revision_id,agent_graph_revision,release_version,definition,
	checksum,visibility,published_at,COALESCE(description,''),namespace_id,status,created_at,
	COALESCE(created_by,''),updated_at,COALESCE(updated_by,''),row_version,COALESCE(metadata,'{}'::jsonb)`

func scanReleasePG(row interface{ Scan(...any) error }) (*entities.AgentGraphRelease, error) {
	var item entities.AgentGraphRelease
	var definition, metadata []byte
	if err := row.Scan(&item.ID, &item.AgentGraphID, &item.AgentGraphRevisionID, &item.AgentGraphRevision,
		&item.ReleaseVersion, &definition, &item.Checksum, &item.Visibility, &item.PublishedAt,
		&item.Description, &item.Namespace, &item.Status, &item.CreatedAt, &item.CreatedBy,
		&item.UpdatedAt, &item.UpdatedBy, &item.RowVersion, &metadata); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(definition, &item.Definition); err != nil {
		return nil, fmt.Errorf("decode release definition: %w", err)
	}
	item.AgentGraphName = item.Definition.ResourceName()
	_ = json.Unmarshal(metadata, &item.Metadata)
	return &item, nil
}

func (s *postgresRepository) GetRelease(ctx context.Context, id string) (*entities.AgentGraphRelease, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_agent_graph_release").PostgresWhere(2)
	args := append([]any{id}, scopeArgs...)
	return scanReleasePG(s.db.QueryRow(ctx, `SELECT `+releaseColumnsPG+` FROM orh_agent_graph_release
		WHERE id=$1 AND status<>'DELETED' AND (`+scopeWhere+`)`, args...))
}

func (s *postgresRepository) ListAccessibleReleases(ctx context.Context, namespace string) ([]*entities.AgentGraphRelease, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_agent_graph_release").PostgresWhere(2)
	args := append([]any{namespace}, scopeArgs...)
	rows, err := s.db.Query(ctx, `SELECT `+releaseColumnsPG+` FROM orh_agent_graph_release r
		WHERE r.status='ACTIVE' AND (
			r.namespace_id=$1 OR r.visibility='SHARED' OR EXISTS (
				SELECT 1 FROM orh_agent_graph_release_grant g
				WHERE g.release_id=r.id AND g.consumer_namespace_id=$1 AND g.status='ACTIVE'
				  AND (g.expires_at IS NULL OR g.expires_at > NOW())
			)
		) AND (`+scopeWhere+`) ORDER BY r.published_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.AgentGraphRelease, 0)
	for rows.Next() {
		item, scanErr := scanReleasePG(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *postgresRepository) SaveRelease(ctx context.Context, item *entities.AgentGraphRelease) error {
	visible, err := s.releaseCandidateVisible(ctx, item)
	if err != nil {
		return err
	}
	if !visible {
		return storage.ErrFlowgentSqlScopeDenied
	}
	graphKey := item.AgentGraphName
	if graphKey == "" {
		graphKey = item.AgentGraphID
	}
	if item.AgentGraphRevision > 0 {
		err = s.db.QueryRow(ctx, `SELECT f.id,f.name,r.id FROM orh_agent_graph f
			JOIN orh_agent_graph_revision r ON r.agent_graph_id=f.id AND r.namespace_id=f.namespace_id
			WHERE f.namespace_id=$1 AND (f.id=$2 OR f.name=$2) AND r.revision=$3 AND f.status<>'DELETED'`,
			item.Namespace, graphKey, item.AgentGraphRevision).Scan(&item.AgentGraphID, &item.AgentGraphName, &item.AgentGraphRevisionID)
	} else {
		err = s.db.QueryRow(ctx, `SELECT f.id,f.name,r.id,r.revision FROM orh_agent_graph f JOIN orh_agent_graph_revision r ON r.id=f.current_revision_id
			WHERE f.namespace_id=$1 AND (f.id=$2 OR f.name=$2) AND f.status<>'DELETED'`, item.Namespace, graphKey).
			Scan(&item.AgentGraphID, &item.AgentGraphName, &item.AgentGraphRevisionID, &item.AgentGraphRevision)
	}
	if err != nil {
		return fmt.Errorf("resolve graph revision: %w", err)
	}
	item.Definition.ID = item.AgentGraphID
	item.Definition.Name = item.AgentGraphName
	definition, err := json.Marshal(item.Definition)
	if err != nil {
		return err
	}
	metadata, _ := json.Marshal(item.Metadata)
	_, err = s.db.Exec(ctx, `INSERT INTO orh_agent_graph_release
		(id,namespace_id,agent_graph_id,agent_graph_revision_id,agent_graph_revision,release_version,definition,checksum,
		 visibility,published_at,description,status,created_at,created_by,updated_at,updated_by,row_version,metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NULLIF($14,''),$15,NULLIF($16,''),$17,$18)`,
		item.ID, item.Namespace, item.AgentGraphID, item.AgentGraphRevisionID, item.AgentGraphRevision, item.ReleaseVersion,
		definition, item.Checksum, item.Visibility, item.PublishedAt, item.Description, item.Status,
		item.CreatedAt, item.CreatedBy, item.UpdatedAt, item.UpdatedBy, item.RowVersion, metadata)
	return err
}

func (s *postgresRepository) RevokeRelease(ctx context.Context, producerNamespace, releaseID, actor string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_agent_graph_release").PostgresWhere(4)
	args := append([]any{actor, releaseID, producerNamespace}, scopeArgs...)
	result, err := s.db.Exec(ctx, `UPDATE orh_agent_graph_release SET status='REVOKED',updated_by=$1
		WHERE id=$2 AND namespace_id=$3 AND status='ACTIVE' AND (`+scopeWhere+`)`, args...)
	return requirePGAffected(result.RowsAffected(), err, "release")
}

func (s *postgresRepository) CanAccessRelease(ctx context.Context, releaseID, namespace string) (bool, error) {
	var allowed bool
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_agent_graph_release").PostgresWhere(3)
	args := append([]any{releaseID, namespace}, scopeArgs...)
	err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orh_agent_graph_release r
		WHERE r.id=$1 AND r.status='ACTIVE' AND (
			r.namespace_id=$2 OR r.visibility='SHARED' OR EXISTS (
				SELECT 1 FROM orh_agent_graph_release_grant g WHERE g.release_id=r.id
				AND g.consumer_namespace_id=$2 AND g.status='ACTIVE'
				AND (g.expires_at IS NULL OR g.expires_at > NOW())
			)
		) AND (`+scopeWhere+`))`, args...).Scan(&allowed)
	return allowed, err
}

const grantColumnsPG = `id,release_id,consumer_namespace_id,expires_at,COALESCE(description,''),
	namespace_id,status,created_at,COALESCE(created_by,''),updated_at,COALESCE(updated_by,''),
	row_version,COALESCE(metadata,'{}'::jsonb)`

func scanGrantPG(row interface{ Scan(...any) error }) (*entities.AgentGraphReleaseGrant, error) {
	var item entities.AgentGraphReleaseGrant
	var metadata []byte
	if err := row.Scan(&item.ID, &item.ReleaseID, &item.ConsumerNamespaceID, &item.ExpiresAt,
		&item.Description, &item.Namespace, &item.Status, &item.CreatedAt, &item.CreatedBy,
		&item.UpdatedAt, &item.UpdatedBy, &item.RowVersion, &metadata); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(metadata, &item.Metadata)
	return &item, nil
}

func (s *postgresRepository) ListGrants(ctx context.Context, producerNamespace, releaseID string) ([]*entities.AgentGraphReleaseGrant, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_agent_graph_release_grant").PostgresWhere(3)
	args := append([]any{producerNamespace, releaseID}, scopeArgs...)
	rows, err := s.db.Query(ctx, `SELECT `+grantColumnsPG+` FROM orh_agent_graph_release_grant
		WHERE namespace_id=$1 AND release_id=$2 AND status<>'DELETED' AND (`+scopeWhere+`) ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.AgentGraphReleaseGrant, 0)
	for rows.Next() {
		item, scanErr := scanGrantPG(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *postgresRepository) SaveGrant(ctx context.Context, item *entities.AgentGraphReleaseGrant) error {
	visible, err := s.grantCandidateVisible(ctx, item)
	if err != nil {
		return err
	}
	if !visible {
		return storage.ErrFlowgentSqlScopeDenied
	}
	metadata, _ := json.Marshal(item.Metadata)
	_, err = s.db.Exec(ctx, `INSERT INTO orh_agent_graph_release_grant
		(id,namespace_id,release_id,consumer_namespace_id,expires_at,description,status,created_at,
		 created_by,updated_at,updated_by,row_version,metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10,NULLIF($11,''),$12,$13)`,
		item.ID, item.Namespace, item.ReleaseID, item.ConsumerNamespaceID, item.ExpiresAt, item.Description,
		item.Status, item.CreatedAt, item.CreatedBy, item.UpdatedAt, item.UpdatedBy, item.RowVersion, metadata)
	return err
}

func (s *postgresRepository) DeleteGrant(ctx context.Context, producerNamespace, releaseID, grantID, actor string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_agent_graph_release_grant").PostgresWhere(5)
	args := append([]any{actor, grantID, releaseID, producerNamespace}, scopeArgs...)
	result, err := s.db.Exec(ctx, `UPDATE orh_agent_graph_release_grant SET status='REVOKED',updated_by=$1
		WHERE id=$2 AND release_id=$3 AND namespace_id=$4 AND status='ACTIVE' AND (`+scopeWhere+`)`, args...)
	return requirePGAffected(result.RowsAffected(), err, "grant")
}

const installationColumnsPG = `id,release_id,release_version,producer_namespace_id,installed_agent_graph_id,
	release_checksum,applied_checksum,resource_bindings,installed_at,COALESCE(description,''),namespace_id,
	status,created_at,COALESCE(created_by,''),updated_at,COALESCE(updated_by,''),row_version,
	COALESCE(metadata,'{}'::jsonb)`

func scanInstallationPG(row interface{ Scan(...any) error }) (*entities.AgentGraphInstallation, error) {
	var item entities.AgentGraphInstallation
	var bindings, metadata []byte
	if err := row.Scan(&item.ID, &item.ReleaseID, &item.ReleaseVersion, &item.ProducerNamespaceID,
		&item.InstalledAgentGraphID, &item.ReleaseChecksum, &item.AppliedChecksum, &bindings,
		&item.InstalledAt, &item.Description, &item.Namespace, &item.Status, &item.CreatedAt,
		&item.CreatedBy, &item.UpdatedAt, &item.UpdatedBy, &item.RowVersion, &metadata); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(bindings, &item.ResourceBindings)
	_ = json.Unmarshal(metadata, &item.Metadata)
	return &item, nil
}

func (s *postgresRepository) ListInstallations(ctx context.Context, namespace string) ([]*entities.AgentGraphInstallation, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_agent_graph_installation").PostgresWhere(2)
	args := append([]any{namespace}, scopeArgs...)
	rows, err := s.db.Query(ctx, `SELECT `+installationColumnsPG+` FROM orh_agent_graph_installation
		WHERE namespace_id=$1 AND status<>'DELETED' AND (`+scopeWhere+`) ORDER BY installed_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.AgentGraphInstallation, 0)
	for rows.Next() {
		item, scanErr := scanInstallationPG(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *postgresRepository) Install(ctx context.Context, definition *entities.AgentGraph, installation *entities.AgentGraphInstallation, actor string) error {
	visible, err := s.installationCandidateVisible(ctx, installation)
	if err != nil {
		return err
	}
	if !visible {
		return storage.ErrFlowgentSqlScopeDenied
	}
	graphName := installation.InstalledAgentGraphName
	if graphName == "" {
		graphName = installation.InstalledAgentGraphID
	}
	if graphName == "" {
		graphName = definition.ResourceName()
	}
	graphID := uuid.NewString()
	persisted := *definition
	persisted.ID = graphID
	persisted.Name = graphName
	persisted.Revision = 0
	definitionJSON, err := json.Marshal(&persisted)
	if err != nil {
		return fmt.Errorf("marshal installed graph: %w", err)
	}
	bindingsJSON, err := json.Marshal(installation.ResourceBindings)
	if err != nil {
		return fmt.Errorf("marshal resource bindings: %w", err)
	}
	metadata, _ := json.Marshal(installation.Metadata)
	sum := sha256.Sum256(definitionJSON)
	revisionID := uuid.NewString()
	installation.InstalledAgentGraphID = graphID
	installation.InstalledAgentGraphName = graphName
	installation.AppliedChecksum = hex.EncodeToString(sum[:])

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO orh_agent_graph
		(id,namespace_id,name,description,status,created_by,updated_by)
		VALUES ($1,$2,$3,$4,'ACTIVE',$5,$5)`, graphID, definition.Namespace, graphName, definition.Description, actor); err != nil {
		return fmt.Errorf("create installed graph: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO orh_agent_graph_revision
		(id,namespace_id,agent_graph_id,revision,definition,summarize_enabled,checksum,comment,description,status,created_by,updated_by)
		VALUES ($1,$2,$3,1,$4,$5,$6,$7,$8,'PUBLISHED',$9,$9)`, revisionID, definition.Namespace,
		graphID, definitionJSON, definition.SummarizeEnabled, hex.EncodeToString(sum[:]),
		"Installed release "+installation.ReleaseVersion, definition.Description, actor); err != nil {
		return fmt.Errorf("create installed graph revision: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE orh_agent_graph SET current_revision_id=$1,updated_by=$2 WHERE id=$3 AND namespace_id=$4`,
		revisionID, actor, graphID, definition.Namespace); err != nil {
		return fmt.Errorf("activate installed graph revision: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO orh_agent_graph_installation
		(id,namespace_id,release_id,release_version,producer_namespace_id,installed_agent_graph_id,release_checksum,
		 applied_checksum,resource_bindings,installed_at,description,status,created_at,created_by,updated_at,
		 updated_by,row_version,metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NULLIF($14,''),$15,NULLIF($16,''),$17,$18)`,
		installation.ID, installation.Namespace, installation.ReleaseID, installation.ReleaseVersion,
		installation.ProducerNamespaceID, installation.InstalledAgentGraphID, installation.ReleaseChecksum,
		installation.AppliedChecksum, bindingsJSON, installation.InstalledAt, installation.Description,
		installation.Status, installation.CreatedAt, installation.CreatedBy, installation.UpdatedAt,
		installation.UpdatedBy, installation.RowVersion, metadata); err != nil {
		return fmt.Errorf("record graph installation: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	definition.ID = graphID
	definition.Name = graphName
	return nil
}

func (s *postgresRepository) releaseCandidateVisible(ctx context.Context, item *entities.AgentGraphRelease) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_agent_graph_release").PostgresWhere(3)
	args := append([]any{item.ID, item.Namespace}, scopeArgs...)
	var visible int
	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM (SELECT CAST($1 AS TEXT) id,CAST($2 AS TEXT) namespace_id) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}

func (s *postgresRepository) grantCandidateVisible(ctx context.Context, item *entities.AgentGraphReleaseGrant) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_agent_graph_release_grant").PostgresWhere(4)
	args := append([]any{item.ID, item.ReleaseID, item.Namespace}, scopeArgs...)
	var visible int
	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM (SELECT CAST($1 AS TEXT) id,CAST($2 AS TEXT) release_id,CAST($3 AS TEXT) namespace_id) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}

func (s *postgresRepository) installationCandidateVisible(ctx context.Context, item *entities.AgentGraphInstallation) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_agent_graph_installation").PostgresWhere(4)
	args := append([]any{item.ID, item.ReleaseID, item.Namespace}, scopeArgs...)
	var visible int
	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM (SELECT CAST($1 AS TEXT) id,CAST($2 AS TEXT) release_id,CAST($3 AS TEXT) namespace_id) candidate WHERE `+scopeWhere, args...).Scan(&visible)
	return visible == 1, err
}

func requirePGAffected(affected int64, err error, resource string) error {
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("%s not found", resource)
	}
	return nil
}
