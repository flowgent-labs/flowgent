package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/flowgent-labs/flowgent/config/src/config"
	"github.com/flowgent-labs/flowgent/store/src"
)

func startConsole() {
	if verbose {
		log.Printf("Config path: %s", cfgPath)
		if v := os.Getenv("FLOWGENT_CONFIG_FILE"); v != "" {
			log.Printf("Config env:  FLOWGENT_CONFIG_FILE=%s", v)
		} else {
			log.Printf("Config env:  FLOWGENT_CONFIG_FILE (not set, using default)")
		}
	}

	serviceCfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if verbose {
		logConfig(serviceCfg)
	}

	storeImpl := initStore(serviceCfg)
	defer storeImpl.(interface{ Close() error }).Close()

	ctx := context.Background()

	fmt.Println("Flowgent Management Console")
	fmt.Println(`Type "help" for available commands.`)
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("flowgent> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		cmd := strings.ToLower(parts[0])
		args := parts[1:]

		switch cmd {
		case "exit", "quit":
			return

		case "help":
			fmt.Print(`Interactive commands:
  list agentflows          List all agentflow definitions
  list runs [agentflow-id] List recent runs (optionally filtered)
  show run <id>            Show details of a specific run
  tasks <run-id>           List task runs for an agentflow run
  show task <id>           Show details of a specific task
  help                     Show this help
  exit, quit               Exit the console
`)

		case "list":
			if len(args) == 0 {
				fmt.Println("Usage: list agentflows | list runs [agentflow-id]")
				continue
			}
			switch strings.ToLower(args[0]) {
			case "agentflows", "flows", "af":
				listAgentFlowsCmd(ctx, storeImpl)
			case "runs":
				filter := ""
				if len(args) > 1 {
					filter = args[1]
				}
				listRunsCmd(ctx, storeImpl, filter)
			default:
				fmt.Printf("Unknown list target: %s\n", args[0])
			}

		case "show":
			if len(args) < 2 {
				fmt.Println("Usage: show run <id> | show task <id>")
				continue
			}
			switch strings.ToLower(args[0]) {
			case "run":
				showRunCmd(ctx, storeImpl, args[1])
			case "task":
				showTaskCmd(ctx, storeImpl, args[1])
			default:
				fmt.Printf("Unknown show target: %s\n", args[0])
			}

		case "tasks":
			if len(args) < 1 {
				fmt.Println("Usage: tasks <run-id>")
				continue
			}
			tasksCmd(ctx, storeImpl, args[0])

		default:
			fmt.Printf("Unknown command: %s (type 'help' for available commands)\n", cmd)
		}
	}
}

func listAgentFlowsCmd(ctx context.Context, s store.IStore) {
	defs, err := s.ListAgentFlowDefinitions(ctx)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if len(defs) == 0 {
		fmt.Println("No agentflow definitions found.")
		return
	}
	fmt.Printf("%-36s %-8s %s\n", "AGENTFLOW ID", "VERSION", "CREATED")
	fmt.Println(strings.Repeat("-", 80))
	for _, d := range defs {
		fmt.Printf("%-36s %-8d %s\n", d.AgentFlowID, d.Version, d.CreatedAt.Format("2006-01-02 15:04:05"))
	}
	fmt.Printf("(%d agentflows)\n", len(defs))
}

func listRunsCmd(ctx context.Context, s store.IStore, agentFlowID string) {
	runs, err := s.ListAgentFlowRuns(ctx, agentFlowID, 50)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if len(runs) == 0 {
		fmt.Println("No runs found.")
		return
	}
	fmt.Printf("%-38s %-36s %-12s %s\n", "RUN ID", "AGENTFLOW", "STATUS", "CREATED")
	fmt.Println(strings.Repeat("-", 110))
	for _, r := range runs {
		fmt.Printf("%-38s %-36s %-12s %s\n", r.ID, truncate(r.AgentFlowID, 36), r.Status, r.CreatedAt.Format("2006-01-02 15:04"))
	}
	fmt.Printf("(%d runs)\n", len(runs))
}

func showRunCmd(ctx context.Context, s store.IStore, id string) {
	run, err := s.GetAgentFlowRun(ctx, id)
	if err != nil || run == nil {
		fmt.Printf("Run not found: %s\n", id)
		return
	}
	printJSON(run)
}

func showTaskCmd(ctx context.Context, s store.IStore, id string) {
	task, err := s.GetTaskRun(ctx, id)
	if err != nil || task == nil {
		fmt.Printf("Task not found: %s\n", id)
		return
	}
	_ = task
	printJSON(task)
}

func tasksCmd(ctx context.Context, s store.IStore, runID string) {
	tasks, err := s.GetTaskRunsByAgentFlowRun(ctx, runID)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if len(tasks) == 0 {
		fmt.Println("No task runs found.")
		return
	}
	fmt.Printf("%-38s %-24s %-12s %s\n", "TASK ID", "NODE", "STATUS", "STARTED")
	fmt.Println(strings.Repeat("-", 110))
	for _, t := range tasks {
		started := "-"
		if t.StartedAt != nil {
			started = t.StartedAt.Format("2006-01-02 15:04")
		}
		fmt.Printf("%-38s %-24s %-12s %s\n", t.ID, truncate(t.NodeID, 24), t.Status, started)
	}
	fmt.Printf("(%d tasks)\n", len(tasks))
}

func printJSON(v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Printf("Error marshalling: %v\n", err)
		return
	}
	fmt.Println(string(b))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
