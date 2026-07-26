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
	if v := os.Getenv("FLOWGENT__CONFIG__FILE"); v != "" {
		defaultCfg = v
	}
	rootCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c",
		defaultCfg, "Path to config file (required if FLOWGENT__CONFIG__FILE not set)")
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
		return StartAllInOne(cfgPath, pidAllInOne)
	},
}

var allInOneStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop all-in-one mode",
	RunE: func(cmd *cobra.Command, args []string) error {
		return StopAllInOne(pidAllInOne)
	},
}

var allInOneRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart all-in-one mode",
	RunE: func(cmd *cobra.Command, args []string) error {
		return RestartAllInOne(cfgPath, pidAllInOne)
	},
}

// ═══════════════════════════════════════════════════════════════
// 2. apiserver — multi-namespace REST + A2A gateway
// ═══════════════════════════════════════════════════════════════

var apiserverCmd = &cobra.Command{
	Use:   "apiserver",
	Short: "Multi-namespace REST API + A2A gateway",
	Long:  "Start, stop, or restart the API server (REST :9999, A2A :9992, mgmt :9991).",
}

var apiserverStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return StartAPIServer(cfgPath, pidAPIServer)
	},
}

var apiserverStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return StopAPIServer(pidAPIServer)
	},
}

var apiserverRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return RestartAPIServer(cfgPath, pidAPIServer)
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
		return StartA2A(cfgPath, pidA2A)
	},
}

var a2aStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the A2A server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return StopA2A(pidA2A)
	},
}

var a2aRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the A2A server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return RestartA2A(cfgPath, pidA2A)
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
Creates dedicated per-flow K8s JM Deployments.`,
}

var controllerStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Controller",
	RunE: func(cmd *cobra.Command, args []string) error {
		return StartController(cfgPath, pidController)
	},
}

var controllerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Controller",
	RunE: func(cmd *cobra.Command, args []string) error {
		return StopController(pidController)
	},
}

var controllerRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the Controller",
	RunE: func(cmd *cobra.Command, args []string) error {
		return RestartController(cfgPath, pidController)
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
		if jmFlowID != "" {
			os.Setenv("FLOWGENT__RUNTIME__AGENT_FLOW_ID", jmFlowID)
		}
		return StartJobManager(cfgPath, pidJobManager)
	},
}

var jobmanagerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the JobManager",
	RunE: func(cmd *cobra.Command, args []string) error {
		return StopJobManager(pidJobManager)
	},
}

var jobmanagerRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the JobManager",
	RunE: func(cmd *cobra.Command, args []string) error {
		return RestartJobManager(cfgPath, pidJobManager)
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
		return StartTaskManager(cfgPath, pidTaskManager)
	},
}

var taskmanagerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the TaskManager",
	RunE: func(cmd *cobra.Command, args []string) error {
		return StopTaskManager(pidTaskManager)
	},
}

var taskmanagerRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the TaskManager",
	RunE: func(cmd *cobra.Command, args []string) error {
		return RestartTaskManager(cfgPath, pidTaskManager)
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
		return StartSandbox(pidSandbox)
	},
}

var sandboxStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the sandbox worker",
	RunE: func(cmd *cobra.Command, args []string) error {
		return StopSandbox(pidSandbox)
	},
}

var sandboxRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the sandbox worker",
	RunE: func(cmd *cobra.Command, args []string) error {
		return RestartSandbox(pidSandbox)
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
		return StartNotifier(cfgPath, pidNotifier)
	},
}

var notifierStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the notifier service",
	RunE: func(cmd *cobra.Command, args []string) error {
		return StopNotifier(pidNotifier)
	},
}

var notifierRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the notifier service",
	RunE: func(cmd *cobra.Command, args []string) error {
		return RestartNotifier(cfgPath, pidNotifier)
	},
}

// ═══════════════════════════════════════════════════════════════
// 9. console — interactive REPL
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
		StartConsole(cfgPath, args, verbose)
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

	// 9. console
	rootCmd.AddCommand(consoleCmd)

	// 10. version
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
