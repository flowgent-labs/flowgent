// Flowgent — Autonomous Agentflow Orchestration Engine
//
// Unified CLI entry point with GNU-style multi-level argument parsing via cobra.
// Subcommands: daemon (start/stop/restart), apiserver, a2a, wallet, console.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	GitCommit = "none"
	BuildTime = "unknown"
)

// ─── Global flags ─────────────────────────────────────────────

var (
	cfgPath string
	verbose bool
	pidFile string
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c",
		defaultConfigPath(), "Path to config file")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v",
		false, "Enable debug logging")
}

func defaultConfigPath() string {
	if v := os.Getenv("FLOWGENT_CONFIG_FILE"); v != "" {
		return v
	}
	return "etc/flowgent.yaml"
}

// ─── Root command ─────────────────────────────────────────────

var rootCmd = &cobra.Command{
	Use:   "flowgent",
	Short: "Autonomous Agentflow Orchestration Engine",
	Long: `Flowgent is an AI-native orchestration runtime that combines LLM agent
intelligence with deterministic DAG execution for predictable, reliable,
and auditable autonomous workflows.`,
	Version:      fmt.Sprintf("%s (commit: %s, built: %s)", Version, GitCommit, BuildTime),
	SilenceErrors: true,
	SilenceUsage:  true,
}

// ─── daemon ───────────────────────────────────────────────────

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start, stop, or restart all components",
	Long: `Manage the Flowgent daemon process (REST API + A2A + cron + worker).

The daemon writes a PID file on start and removes it on stop. Signals
(SIGINT/SIGTERM) trigger graceful shutdown with configurable timeout.`,
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Flowgent daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDaemon("start")
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop a running Flowgent daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDaemon("stop")
	},
}

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Stop then start the Flowgent daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDaemon("restart")
	},
}

// ─── apiserver ────────────────────────────────────────────────

var apiserverCmd = &cobra.Command{
	Use:   "apiserver",
	Short: "Start the REST API server only",
	Long:  "Start only the REST API server on the configured port (management UI backend).",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAPIServer()
	},
}

// ─── a2a ──────────────────────────────────────────────────────

var a2aCmd = &cobra.Command{
	Use:   "a2a",
	Short: "Start the A2A protocol server only",
	Long: `Start only the Google Agent-to-Agent (A2A) protocol server.

External AI systems can discover agentflows via /.well-known/agent.json
and trigger runs programmatically.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runA2AServer()
	},
}

// ─── wallet ───────────────────────────────────────────────────

var (
	walletListen string
	walletDB     string
	masterKey    string
	masterKeyFile string
	generateKey  bool
)

var walletCmd = &cobra.Command{
	Use:   "wallet",
	Short: "Start the wallet key-management daemon",
	Long: `Standalone daemon for secure key management and payment signing.

The wallet daemon is the ONLY process with access to raw private keys.
Keys are encrypted at rest with AES-256-GCM.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWallet()
	},
}

// ─── console ──────────────────────────────────────────────────

var consoleCmd = &cobra.Command{
	Use:   "console",
	Short: "Interactive management console",
	Long: `Open an interactive REPL for querying the Flowgent store.

Commands:
  list agentflows          List all agentflow definitions
  list runs [agentflow-id] List recent runs
  show run <id>            Show details of a specific run
  tasks <run-id>           List task runs for an agentflow run
  show task <id>           Show details of a specific task
  help                     Show available commands
  exit, quit               Exit the console`,
	RunE: func(cmd *cobra.Command, args []string) error {
		startConsole()
		return nil
	},
}

// ─── version ──────────────────────────────────────────────────

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Flowgent v%s (commit: %s, built: %s)\n", Version, GitCommit, BuildTime)
	},
}

// ─── main ─────────────────────────────────────────────────────

func main() {
	rootCmd.AddCommand(daemonCmd)
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonRestartCmd)
	rootCmd.AddCommand(apiserverCmd)
	rootCmd.AddCommand(a2aCmd)
	rootCmd.AddCommand(walletCmd)
	rootCmd.AddCommand(consoleCmd)
	rootCmd.AddCommand(versionCmd)

	// daemon flags
	daemonCmd.PersistentFlags().StringVar(&pidFile, "pid-file", "/tmp/flowgent.pid", "PID file path")

	// wallet flags
	walletCmd.Flags().StringVar(&walletListen, "listen", "127.0.0.1:9901", "Listen address")
	walletCmd.Flags().StringVar(&walletDB, "db", "", "SQLite database path (default: $HOME/.flowgent/wallet.db)")
	walletCmd.Flags().StringVar(&masterKey, "master-key", "", "Master encryption key")
	walletCmd.Flags().StringVar(&masterKeyFile, "master-key-file", "", "Path to master key file")
	walletCmd.Flags().BoolVar(&generateKey, "generate-key", false, "Generate a new wallet keypair and exit")

	cobra.EnableCommandSorting = false

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		// Show help for the failing command on usage errors
		if strings.Contains(err.Error(), "unknown command") ||
			strings.Contains(err.Error(), "required flag") ||
			strings.Contains(err.Error(), "unknown flag") {
			fmt.Fprintf(os.Stderr, "Run 'flowgent --help' for usage.\n")
		}
		os.Exit(1)
	}
}
