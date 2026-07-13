package console

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"
)

// HasSecretStore returns true if the wallet secret store is available.
func (fc *FlowgentConsole) HasSecretStore() bool {
	return fc.secretStore != nil
}

// CreateWalletKey generates or imports an Ed25519 keypair for the given wallet name.
// If hexPrivateKey is provided it imports; otherwise it generates a new keypair.
// Returns the address (public key as 0x-prefixed hex).
func (fc *FlowgentConsole) CreateWalletKey(name, hexPrivateKey string) (string, error) {
	if fc.secretStore == nil {
		return "", fmt.Errorf("wallet secret store not available")
	}
	var privKey ed25519.PrivateKey
	if hexPrivateKey != "" {
		keyBytes, err := hex.DecodeString(hexPrivateKey)
		if err != nil {
			return "", fmt.Errorf("invalid hex private key: %w", err)
		}
		if len(keyBytes) != ed25519.PrivateKeySize {
			return "", fmt.Errorf("invalid key length: got %d, want %d", len(keyBytes), ed25519.PrivateKeySize)
		}
		privKey = ed25519.PrivateKey(keyBytes)
	} else {
		_, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			return "", fmt.Errorf("generating key: %w", err)
		}
		privKey = priv
	}
	if err := fc.secretStore.PutSecret(fc.ctx, "wallet:"+name, []byte(privKey)); err != nil {
		return "", fmt.Errorf("storing key: %w", err)
	}
	pubKey := privKey.Public().(ed25519.PublicKey)
	return "0x" + hex.EncodeToString(pubKey), nil
}

// GetWalletKey returns the address for a named wallet.
func (fc *FlowgentConsole) GetWalletKey(name string) (string, error) {
	if fc.secretStore == nil {
		return "", fmt.Errorf("wallet secret store not available")
	}
	keyBytes, err := fc.secretStore.GetSecret(fc.ctx, "wallet:"+name)
	if err != nil {
		return "", err
	}
	privKey := ed25519.PrivateKey(keyBytes)
	pubKey := privKey.Public().(ed25519.PublicKey)
	return "0x" + hex.EncodeToString(pubKey), nil
}

// ListWalletKeys returns all wallet names.
func (fc *FlowgentConsole) ListWalletKeys() ([]string, error) {
	if fc.secretStore == nil {
		return nil, fmt.Errorf("wallet secret store not available")
	}
	keys, err := fc.secretStore.ListSecrets(fc.ctx, "wallet:")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, k := range keys {
		names = append(names, strings.TrimPrefix(k, "wallet:"))
	}
	return names, nil
}

// DeleteWalletKey deletes a named wallet key.
func (fc *FlowgentConsole) DeleteWalletKey(name string) error {
	if fc.secretStore == nil {
		return fmt.Errorf("wallet secret store not available")
	}
	return fc.secretStore.DeleteSecret(fc.ctx, "wallet:"+name)
}
