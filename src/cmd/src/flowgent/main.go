// Flowgent — Autonomous Agentflow Orchestration Engine
//
// Unified CLI entry point. Every daemon-like subcommand supports start/stop/restart.
// Subcommand order follows the logical execution flow:
//
//	all-in-one → apiserver → a2a → controller → jobmanager → taskmanager
//	→ sandbox → notifier → wallet → console → version
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
	defaultCfg := ""
	if v := os.Getenv("FLOWGENT_CONFIG_FILE"); v != "" {
		defaultCfg = v
	}
	rootCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c",
		defaultCfg, "Path to config file (required if FLOWGENT_CONFIG_FILE not set)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v",
		false, "Enable debug logging")
}

// ─── Root command ─────────────────────────────────────────────

var rootCmd = &cobra.Command{
	Use:   "flowgent",
	Short: "AI-native orchestration engine — deterministic DAG + autonomous agents",
	Long: `Flowgent combines LLM agent intelligence with deterministic DAG execution
for predictable, auditable, distributed enterprise workflows.`,
	Version:       fmt.Sprintf("%s (commit: %s, built: %s)", Version, GitCommit, BuildTime),
	SilenceErrors: true,
	SilenceUsage:  true,
}

// ─── Shared PID file flags ────────────────────────────────────

var (
	pidAllInOne    string
	pidAPIServer   string
	pidA2A         string
	pidController  string
	pidJobManager  string
	jmFlowID       string
	pidTaskManager string
	pidSandbox     string
	pidNotifier    string
	pidWallet      string
)

// ═══════════════════════════════════════════════════════════════
// 1. all-in-one — single process (dev/local)
// ═══════════════════════════════════════════════════════════════

var allInOneCmd = &cobra.Command{
	Use:   "all-in-one",
	Short: "Run all components in a single process (dev/local)",
	Long:  "Start, stop, or restart the entire Flowgent stack in one process. Uses SQLite + memory queue.",
}

var allInOneStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start all-in-one mode",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAllInOne("start", pidAllInOne)
	},
}

var allInOneStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop all-in-one mode",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAllInOne("stop", pidAllInOne)
	},
}

var allInOneRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart all-in-one mode",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAllInOne("restart", pidAllInOne)
	},
}

// ═══════════════════════════════════════════════════════════════
// 2. apiserver — multi-tenant REST + A2A gateway
// ═══════════════════════════════════════════════════════════════

var apiserverCmd = &cobra.Command{
	Use:   "apiserver",
	Short: "Multi-tenant REST API + A2A gateway",
	Long:  "Start, stop, or restart the API server (REST :9999, A2A :9992, mgmt :9991).",
}

var apiserverStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAPIServer("start", pidAPIServer)
	},
}

var apiserverStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAPIServer("stop", pidAPIServer)
	},
}

var apiserverRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAPIServer("restart", pidAPIServer)
	},
}

// ═══════════════════════════════════════════════════════════════
// 3. a2a — Google Agent-to-Agent protocol
// ═══════════════════════════════════════════════════════════════

var a2aCmd = &cobra.Command{
	Use:   "a2a",
	Short: "Google A2A protocol server",
	Long:  "Start, stop, or restart the A2A server for inter-agent interoperability.",
}

var a2aStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the A2A server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runA2AServer("start", pidA2A)
	},
}

var a2aStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the A2A server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runA2AServer("stop", pidA2A)
	},
}

var a2aRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the A2A server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runA2AServer("restart", pidA2A)
	},
}

// ═══════════════════════════════════════════════════════════════
// 4. controller — distributed sharded flow driver
// ═══════════════════════════════════════════════════════════════

var controllerCmd = &cobra.Command{
	Use:   "controller",
	Short: "Distributed sharded flow driver",
	Long: `Start, stop, or restart a Controller that polls agentflow definitions from
PostgreSQL and drives execution via hash-mod sharding across N pods.

Session mode: inserts PENDING runs for the shared JM pool.
Application mode: creates dedicated K8s JM Deployment + PENDING run.`,
}

var controllerStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Controller",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runController("start", pidController)
	},
}

var controllerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Controller",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runController("stop", pidController)
	},
}

var controllerRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the Controller",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runController("restart", pidController)
	},
}

// ═══════════════════════════════════════════════════════════════
// 5. jobmanager — control plane (DAG → ExecutionPlans)
// ═══════════════════════════════════════════════════════════════

var jobmanagerCmd = &cobra.Command{
	Use:   "jobmanager",
	Short: "Control plane — parses flows, builds DAGs, dispatches plans",
	Long:  "Start, stop, or restart a standalone JobManager for K8s distributed mode.",
}

var jobmanagerStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the JobManager",
	RunE: func(cmd *cobra.Command, args []string) error {
		// --flow-id sets the dedicated flow for application mode.
		// When set, the JM skips tenant-wide scanning and only handles this flow.
		if jmFlowID != "" {
			os.Setenv("FLOWGENT_AGENTFLOW_ID", jmFlowID)
		}
		return runJobManager("start", pidJobManager)
	},
}

var jobmanagerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the JobManager",
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

// ═══════════════════════════════════════════════════════════════
// 6. taskmanager — persistent worker (executes ExecutionPlans)
// ═══════════════════════════════════════════════════════════════

var taskmanagerCmd = &cobra.Command{
	Use:   "taskmanager",
	Short: "Persistent worker — executes ExecutionPlans via slot pool",
	Long:  "Start, stop, or restart a TaskManager that consumes ExecutionPlans from MQTT.",
}

var taskmanagerStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the TaskManager",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTaskManager("start", pidTaskManager)
	},
}

var taskmanagerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the TaskManager",
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

// ═══════════════════════════════════════════════════════════════
// 7. sandbox — secure script execution worker
// ═══════════════════════════════════════════════════════════════

var sandboxCmd = &cobra.Command{
	Use:   "sandbox",
	Short: "Sandbox worker (secure script execution)",
	Long:  "Start, stop, or restart the sandbox worker with seccomp-bpf isolation.",
}

var sandboxStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the sandbox worker",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSandbox("start", pidSandbox)
	},
}

var sandboxStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the sandbox worker",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSandbox("stop", pidSandbox)
	},
}

var sandboxRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the sandbox worker",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSandbox("restart", pidSandbox)
	},
}

// ═══════════════════════════════════════════════════════════════

// 8. notifier — multi-channel push (WebSocket + Slack/Telegram/...)
// ═══════════════════════════════════════════════════════════════

var notifierCmd = &cobra.Command{
	Use:   "notifier",
	Short: "Multi-channel notifier service (WS push + Slack/Telegram/Email)",
	Long:  "Start, stop, or restart the notifier service with WebSocket SSE push.",
}

var notifierStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the notifier service",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNotifier("start", pidNotifier)
	},
}

var notifierStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the notifier service",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNotifier("stop", pidNotifier)
	},
}

var notifierRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the notifier service",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNotifier("restart", pidNotifier)
	},
}

// ═══════════════════════════════════════════════════════════════
// 9. wallet — x402 payment key management
// ═══════════════════════════════════════════════════════════════

var (
	walletListen  string
	walletDB      string
	masterKey     string
	masterKeyFile string
	keyFormat     string
	keyEncoding   string
)

var walletCmd = &cobra.Command{
	Use:   "wallet",
	Short: "x402 payment key management daemon",
	Long: `Start, stop, or restart the wallet daemon for Ed25519 key management
and payment signing. Keys are encrypted at rest with AES-256-GCM.`,
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
	Short: "Stop the wallet daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWallet("stop")
	},
}

var walletRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the wallet daemon",
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

// ═══════════════════════════════════════════════════════════════
// 10. console — interactive REPL
// ═══════════════════════════════════════════════════════════════

var consoleCmd = &cobra.Command{
	Use:   "console",
	Short: "Interactive management console (REPL)",
	Long: `Open an interactive REPL for querying the Flowgent store.

Commands:
  list agentflows          List all agentflow definitions
  list runs [id]           List recent runs
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

// ═══════════════════════════════════════════════════════════════
// 11. version
// ═══════════════════════════════════════════════════════════════

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Flowgent v%s (commit: %s, built: %s)\n", Version, GitCommit, BuildTime)
	},
}

// ─── main ─────────────────────────────────────────────────────

func main() {
	// 1. all-in-one
	rootCmd.AddCommand(allInOneCmd)
	allInOneCmd.AddCommand(allInOneStartCmd)
	allInOneCmd.AddCommand(allInOneStopCmd)
	allInOneCmd.AddCommand(allInOneRestartCmd)

	// 2. apiserver
	rootCmd.AddCommand(apiserverCmd)
	apiserverCmd.AddCommand(apiserverStartCmd)
	apiserverCmd.AddCommand(apiserverStopCmd)
	apiserverCmd.AddCommand(apiserverRestartCmd)

	// 3. a2a
	rootCmd.AddCommand(a2aCmd)
	a2aCmd.AddCommand(a2aStartCmd)
	a2aCmd.AddCommand(a2aStopCmd)
	a2aCmd.AddCommand(a2aRestartCmd)

	// 4. controller
	rootCmd.AddCommand(controllerCmd)
	controllerCmd.AddCommand(controllerStartCmd)
	controllerCmd.AddCommand(controllerStopCmd)
	controllerCmd.AddCommand(controllerRestartCmd)

	// 5. jobmanager
	rootCmd.AddCommand(jobmanagerCmd)
	jobmanagerCmd.AddCommand(jobmanagerStartCmd)
	jobmanagerCmd.AddCommand(jobmanagerStopCmd)
	jobmanagerCmd.AddCommand(jobmanagerRestartCmd)
	jobmanagerStartCmd.Flags().StringVar(&jmFlowID, "flow-id", "", "Dedicated flow ID (application mode)")

	// 6. taskmanager
	rootCmd.AddCommand(taskmanagerCmd)
	taskmanagerCmd.AddCommand(taskmanagerStartCmd)
	taskmanagerCmd.AddCommand(taskmanagerStopCmd)
	taskmanagerCmd.AddCommand(taskmanagerRestartCmd)

	// 7. sandbox
	rootCmd.AddCommand(sandboxCmd)
	sandboxCmd.AddCommand(sandboxStartCmd)
	sandboxCmd.AddCommand(sandboxStopCmd)
	sandboxCmd.AddCommand(sandboxRestartCmd)


	// 8. notifier
	rootCmd.AddCommand(notifierCmd)
	notifierCmd.AddCommand(notifierStartCmd)
	notifierCmd.AddCommand(notifierStopCmd)
	notifierCmd.AddCommand(notifierRestartCmd)
	notifierStartCmd.Flags().StringVar(&pidNotifier, "pid-file", "", "PID file")

	// 9. wallet
	rootCmd.AddCommand(walletCmd)
	walletCmd.AddCommand(walletStartCmd)
	walletCmd.AddCommand(walletStopCmd)
	walletCmd.AddCommand(walletRestartCmd)
	walletCmd.AddCommand(walletGenKeyCmd)
	walletStartCmd.Flags().StringVar(&walletListen, "listen", "127.0.0.1:9901", "Listen address")
	walletStartCmd.Flags().StringVar(&walletDB, "db", "", "SQLite database path")
	walletStartCmd.Flags().StringVar(&masterKey, "master-key", "", "Master encryption key")
	walletStartCmd.Flags().StringVar(&masterKeyFile, "master-key-file", "", "Path to master key file")
	walletGenKeyCmd.Flags().StringVar(&keyFormat, "format", "text", "Output structure: text|json")
	walletGenKeyCmd.Flags().StringVar(&keyEncoding, "encoding", "hex", "Key encoding: hex|base64")

	// 10. console
	rootCmd.AddCommand(consoleCmd)

	// 11. version
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
