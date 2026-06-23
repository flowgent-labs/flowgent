// Package providers implements secret store providers for wallet key management.
package providers

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flowgent-labs/flowgent/wallet/pkg"
)

// CSISecretStoreProvider reads private keys from CSI-mounted credential files.
//
// The Kubernetes CSI Secret Driver (for HashiCorp Vault, GCP Secret Manager,
// or AWS Secrets Manager) mounts secrets as files in a pod volume. This provider
// reads those files directly — no network round-trip, no key stored in a database.
//
// File layout expected:
//
//	{basePath}/wallet/secret/.credentials
//
// Format (one per line):
//
//	WALLET_KEY_<name>=<hex-encoded-ed25519-private-key>
//
//	Example:
//	WALLET_KEY_default=8b12a3c4...
//	WALLET_KEY_payments=7f91e2d3...
//
// Private keys never leave the wallet pod. They are read once at startup and
// held in memory for the lifetime of the process. This provider is read-only —
// key creation and rotation are managed externally via the secret manager.
type CSISecretStoreProvider struct {
	keys map[string][]byte // wallet name → raw ed25519 private key bytes
}

// NewCSISecretStoreProvider reads wallet private keys from a CSI-mounted credentials file.
// basePath is typically /var/flowgent.
func NewCSISecretStoreProvider(basePath string) (*CSISecretStoreProvider, error) {
	if basePath == "" {
		basePath = "/var/flowgent"
	}
	credFile := filepath.Join(basePath, "wallet", "secret", ".credentials")

	keys, err := loadWalletKeys(credFile)
	if err != nil {
		return nil, fmt.Errorf("csi provider: %w", err)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("csi provider: no wallet keys found in %s — ensure WALLET_KEY_<name>=<hex-key> entries exist", credFile)
	}

	return &CSISecretStoreProvider{keys: keys}, nil
}

func loadWalletKeys(path string) (map[string][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	keys := make(map[string][]byte)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if !strings.HasPrefix(k, "WALLET_KEY_") {
			continue
		}
		name := strings.TrimPrefix(k, "WALLET_KEY_")
		if name == "" {
			continue
		}
		raw, err := hex.DecodeString(v)
		if err != nil {
			return nil, fmt.Errorf("invalid hex key for %s: %w", k, err)
		}
		keys[name] = raw
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return keys, nil
}

// GetSecret returns the raw private key bytes for the given wallet.
// The key format is "wallet:<name>" (e.g., "wallet:default").
func (p *CSISecretStoreProvider) GetSecret(_ context.Context, key string) ([]byte, error) {
	name := strings.TrimPrefix(key, "wallet:")
	raw, ok := p.keys[name]
	if !ok {
		return nil, fmt.Errorf("wallet not found: %s", name)
	}
	return raw, nil
}

// PutSecret is not supported — keys are managed externally by the secret manager.
func (p *CSISecretStoreProvider) PutSecret(_ context.Context, _ string, _ []byte) error {
	return errors.New("csi provider: PutSecret not supported — keys are managed externally via secret manager (Vault/GCP/AWS)")
}

// DeleteSecret is not supported — keys are managed externally by the secret manager.
func (p *CSISecretStoreProvider) DeleteSecret(_ context.Context, _ string) error {
	return errors.New("csi provider: DeleteSecret not supported — keys are managed externally via secret manager (Vault/GCP/AWS)")
}

// ListSecrets returns all wallet names known to this provider.
func (p *CSISecretStoreProvider) ListSecrets(_ context.Context, prefix string) ([]string, error) {
	var names []string
	for k := range p.keys {
		fullKey := "wallet:" + k
		if strings.HasPrefix(fullKey, prefix) {
			names = append(names, fullKey)
		}
	}
	if names == nil {
		names = []string{}
	}
	return names, nil
}

var _ payments.SecretStoreProvider = (*CSISecretStoreProvider)(nil)
