// Package providers implements secret store providers for wallet key management.
package providers

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// VaultSecretStoreProvider retrieves wallet private keys from HashiCorp Vault
// using the KV v2 secrets engine. Private keys are fetched on demand and are
// never persisted to local storage.
//
// The provider supports token-based auth (via X-Vault-Token header or token
// file) and Kubernetes auth (planned). The secret path convention is:
//
//	{secretPath}/wallet/{name}  with key "private_key" containing hex-encoded ed25519 key
type VaultSecretStoreProvider struct {
	address    string
	token      string
	mountPath  string
	secretPath string
	role       string
	client     *http.Client
}

// NewVaultSecretStoreProvider creates a new Vault-backed secret store provider.
// Token is resolved from the direct value, VAULT_TOKEN env var, or token_file.
func NewVaultSecretStoreProvider(address, token, tokenFile, mountPath, secretPath, role string) (*VaultSecretStoreProvider, error) {
	if address == "" {
		return nil, fmt.Errorf("vault address is required")
	}
	if token == "" {
		token = os.Getenv("VAULT_TOKEN")
	}
	if token == "" && tokenFile != "" {
		data, err := os.ReadFile(tokenFile)
		if err != nil {
			return nil, fmt.Errorf("read vault token file %s: %w", tokenFile, err)
		}
		token = strings.TrimSpace(string(data))
	}
	if token == "" {
		return nil, fmt.Errorf("vault token is required: set address/token or VAULT_TOKEN env")
	}
	if mountPath == "" {
		mountPath = "secret"
	}
	if secretPath == "" {
		secretPath = "flowgent"
	}

	return &VaultSecretStoreProvider{
		address:    strings.TrimRight(address, "/"),
		token:      token,
		mountPath:  mountPath,
		secretPath: secretPath,
		role:       role,
		client:     &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// vaultKVResponse is the Vault KV v2 read response envelope.
type vaultKVResponse struct {
	Data struct {
		Data map[string]string `json:"data"`
	} `json:"data"`
	Errors []string `json:"errors"`
}

// GetSecret retrieves a wallet private key from Vault KV v2.
// The key parameter is "wallet:<name>". The provider reads:
//
//	GET {address}/v1/{mountPath}/data/{secretPath}/wallet/{name}
func (p *VaultSecretStoreProvider) GetSecret(ctx context.Context, key string) ([]byte, error) {
	name := strings.TrimPrefix(key, "wallet:")
	path := fmt.Sprintf("%s/v1/%s/data/%s/wallet/%s", p.address, p.mountPath, p.secretPath, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("vault: build request: %w", err)
	}
	req.Header.Set("X-Vault-Token", p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("wallet not found in vault: %s (path: %s)", name, path)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vault: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var vr vaultKVResponse
	if err := json.Unmarshal(body, &vr); err != nil {
		return nil, fmt.Errorf("vault: parse response: %w", err)
	}
	if len(vr.Errors) > 0 {
		return nil, fmt.Errorf("vault: %s", strings.Join(vr.Errors, "; "))
	}

	hexKey, ok := vr.Data.Data["private_key"]
	if !ok {
		return nil, fmt.Errorf("vault: no 'private_key' field at %s", path)
	}

	raw, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("vault: invalid hex private_key: %w", err)
	}

	return raw, nil
}

// PutSecret stores a wallet private key in Vault KV v2.
func (p *VaultSecretStoreProvider) PutSecret(ctx context.Context, key string, value []byte) error {
	name := strings.TrimPrefix(key, "wallet:")
	path := fmt.Sprintf("%s/v1/%s/data/%s/wallet/%s", p.address, p.mountPath, p.secretPath, name)

	payload := map[string]interface{}{
		"data": map[string]string{
			"private_key": hex.EncodeToString(value),
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("vault: build request: %w", err)
	}
	req.Header.Set("X-Vault-Token", p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("vault: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vault: write failed status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// DeleteSecret removes a wallet key from Vault.
func (p *VaultSecretStoreProvider) DeleteSecret(ctx context.Context, key string) error {
	name := strings.TrimPrefix(key, "wallet:")
	path := fmt.Sprintf("%s/v1/%s/data/%s/wallet/%s", p.address, p.mountPath, p.secretPath, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("vault: build request: %w", err)
	}
	req.Header.Set("X-Vault-Token", p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("vault: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vault: delete failed status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ListSecrets lists wallet keys matching a prefix from Vault.
func (p *VaultSecretStoreProvider) ListSecrets(ctx context.Context, prefix string) ([]string, error) {
	path := fmt.Sprintf("%s/v1/%s/metadata/%s/wallet", p.address, p.mountPath, p.secretPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("vault: build request: %w", err)
	}
	req.Header.Set("X-Vault-Token", p.token)

	if prefix != "" {
		q := req.URL.Query()
		q.Set("list", "true")
		req.URL.RawQuery = q.Encode()
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return []string{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vault: list failed status %d: %s", resp.StatusCode, string(body))
	}

	var vr struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		return nil, fmt.Errorf("vault: parse list response: %w", err)
	}

	var names []string
	for _, k := range vr.Data.Keys {
		fullKey := "wallet:" + strings.TrimSuffix(k, "/")
		if strings.HasPrefix(fullKey, prefix) {
			names = append(names, fullKey)
		}
	}
	if names == nil {
		names = []string{}
	}
	return names, nil
}

// Address returns the configured Vault address.
func (p *VaultSecretStoreProvider) Address() string {
	return p.address
}

