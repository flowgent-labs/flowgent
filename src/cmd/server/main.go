package main

import (
	"fmt"
	"os"
	"strings"
)

var (
	Version   = "dev"
	GitCommit = "none"
	BuildTime = "unknown"
)

const usage = `Flowgent — Autonomous Agentflow Orchestration Engine

Usage:
  flowgent [options] <command> [args]

Commands:
  daemon     Start, stop, or restart the Flowgent server
  console    Interactive management console for querying store data

Options:
  -c, --config FILE   Path to config file (default: etc/flowgent.yaml, or $FLOWGENT_CONFIG_FILE)
  -v, --verbose       Enable debug logging (prints config path, version, etc.)
  -V, --version       Show version information
  -h, --help          Show this help message

Run 'flowgent <command> --help' for more information about a command.
`

const daemonUsage = `Usage:
  flowgent daemon <action> [options]

Actions:
  start      Start the Flowgent server (REST API + A2A API)
  stop       Stop a running Flowgent server via PID file
  restart    Stop then start the Flowgent server

Options:
  -c, --config FILE     Path to config file (default: etc/flowgent.yaml, or $FLOWGENT_CONFIG_FILE)
  --pid-file FILE       PID file path (default: /tmp/flowgent.pid)
`

const consoleUsage = `Usage:
  flowgent console [options]

Options:
  -c, --config FILE   Path to config file (default: etc/flowgent.yaml, or $FLOWGENT_CONFIG_FILE)

Interactive commands:
  list agentflows          List all agentflow definitions
  list runs [agentflow-id] List recent runs (optionally filtered)
  show run <id>            Show details of a specific run
  tasks <run-id>           List task runs for an agentflow run
  show task <id>           Show details of a specific task
  help                     Show this help
  exit, quit               Exit the console
`

func main() {
	args := os.Args[1:]

	cfgPath := os.Getenv("FLOWGENT_CONFIG_FILE")
	if cfgPath == "" {
		cfgPath = "etc/flowgent.yaml"
	}
	showHelp := false
	showVersion := false
	verbose := false
	pidFile := "/tmp/flowgent.pid"

	i := 0
	for i < len(args) {
		a := args[i]
		switch a {
		case "-c", "--config":
			i++
			if i < len(args) {
				cfgPath = args[i]
			}
		case "--pid-file":
			i++
			if i < len(args) {
				pidFile = args[i]
			}
		case "-h", "--help":
			showHelp = true
		case "-v", "--verbose":
			verbose = true
		case "-V", "--version":
			showVersion = true
		default:
			if !strings.HasPrefix(a, "-") {
				goto parseCmd
			}
		}
		i++
	}
parseCmd:

	remaining := args[i:]
	var cmd string
	if len(remaining) > 0 {
		cmd = remaining[0]
	}

	if showVersion {
		fmt.Printf("Flowgent v%s (commit: %s, built: %s)\n", Version, GitCommit, BuildTime)
		os.Exit(0)
	}

	if showHelp || cmd == "" || cmd == "help" {
		fmt.Print(usage)
		os.Exit(0)
	}

	cmdArgs := remaining[1:]

	switch cmd {
	case "daemon":
		handleDaemon(cfgPath, pidFile, verbose, cmdArgs)
	case "console":
		handleConsole(cfgPath, verbose)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n%s", cmd, usage)
		os.Exit(1)
	}
}
