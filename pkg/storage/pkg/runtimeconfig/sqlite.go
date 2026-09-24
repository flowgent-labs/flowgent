package runtimeconfig

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/flowgent-labs/flowgent/common/pkg/secretbox"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
)

type sqliteRepository struct{ db *sql.DB }

func (s *sqliteRepository) Get(ctx context.Context, namespace, scope, scopeID string) (*entities.RuntimeConfiguration, error) {
	flowID, flowName := "", ""
	if scope == entities.RuntimeConfigScopeFlow {
		if err := s.db.QueryRowContext(ctx, `SELECT id,name FROM orh_flow WHERE namespace_id=? AND (id=? OR name=?) AND status<>'DELETED'`, namespace, scopeID, scopeID).Scan(&flowID, &flowName); err != nil {
			return nil, normalizeNotFound(err)
		}
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_runtime_configuration").SQLiteWhere()
	args := append([]any{namespace, scope, flowID}, scopeArgs...)
	var item entities.RuntimeConfiguration
	var environment, envelope, keys sql.NullString
	var createdAt, updatedAt string
	var metadata sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,scope,COALESCE(flow_id,''),environment,sealed_secrets,secret_keys,description,namespace_id,status,created_at,created_by,updated_at,updated_by,row_version,metadata FROM orh_runtime_configuration WHERE namespace_id=?1 AND scope=?2 AND flow_id IS NULLIF(?3,'') AND status<>'DELETED' AND (`+scopeWhere+`)`, args...).Scan(&item.ID, &item.Scope, &item.FlowID, &environment, &envelope, &keys, &item.Description, &item.Namespace, &item.Status, &createdAt, &item.CreatedBy, &updatedAt, &item.UpdatedBy, &item.RowVersion, &metadata)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	item.FlowName = flowName
	item.NormalizeAliases()
	if err = decodeRuntimeConfig(&item, []byte(environment.String), []byte(envelope.String), []byte(keys.String)); err != nil {
		return nil, err
	}
	if metadata.Valid {
		_ = json.Unmarshal([]byte(metadata.String), &item.Metadata)
	}
	item.CreatedAt, _ = utils.ParseTime(createdAt)
	item.UpdatedAt, _ = utils.ParseTime(updatedAt)
	return &item, nil
}
func (s *sqliteRepository) Upsert(ctx context.Context, item *entities.RuntimeConfiguration) error {
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO orh_namespace(id,name,description,created_by,updated_by) VALUES(?,?,?, ?,?)`, item.Namespace, item.Namespace, "Flowgent namespace", item.CreatedBy, item.CreatedBy); err != nil {
		return err
	}
	if item.Scope == entities.RuntimeConfigScopeFlow {
		if err = tx.QueryRowContext(ctx, `SELECT id,name FROM orh_flow WHERE namespace_id=? AND (id=? OR name=?) AND status<>'DELETED'`, item.Namespace, flowKey, flowKey).Scan(&item.FlowID, &item.FlowName); err != nil {
			return fmt.Errorf("resolve runtime configuration flow: %w", err)
		}
		item.ScopeID = item.FlowName
	} else {
		item.FlowID, item.FlowName, item.ScopeID = "", "", ""
	}
	var id string
	var rowVersion int64
	err = tx.QueryRowContext(ctx, `SELECT id,row_version FROM orh_runtime_configuration WHERE namespace_id=? AND scope=? AND flow_id IS NULLIF(?,'')`, item.Namespace, item.Scope, item.FlowID).Scan(&id, &rowVersion)
	if err == sql.ErrNoRows {
		if _, err = tx.ExecContext(ctx, `INSERT INTO orh_runtime_configuration(id,namespace_id,scope,flow_id,environment,sealed_secrets,secret_keys,description,status,created_by,updated_by,metadata) VALUES(?,?,?,NULLIF(?,''),?,?,?,?, 'ACTIVE',?,?,?)`, item.ID, item.Namespace, item.Scope, item.FlowID, string(environment), string(envelope), string(keys), item.Description, item.CreatedBy, item.CreatedBy, string(jsonBytes(item.Metadata))); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		result, updateErr := tx.ExecContext(ctx, `UPDATE orh_runtime_configuration SET environment=?,sealed_secrets=?,secret_keys=?,description=?,status='ACTIVE',updated_by=?,metadata=? WHERE id=? AND row_version=?`, string(environment), string(envelope), string(keys), item.Description, item.UpdatedBy, string(jsonBytes(item.Metadata)), id, rowVersion)
		if updateErr != nil {
			return updateErr
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return fmt.Errorf("runtime configuration CAS conflict")
		}
		item.ID = id
	}
	if scope.Where != "1=1" {
		where, args0 := scope.SQLiteWhere()
		args := append([]any{item.Namespace, item.Scope, item.FlowID}, args0...)
		var visible int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM orh_runtime_configuration WHERE namespace_id=?1 AND scope=?2 AND flow_id IS NULLIF(?3,'') AND (`+where+`)`, args...).Scan(&visible); err != nil {
			return err
		}
		if visible != 1 {
			return storage.ErrFlowgentSqlScopeDenied
		}
	}
	return tx.Commit()
}
func encodeRuntimeConfig(item *entities.RuntimeConfiguration) ([]byte, []byte, []byte, error) {
	environment, err := json.Marshal(item.Environment)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal runtime environment: %w", err)
	}
	envelope, err := json.Marshal(item.SealedSecrets)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal runtime secrets: %w", err)
	}
	keys, err := json.Marshal(item.ConfiguredSecretKeys)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal runtime secret keys: %w", err)
	}
	return environment, envelope, keys, nil
}
func decodeRuntimeConfig(item *entities.RuntimeConfiguration, environment, envelope, keys []byte) error {
	if err := json.Unmarshal(environment, &item.Environment); err != nil {
		return fmt.Errorf("decode runtime environment: %w", err)
	}
	if string(envelope) != "" && string(envelope) != "null" && string(envelope) != "{}" {
		var value secretbox.Envelope
		if err := json.Unmarshal(envelope, &value); err != nil {
			return fmt.Errorf("decode runtime secrets: %w", err)
		}
		item.SealedSecrets = &value
	}
	if err := json.Unmarshal(keys, &item.ConfiguredSecretKeys); err != nil {
		return fmt.Errorf("decode runtime secret keys: %w", err)
	}
	if item.Environment == nil {
		item.Environment = map[string]string{}
	}
	return nil
}
