package providers

import (
	"testing"

	"github.com/flowgent-labs/flowgent/wallet/pkg"
)

func TestVaultProvider_New(t *testing.T) {
	p, err := NewVaultSecretStoreProvider("https://vault.example.com:8200", "s.token", "", "secret", "wallet", "flowgent")
	if err != nil {
		t.Fatalf("NewVaultSecretStoreProvider: %v", err)
	}
	if p.Address() != "https://vault.example.com:8200" {
		t.Errorf("expected vault address, got %s", p.Address())
	}
}

func TestVaultProvider_NewMissingAddress(t *testing.T) {
	_, err := NewVaultSecretStoreProvider("", "token", "", "", "", "")
	if err == nil {
		t.Fatal("expected error for missing address")
	}
}

func TestVaultProvider_NewMissingToken(t *testing.T) {
	_, err := NewVaultSecretStoreProvider("https://vault:8200", "", "", "", "", "")
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestVaultProvider_ImplementsInterface(t *testing.T) {
	var _ payments.SecretStoreProvider = (*VaultSecretStoreProvider)(nil)
}

func TestVaultProvider_NotConnected(t *testing.T) {
	p, _ := NewVaultSecretStoreProvider("https://vault:8200", "tok", "", "", "", "")
	_, err := p.GetSecret(nil, "any")
	if err == nil {
		t.Fatal("expected not-connected error from GetSecret")
	}
	_, err = p.ListSecrets(nil, "")
	if err == nil {
		t.Fatal("expected not-connected error from ListSecrets")
	}
}
