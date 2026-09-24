package runtimeconfig

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresRepository struct{ db *pgxpool.Pool }

func (s *postgresRepository) Get(ctx context.Context, namespace, scope, scopeID string) (*entities.RuntimeConfiguration, error) {
	flowID, flowName := "", ""
	if scope == entities.RuntimeConfigScopeFlow {
		if err := s.db.QueryRow(ctx, `SELECT id,name FROM orh_flow WHERE namespace_id=$1 AND (id=$2 OR name=$2) AND status<>'DELETED'`, namespace, scopeID).Scan(&flowID, &flowName); err != nil {
			return nil, normalizeNotFound(err)
		}
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_runtime_configuration").PostgresWhere(4)
	args := append([]any{namespace, scope, flowID}, scopeArgs...)
	var item entities.RuntimeConfiguration
	var environment, envelope, keys, metadata []byte
	err := s.db.QueryRow(ctx, `SELECT id,scope,COALESCE(flow_id,''),environment,sealed_secrets,secret_keys,description,namespace_id,status,created_at,created_by,updated_at,updated_by,row_version,metadata FROM orh_runtime_configuration WHERE namespace_id=$1 AND scope=$2 AND flow_id IS NOT DISTINCT FROM NULLIF($3,'') AND status<>'DELETED' AND (`+scopeWhere+`)`, args...).Scan(&item.ID, &item.Scope, &item.FlowID, &environment, &envelope, &keys, &item.Description, &item.Namespace, &item.Status, &item.CreatedAt, &item.CreatedBy, &item.UpdatedAt, &item.UpdatedBy, &item.RowVersion, &metadata)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	item.FlowName = flowName
	item.NormalizeAliases()
	if err = decodeRuntimeConfig(&item, environment, envelope, keys); err != nil {
		return nil, err
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &item.Metadata)
	}
	return &item, nil
}
func (s *postgresRepository) Upsert(ctx context.Context, item *entities.RuntimeConfiguration) error {
	item.NormalizeAliases()
	flowKey := item.FlowKey()
	environment, envelope, keys, err := encodeRuntimeConfig(item)
	if err != nil {
		return err
	}
	scope := storage.FlowgentSqlScopeForTable(ctx, "orh_runtime_configuration")
	if scope.Where == "0=1" {
		return storage.ErrFlowgentSqlScopeDenied
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO orh_namespace(id,name,description,created_by,updated_by) VALUES($1::varchar(64),$1::text,'Flowgent namespace',$2::varchar(255),$2::varchar(255)) ON CONFLICT(id) DO NOTHING`, item.Namespace, item.CreatedBy); err != nil {
		return err
	}
	if item.Scope == entities.RuntimeConfigScopeFlow {
		if err = tx.QueryRow(ctx, `SELECT id,name FROM orh_flow WHERE namespace_id=$1 AND (id=$2 OR name=$2) AND status<>'DELETED' FOR SHARE`, item.Namespace, flowKey).Scan(&item.FlowID, &item.FlowName); err != nil {
			return fmt.Errorf("resolve runtime configuration flow: %w", err)
		}
		item.ScopeID = item.FlowName
	} else {
		item.FlowID, item.FlowName, item.ScopeID = "", "", ""
	}
	var id string
	var rowVersion int64
	err = tx.QueryRow(ctx, `SELECT id,row_version FROM orh_runtime_configuration WHERE namespace_id=$1 AND scope=$2 AND flow_id IS NOT DISTINCT FROM NULLIF($3,'') FOR UPDATE`, item.Namespace, item.Scope, item.FlowID).Scan(&id, &rowVersion)
	if err == pgx.ErrNoRows {
		if _, err = tx.Exec(ctx, `INSERT INTO orh_runtime_configuration(id,namespace_id,scope,flow_id,environment,sealed_secrets,secret_keys,description,status,created_by,updated_by,metadata) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,'ACTIVE',$9,$9,$10)`, item.ID, item.Namespace, item.Scope, item.FlowID, environment, envelope, keys, item.Description, item.CreatedBy, jsonBytes(item.Metadata)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		result, updateErr := tx.Exec(ctx, `UPDATE orh_runtime_configuration SET environment=$1,sealed_secrets=$2,secret_keys=$3,description=$4,status='ACTIVE',updated_by=$5,metadata=$6 WHERE id=$7 AND row_version=$8`, environment, envelope, keys, item.Description, item.UpdatedBy, jsonBytes(item.Metadata), id, rowVersion)
		if updateErr != nil {
			return updateErr
		}
		if result.RowsAffected() != 1 {
			return fmt.Errorf("runtime configuration CAS conflict")
		}
		item.ID = id
	}
	if scope.Where != "1=1" {
		where, args0 := scope.PostgresWhere(4)
		args := append([]any{item.Namespace, item.Scope, item.FlowID}, args0...)
		var visible int
		if err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM orh_runtime_configuration WHERE namespace_id=$1 AND scope=$2 AND flow_id IS NOT DISTINCT FROM NULLIF($3,'') AND (`+where+`)`, args...).Scan(&visible); err != nil {
			return err
		}
		if visible != 1 {
			return storage.ErrFlowgentSqlScopeDenied
		}
	}
	return tx.Commit(ctx)
}
func jsonBytes(value any) []byte { data, _ := json.Marshal(value); return data }
