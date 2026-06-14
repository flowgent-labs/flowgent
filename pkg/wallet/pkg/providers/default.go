// Package providers implements secret store providers for wallet key management.
package providers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/flowgent-labs/flowgent/wallet/pkg"
)

// DefaultSecretStoreProvider stores encrypted secrets in SQLite or Postgres.
// Secrets are encrypted with AES-256-GCM. The master key comes from the
// flowgent.yaml config (payments.wallet.secret_store.master_key or
// master_key_file), where ${VAR} placeholders are expanded from CSI credentials.
type DefaultSecretStoreProvider struct {
	db        *sql.DB
	gcm       cipher.AEAD
	masterKey []byte
}

// NewDefaultSecretStoreProvider creates a new default secret store.
// masterKeyFile comes from config (payments.wallet.secret_store.master_key_file).
func NewDefaultSecretStoreProvider(db *sql.DB, masterKeyFile string) (*DefaultSecretStoreProvider, error) {
	key, err := resolveMasterKey(masterKeyFile)
	if err != nil {
		return nil, fmt.Errorf("resolve master key: %w", err)
	}

	hashed := sha256.Sum256(key)
	block, err := aes.NewCipher(hashed[:])
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	store := &DefaultSecretStoreProvider{
		db:        db,
		gcm:       gcm,
		masterKey: key,
	}

	if err := store.migrate(context.Background()); err != nil {
		return nil, fmt.Errorf("migrate secret store: %w", err)
	}

	return store, nil
}

// resolveMasterKey reads the master key from the configured file path.
func resolveMasterKey(masterKeyFile string) ([]byte, error) {
	if masterKeyFile != "" {
		data, err := os.ReadFile(masterKeyFile)
		if err != nil {
			return nil, fmt.Errorf("read master key file %s: %w", masterKeyFile, err)
		}
		return []byte(strings.TrimSpace(string(data))), nil
	}
	return nil, fmt.Errorf("no master key configured: set payments.wallet.secret_store.master_key_file in flowgent.yaml")
}

func (s *DefaultSecretStoreProvider) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS payment_secrets (
			key   TEXT PRIMARY KEY,
			value BYTEA NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	return err
}

// GetSecret retrieves and decrypts a secret by key.
func (s *DefaultSecretStoreProvider) GetSecret(ctx context.Context, key string) ([]byte, error) {
	var encrypted []byte
	err := s.db.QueryRowContext(ctx, "SELECT value FROM payment_secrets WHERE key = $1", key).Scan(&encrypted)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("secret not found: %s", key)
	}
	if err != nil {
		return nil, fmt.Errorf("query secret: %w", err)
	}
	return s.decrypt(encrypted)
}

// PutSecret encrypts and stores a secret.
func (s *DefaultSecretStoreProvider) PutSecret(ctx context.Context, key string, value []byte) error {
	encrypted, err := s.encrypt(value)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO payment_secrets (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = CURRENT_TIMESTAMP
	`, key, encrypted)
	return err
}

// DeleteSecret removes a secret by key.
func (s *DefaultSecretStoreProvider) DeleteSecret(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM payment_secrets WHERE key = $1", key)
	return err
}

// ListSecrets returns all keys matching the given prefix.
func (s *DefaultSecretStoreProvider) ListSecrets(ctx context.Context, prefix string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT key FROM payment_secrets WHERE key LIKE $1", prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *DefaultSecretStoreProvider) encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	// Prepend nonce to ciphertext
	return s.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (s *DefaultSecretStoreProvider) decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := s.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return s.gcm.Open(nil, nonce, ciphertext, nil)
}

var _ payments.SecretStoreProvider = (*DefaultSecretStoreProvider)(nil)
