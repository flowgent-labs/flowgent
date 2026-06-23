// Flowgent Wallet — standalone key management and payment signing daemon.
//
// The wallet service runs in its own container with minimal dependencies
// (config, common, model, messager only). It communicates with the rest of
// the Flowgent system via MQTT for async signing requests and exposes an
// HTTP API for key management.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	walletcmd "github.com/flowgent-labs/flowgent/wallet/cmd"
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
		return walletcmd.RunWallet("start", walletListen, walletDB, cfgPath)
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the wallet daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return walletcmd.RunWallet("stop", "", "", "")
	},
}

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the wallet daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return walletcmd.RunWallet("restart", walletListen, walletDB, cfgPath)
	},
}

var genKeyCmd = &cobra.Command{
	Use:   "generate-key",
	Short: "Generate a new Ed25519 wallet keypair",
	RunE: func(cmd *cobra.Command, args []string) error {
		return walletcmd.RunWalletGenKey(keyFormat, keyEncoding)
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
