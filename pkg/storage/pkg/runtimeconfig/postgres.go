package runtimeconfig

import (
	"context"
	"encoding/json"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresRepository struct{ db *pgxpool.Pool }

func (s *postgresRepository) Get(ctx context.Context, namespace, scopeType, scopeID string) (*entities.RuntimeConfiguration, error) {
	var item entities.RuntimeConfiguration
	var environment, envelope, keys []byte
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_runtime_configuration").PostgresWhere(4)
	args := append([]any{namespace, scopeType, scopeID}, scopeArgs...)
	err := s.db.QueryRow(ctx, `SELECT id,scope_type,scope_id,environment,sealed_secrets,secret_keys,
		description,namespace_id,status,created_at,created_by,updated_at,updated_by,del_flag
		FROM orh_runtime_configuration WHERE namespace_id=$1 AND scope_type=$2 AND scope_id=$3 AND del_flag=false AND (`+scopeWhere+`)`,
		args...).Scan(&item.ID, &item.ScopeType, &item.ScopeID, &environment, &envelope, &keys,
		&item.Description, &item.Namespace, &item.Status, &item.CreatedAt, &item.CreatedBy, &item.UpdatedAt, &item.UpdatedBy, &item.DelFlag)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	if err := decodeRuntimeConfig(&item, environment, envelope, keys); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *postgresRepository) Upsert(ctx context.Context, item *entities.RuntimeConfiguration) error {
	environment, envelope, keys, err := encodeRuntimeConfig(item)
	if err != nil {
		return err
	}
	scope := storage.FlowgentSqlScopeForTable(ctx, "orh_runtime_configuration")
	if scope.Where == "0=1" {
		return storage.ErrFlowgentSqlScopeDenied
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO orh_runtime_configuration
		(id,scope_type,scope_id,environment,sealed_secrets,secret_keys,description,namespace_id,status,
		 created_at,created_by,updated_at,updated_by,del_flag)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,false)
		ON CONFLICT(namespace_id,scope_type,scope_id) DO UPDATE SET
		 environment=EXCLUDED.environment,sealed_secrets=EXCLUDED.sealed_secrets,secret_keys=EXCLUDED.secret_keys,
		 description=EXCLUDED.description,status='ACTIVE',updated_at=EXCLUDED.updated_at,updated_by=EXCLUDED.updated_by,del_flag=false`,
		item.ID, item.ScopeType, item.ScopeID, json.RawMessage(environment), json.RawMessage(envelope), json.RawMessage(keys),
		item.Description, item.Namespace, item.Status, item.CreatedAt, item.CreatedBy, item.UpdatedAt, item.UpdatedBy)
	if err != nil {
		return err
	}
	if scope.Where != "1=1" {
		scopeWhere, scopeArgs := scope.PostgresWhere(4)
		args := append([]any{item.Namespace, item.ScopeType, item.ScopeID}, scopeArgs...)
		var visible int
		if err := tx.QueryRow(ctx, `SELECT COUNT(1) FROM orh_runtime_configuration
			WHERE namespace_id=$1 AND scope_type=$2 AND scope_id=$3 AND (`+scopeWhere+`)`, args...).Scan(&visible); err != nil {
			return err
		}
		if visible != 1 {
			return storage.ErrFlowgentSqlScopeDenied
		}
	}
	return tx.Commit(ctx)
}
