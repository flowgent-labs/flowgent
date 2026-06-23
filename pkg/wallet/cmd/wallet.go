// Package cmd provides the CLI-facing functions for the wallet service.
// It delegates to walletmanager.WalletManager for the actual service logic.
package cmd

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/flowgent-labs/flowgent/wallet/pkg/walletmanager"
)

// RunWallet creates a WalletManager and starts the wallet service.
func RunWallet(action, listen, db, cfgPath string) error {
	switch action {
	case "start":
		wm, err := walletmanager.New(cfgPath, listen, db)
		if err != nil {
			return fmt.Errorf("create wallet manager: %w", err)
		}
		return wm.Start(context.Background())
	case "stop":
		return fmt.Errorf("stop: send SIGTERM to the wallet process")
	case "restart":
		return fmt.Errorf("restart: not supported — stop the process and start a new one")
	default:
		return fmt.Errorf("unknown wallet action: %s", action)
	}
}

// RunWalletGenKey generates a new Ed25519 keypair.
func RunWalletGenKey(format, encoding string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("key generation failed: %w", err)
	}

	pubStr := encodeKey(pub, encoding)
	privStr := encodeKey(priv, encoding)

	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]string{
			"public_key":  pubStr,
			"private_key": privStr,
		})
	default:
		fmt.Printf("Public key:  %s\n", pubStr)
		fmt.Printf("Private key: %s\n", privStr)
		return nil
	}
}

func encodeKey(key []byte, enc string) string {
	switch enc {
	case "base64":
		return base64.StdEncoding.EncodeToString(key)
	default:
		return hex.EncodeToString(key)
	}
}
