// Flowgent Wallet — standalone key management and payment signing daemon.
//
// The wallet service runs in its own container with minimal dependencies
// (config, common, model, messager only). It communicates with the rest of
// the Flowgent system via MQTT for async signing requests and exposes an
// HTTP API for key management.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/flowgent-labs/flowgent/wallet/pkg"
)

var (
	cfgPath string
)

func init() {
	defaultCfg := ""
	if v := os.Getenv("FLOWGENT__CONFIG__FILE"); v != "" {
		defaultCfg = v
	}
	rootCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c",
		defaultCfg, "Path to config file (required if FLOWGENT__CONFIG__FILE not set)")
}

var (
	walletListen string
	walletDB     string
	keyFormat    string
	keyEncoding  string
)

var rootCmd = &cobra.Command{
	Use:   "flowgent-wallet",
	Short: "Flowgent Wallet — x402 payment key management and signing daemon",
	Long: `Standalone wallet service for Ed25519 key management and payment signing.
Keys are encrypted at rest with AES-256-GCM (default), CSI-injected (K8s),
or stored in HashiCorp Vault.`,
	SilenceErrors: true,
	SilenceUsage:  true,
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the wallet daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWallet("start", walletListen, walletDB, cfgPath)
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the wallet daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWallet("stop", "", "", "")
	},
}

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the wallet daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWallet("restart", walletListen, walletDB, cfgPath)
	},
}

var genKeyCmd = &cobra.Command{
	Use:   "generate-key",
	Short: "Generate a new Ed25519 wallet keypair",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWalletGenKey(keyFormat, keyEncoding)
	},
}

func main() {
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(genKeyCmd)

	startCmd.Flags().StringVar(&walletListen, "listen", "127.0.0.1:9901", "Listen address")
	startCmd.Flags().StringVar(&walletDB, "db", "", "SQLite database path")
	genKeyCmd.Flags().StringVar(&keyFormat, "format", "text", "Output structure: text|json")
	genKeyCmd.Flags().StringVar(&keyEncoding, "encoding", "hex", "Key encoding: hex|base64")

	cobra.EnableCommandSorting = false

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runWallet creates a FlowgentWalletManager and starts the wallet service.
func runWallet(action, listen, db, cfgPath string) error {
	switch action {
	case "start":
		wm, err := payments.NewFlowgentWalletManager(cfgPath, listen, db)
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

// runWalletGenKey generates a new Ed25519 keypair.
func runWalletGenKey(format, encoding string) error {
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
