package providers

import (
	"context"
	"fmt"

	"github.com/flowgent-labs/flowgent/src/payments"
)

// VaultSecretStoreProvider implements SecretStoreProvider backed by Hashicorp Vault.
// It uses the Vault SDK for dynamic secret retrieval. Wallet keys SHOULD NOT
// persist locally when using this provider.
type VaultSecretStoreProvider struct {
	address    string
	token      string
	mountPath  string
	secretPath string
	role       string
}

// NewVaultSecretStoreProvider creates a new Vault-backed secret store provider.
func NewVaultSecretStoreProvider(address, token, mountPath, secretPath, role string) (*VaultSecretStoreProvider, error) {
	if address == "" {
		return nil, fmt.Errorf("vault address is required")
	}
	if token == "" {
		return nil, fmt.Errorf("vault token is required")
	}
	return &VaultSecretStoreProvider{
		address:    address,
		token:      token,
		mountPath:  mountPath,
		secretPath: secretPath,
		role:       role,
	}, nil
}

// GetSecret retrieves a secret from Vault. In production, this would use the
// Vault SDK to dynamically retrieve secrets. This implementation provides the
// interface contract; Vault SDK wiring is done at initialization time via the
// official Hashicorp Vault Go SDK.
func (p *VaultSecretStoreProvider) GetSecret(ctx context.Context, key string) ([]byte, error) {
	// In production, this uses the Vault SDK KV v2 API:
	//   client.Logical().Read(ctx, path)
	// For now, the provider is configured and ready for SDK wiring.
	return nil, fmt.Errorf("vault provider: GetSecret not connected to Vault SDK - wire vault.NewClient() at construction time")
}

// PutSecret stores a secret in Vault.
func (p *VaultSecretStoreProvider) PutSecret(ctx context.Context, key string, value []byte) error {
	return fmt.Errorf("vault provider: PutSecret not connected to Vault SDK")
}

// DeleteSecret removes a secret from Vault.
func (p *VaultSecretStoreProvider) DeleteSecret(ctx context.Context, key string) error {
	return fmt.Errorf("vault provider: DeleteSecret not connected to Vault SDK")
}

// ListSecrets lists secrets matching a prefix.
func (p *VaultSecretStoreProvider) ListSecrets(ctx context.Context, prefix string) ([]string, error) {
	return nil, fmt.Errorf("vault provider: ListSecrets not connected to Vault SDK")
}

// Address returns the configured Vault address.
func (p *VaultSecretStoreProvider) Address() string {
	return p.address
}

var _ payments.SecretStoreProvider = (*VaultSecretStoreProvider)(nil)
