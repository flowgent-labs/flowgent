package payments

import "context"

// SecretStoreProvider is a pluggable secret store backend.
// Implementations include the default AES-256-GCM encrypted DB store
// and Hashicorp Vault.
type SecretStoreProvider interface {
	GetSecret(ctx context.Context, key string) ([]byte, error)
	PutSecret(ctx context.Context, key string, value []byte) error
	DeleteSecret(ctx context.Context, key string) error
	ListSecrets(ctx context.Context, prefix string) ([]string, error)
}
