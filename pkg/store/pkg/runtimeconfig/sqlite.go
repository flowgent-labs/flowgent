package runtimeconfig

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/secretbox"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

type sqliteRepository struct{ db *sql.DB }

func (s *sqliteRepository) Get(ctx context.Context, namespace, scopeType, scopeID string) (*entities.RuntimeConfiguration, error) {
	var item entities.RuntimeConfiguration
	var environment, envelope, keys string
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx, `SELECT id,scope_type,scope_id,environment,sealed_secrets,secret_keys,
		description,namespace_id,status,created_at,created_by,updated_at,updated_by,del_flag
		FROM orh_runtime_configuration WHERE namespace_id=? AND scope_type=? AND scope_id=? AND del_flag=0`,
		namespace, scopeType, scopeID).Scan(&item.ID, &item.ScopeType, &item.ScopeID, &environment, &envelope, &keys,
		&item.Description, &item.Namespace, &item.Status, &createdAt, &item.CreatedBy, &updatedAt, &item.UpdatedBy, &item.DelFlag)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	if err := decodeRuntimeConfig(&item, []byte(environment), []byte(envelope), []byte(keys)); err != nil {
		return nil, err
	}
	item.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
	if err != nil {
		return nil, fmt.Errorf("decode runtime created_at: %w", err)
	}
	item.UpdatedAt, err = time.Parse("2006-01-02 15:04:05", updatedAt)
	if err != nil {
		return nil, fmt.Errorf("decode runtime updated_at: %w", err)
	}
	return &item, nil
}

func (s *sqliteRepository) Upsert(ctx context.Context, item *entities.RuntimeConfiguration) error {
	environment, envelope, keys, err := encodeRuntimeConfig(item)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO orh_runtime_configuration
		(id,scope_type,scope_id,environment,sealed_secrets,secret_keys,description,namespace_id,status,
		 created_at,created_by,updated_at,updated_by,del_flag)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0)
		ON CONFLICT(namespace_id,scope_type,scope_id) DO UPDATE SET
		 environment=excluded.environment,sealed_secrets=excluded.sealed_secrets,secret_keys=excluded.secret_keys,
		 description=excluded.description,status='ACTIVE',updated_at=excluded.updated_at,updated_by=excluded.updated_by,del_flag=0`,
		item.ID, item.ScopeType, item.ScopeID, environment, envelope, keys, item.Description, item.Namespace,
		item.Status, formatSQLiteTime(item.CreatedAt), item.CreatedBy, formatSQLiteTime(item.UpdatedAt), item.UpdatedBy)
	return err
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

func formatSQLiteTime(value time.Time) string { return value.UTC().Format("2006-01-02 15:04:05") }
