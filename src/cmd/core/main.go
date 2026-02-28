// Flowgent — Autonomous Agentflow Orchestration Engine
//
// Unified CLI entry point with GNU-style multi-level argument parsing via cobra.
// Every daemon-like subcommand supports start/stop/restart.
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
	Version:       fmt.Sprintf("%s (commit: %s, built: %s)", Version, GitCommit, BuildTime),
	SilenceErrors: true,
	SilenceUsage:  true,
}

// ─── Shared PID file flags (per service) ──────────────────────

var (
	pidDaemon      string
	pidAPIServer   string
	pidA2A         string
	pidWallet      string
	pidTaskManager string
	pidJobManager  string
)

// ─── daemon ───────────────────────────────────────────────────

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start, stop, or restart all components (REST + A2A + cron + worker)",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Flowgent daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDaemon("start", pidDaemon)
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop a running Flowgent daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDaemon("stop", pidDaemon)
	},
}

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Stop then start the Flowgent daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDaemon("restart", pidDaemon)
	},
}

// ─── apiserver ────────────────────────────────────────────────

var apiserverCmd = &cobra.Command{
	Use:   "apiserver",
	Short: "Manage the REST API server",
	Long:  "Start, stop, or restart the REST API server independently (management UI backend).",
}

var apiserverStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the REST API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAPIServer("start", pidAPIServer)
	},
}

var apiserverStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop a running REST API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAPIServer("stop", pidAPIServer)
	},
}

var apiserverRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Stop then start the REST API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAPIServer("restart", pidAPIServer)
	},
}

// ─── a2a ──────────────────────────────────────────────────────

var a2aCmd = &cobra.Command{
	Use:   "a2a",
	Short: "Manage the A2A protocol server",
}

var a2aStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the A2A protocol server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runA2AServer("start", pidA2A)
	},
}

var a2aStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop a running A2A server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runA2AServer("stop", pidA2A)
	},
}

var a2aRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Stop then start the A2A server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runA2AServer("restart", pidA2A)
	},
}

// ─── wallet ───────────────────────────────────────────────────

var (
	walletListen    string
	walletDB        string
	masterKey       string
	masterKeyFile   string
	keyFormat       string
	keyEncoding     string
)

var walletCmd = &cobra.Command{
	Use:   "wallet",
	Short: "Manage the wallet key-management daemon",
	Long: `Secure key management and payment signing daemon.

The wallet daemon is the ONLY process with access to raw private keys.
Keys are encrypted at rest with AES-256-GCM.`,
}

var walletStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the wallet daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWallet("start")
	},
}

var walletStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop a running wallet daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWallet("stop")
	},
}

var walletRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Stop then start the wallet daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWallet("restart")
	},
}

var walletGenKeyCmd = &cobra.Command{
	Use:   "generate-key",
	Short: "Generate a new Ed25519 wallet keypair",
	Long: `Generate a new Ed25519 keypair for wallet signing.

--format controls output structure:
  text   Human-readable labels (default)
  json   Machine-parseable key-value pairs

--encoding controls key representation:
  hex    Hexadecimal (default)
  base64 Base64-encoded raw bytes`,
	Example: `  flowgent wallet generate-key
  flowgent wallet generate-key --format json
  flowgent wallet generate-key --encoding base64
  flowgent wallet generate-key --format json --encoding base64`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWalletGenKey(keyFormat, keyEncoding)
	},
}

// ─── taskmanager ──────────────────────────────────────────────

var taskmanagerCmd = &cobra.Command{
	Use:   "taskmanager",
	Short: "Manage a TaskManager worker (distributed mode)",
	Long:  "Start, stop, or restart a persistent TaskManager that consumes ExecutionPlans from MQTT.",
}

var taskmanagerStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the TaskManager worker",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTaskManager("start", pidTaskManager)
	},
}

var taskmanagerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop a running TaskManager",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTaskManager("stop", pidTaskManager)
	},
}

var taskmanagerRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the TaskManager",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTaskManager("restart", pidTaskManager)
	},
}

// ─── jobmanager ───────────────────────────────────────────────

var jobmanagerCmd = &cobra.Command{
	Use:   "jobmanager",
	Short: "Manage a standalone JobManager (distributed control plane)",
	Long:  "Start, stop, or restart a standalone JobManager for K8s distributed mode.",
}

var jobmanagerStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the JobManager",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runJobManager("start", pidJobManager)
	},
}

var jobmanagerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop a running JobManager",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runJobManager("stop", pidJobManager)
	},
}

var jobmanagerRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the JobManager",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runJobManager("restart", pidJobManager)
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
	// daemon
	rootCmd.AddCommand(daemonCmd)
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonRestartCmd)

	// apiserver
	rootCmd.AddCommand(apiserverCmd)
	apiserverCmd.AddCommand(apiserverStartCmd)
	apiserverCmd.AddCommand(apiserverStopCmd)
	apiserverCmd.AddCommand(apiserverRestartCmd)

	// a2a
	rootCmd.AddCommand(a2aCmd)
	a2aCmd.AddCommand(a2aStartCmd)
	a2aCmd.AddCommand(a2aStopCmd)
	a2aCmd.AddCommand(a2aRestartCmd)

	// wallet
	rootCmd.AddCommand(walletCmd)
	walletCmd.AddCommand(walletStartCmd)
	walletCmd.AddCommand(walletStopCmd)
	walletCmd.AddCommand(walletRestartCmd)
	walletCmd.AddCommand(walletGenKeyCmd)
	walletStartCmd.Flags().StringVar(&walletListen, "listen", "127.0.0.1:9901", "Listen address")
	walletStartCmd.Flags().StringVar(&walletDB, "db", "", "SQLite database path (default: $HOME/.flowgent/wallet.db)")
	walletStartCmd.Flags().StringVar(&masterKey, "master-key", "", "Master encryption key")
	walletStartCmd.Flags().StringVar(&masterKeyFile, "master-key-file", "", "Path to master key file")
	walletGenKeyCmd.Flags().StringVar(&keyFormat, "format", "text", "Output structure: text|json")
	walletGenKeyCmd.Flags().StringVar(&keyEncoding, "encoding", "hex", "Key encoding: hex|base64")

	// taskmanager
	rootCmd.AddCommand(taskmanagerCmd)
	taskmanagerCmd.AddCommand(taskmanagerStartCmd)
	taskmanagerCmd.AddCommand(taskmanagerStopCmd)
	taskmanagerCmd.AddCommand(taskmanagerRestartCmd)

	// jobmanager
	rootCmd.AddCommand(jobmanagerCmd)
	jobmanagerCmd.AddCommand(jobmanagerStartCmd)
	jobmanagerCmd.AddCommand(jobmanagerStopCmd)
	jobmanagerCmd.AddCommand(jobmanagerRestartCmd)

	// console
	rootCmd.AddCommand(consoleCmd)

	// version
	rootCmd.AddCommand(versionCmd)

	cobra.EnableCommandSorting = false

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		if strings.Contains(err.Error(), "unknown command") ||
			strings.Contains(err.Error(), "required flag") ||
			strings.Contains(err.Error(), "unknown flag") {
			fmt.Fprintf(os.Stderr, "Run 'flowgent --help' for usage.\n")
		}
		os.Exit(1)
	}
}
