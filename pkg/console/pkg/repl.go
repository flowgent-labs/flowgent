package console

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/peterh/liner"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"

	"gopkg.in/yaml.v3"
)

// consoleState holds the runtime state of the interactive console.
type consoleState struct {
	cfg       *config.FlowgentConfig
	fc        *FlowgentConsole
	namespace string
	rl        *liner.State
	ctx       context.Context
}

// RunREPL runs the interactive management console.
// When args is non-empty, runs in batch mode: executes the command and exits.
func RunREPL(cfgPath string, args []string, verbose bool) {
	if verbose {
		slog.Info("Config path", "path", cfgPath)
		if v := os.Getenv("FLOWGENT__CONFIG__FILE"); v != "" {
			slog.Info("Config env", "FLOWGENT__CONFIG__FILE", v)
		}
	}

	serviceCfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if verbose {
		config.LogConfig(serviceCfg)
	}

	fc, err := NewFlowgentConsole(serviceCfg)
	if err != nil {
		log.Fatalf("Failed to create console: %v", err)
	}
	defer fc.Close()

	state := &consoleState{
		cfg: serviceCfg,
		fc:  fc,
		ctx: context.Background(),
	}

	if serviceCfg.Runtime.Namespace.DefaultNamespace != "" {
		state.namespace = serviceCfg.Runtime.Namespace.DefaultNamespace
	}

	// Batch mode: execute command line and exit
	if len(args) > 0 {
		line := strings.Join(args, " ")
		if verbose {
			slog.Info("Batch command", "line", line)
		}
		state.dispatch(line)
		return
	}

	// Interactive REPL
	rl := liner.NewLiner()
	defer rl.Close()
	rl.SetCtrlCAborts(true)
	historyFile := filepath.Join(os.Getenv("HOME"), ".flowgent", "console_history")
	if f, err := os.Open(historyFile); err == nil {
		rl.ReadHistory(f)
		f.Close()
	}
	os.MkdirAll(filepath.Dir(historyFile), 0755)
	defer func() {
		if f, err := os.Create(historyFile); err == nil {
			rl.WriteHistory(f)
			f.Close()
		}
	}()
	state.rl = rl

	fmt.Println("Flowgent Management Console")
	fmt.Println(`Type "help" for available commands.`)
	fmt.Println()

	for {
		namespaceDisplay := state.namespace
		if namespaceDisplay == "" {
			namespaceDisplay = "(no namespace)"
		}
		line, err := rl.Prompt(fmt.Sprintf("flowgent [%s]> ", namespaceDisplay))
		if err != nil {
			break
		}
		rl.AppendHistory(line)
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		state.dispatch(line)
	}
}

func (s *consoleState) dispatch(line string) {
	parts := strings.Fields(line)
	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "exit", "quit":
		os.Exit(0)

	case "help":
		s.cmdHelp(args)

	case "namespace":
		s.cmdNamespace(args)

	case "flow":
		s.resourceCmd(args, "flow", s.flowList, s.flowGet, s.flowAdd, s.flowRemove)
	case "run":
		s.cmdRun(args)
	case "agent":
		s.resourceCmd(args, "agent", s.agentList, s.agentGet, s.agentAdd, s.agentRemove)
	case "mcp":
		s.resourceCmd(args, "mcp", s.mcpList, s.mcpGet, s.mcpAdd, s.mcpRemove)
	case "skill":
		s.resourceCmd(args, "skill", s.skillList, s.skillGet, s.skillAdd, s.skillRemove)
	case "llm":
		s.resourceCmd(args, "llm", s.llmList, s.llmGet, s.llmAdd, s.llmRemove)
	case "channel":
		s.resourceCmd(args, "channel", s.channelList, s.channelGet, s.channelAdd, s.channelRemove)

	case "export":
		s.cmdExport(args)
	case "import":
		s.cmdImport(args)

	default:
		fmt.Printf("Unknown command: %s (type 'help' for available commands)\n", cmd)
	}
}

// ─── Namespace ────────────────────────────────────────────────────

func (s *consoleState) cmdNamespace(args []string) {
	if len(args) == 0 {
		if s.namespace == "" {
			fmt.Println("No namespace set. Usage: namespace set <namespace-id>")
		} else {
			fmt.Printf("Current namespace: %s\n", s.namespace)
		}
		return
	}
	switch strings.ToLower(args[0]) {
	case "set":
		if len(args) < 2 {
			fmt.Println("Usage: namespace set <namespace-id>")
			return
		}
		s.namespace = args[1]
		fmt.Printf("Namespace set to: %s\n", s.namespace)
	case "unset":
		s.namespace = ""
		fmt.Println("Namespace cleared.")
	default:
		fmt.Println("Usage: namespace [set <id> | unset]")
	}
}

// ─── Help ───────────────────────────────────────────────────────

func (s *consoleState) cmdHelp(args []string) {
	fmt.Print(`Management console commands (all namespace-scoped):

  namespace set <id>           Set the active namespace (required before CRUD commands)
  namespace unset              Clear the active namespace

  flow    [list | get <id> | add | remove <id>]
  run     [list | get <id> | create <flow-id> | start <run-id> | stop <run-id>]
  agent   [list | get <name> | add | remove <name>]
  mcp     [list | get <name> | add | remove <name>]
  skill   [list | get <id> | add | remove <id>]
  llm     [list | get <id> | add | remove <id>]
  channel [list | get <id> | add | remove <id>]
  export [--kind <kinds>] --output <filepath>   Export Flowgent resources
  import <pattern...>                           Import resources from JSON/YAML files

  help                     Show this help
  exit, quit               Exit the console
`)
}

// ─── Resource dispatcher ────────────────────────────────────────

type cmdFunc func(args []string)

func (s *consoleState) requireNamespace() bool {
	if s.namespace == "" {
		fmt.Println("Error: no namespace set. Use 'namespace set <id>' first.")
		return false
	}
	return true
}

func (s *consoleState) resourceCmd(args []string, name string, listFn, getFn, addFn, removeFn cmdFunc) {
	if len(args) == 0 {
		fmt.Printf("Usage: %s [list | get <id> | add | remove <id>]\n", name)
		return
	}
	switch strings.ToLower(args[0]) {
	case "list":
		listFn(args[1:])
	case "get":
		getFn(args[1:])
	case "add":
		addFn(args[1:])
	case "remove":
		removeFn(args[1:])
	default:
		fmt.Printf("Unknown %s action: %s (use list, get, add, remove)\n", name, args[0])
	}
}

// ─── Flow ───────────────────────────────────────────────────────

func (s *consoleState) flowList(args []string) {
	if !s.requireNamespace() {
		return
	}
	items, err := s.fc.ListFlows()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if len(items) == 0 {
		fmt.Println("No agentflows found.")
		return
	}
	fmt.Printf("%-38s %-8s %s\n", "ID", "VERSION", "CREATED")
	fmt.Println(strings.Repeat("-", 80))
	for _, f := range items {
		fmt.Printf("%-38s %-8d %s\n", f.FlowID, f.Version, f.CreatedAt.Format("2006-01-02 15:04"))
	}
	fmt.Printf("(%d flows)\n", len(items))
}

func (s *consoleState) flowGet(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: flow get <id>")
		return
	}
	spec, err := s.fc.GetFlow(args[0])
	if err != nil || spec == nil {
		fmt.Printf("Flow not found: %s\n", args[0])
		return
	}
	printJSON(spec)
}

func (s *consoleState) flowAdd(args []string) {
	if !s.requireNamespace() {
		return
	}
	s.fc.SetNamespace(s.namespace)
	fmt.Println("Enter agentflow JSON (end with a line containing only '.'):")
	body := readMultiline(s.rl)
	var spec entities.FlowInfo
	if err := json.Unmarshal([]byte(body), &spec); err != nil {
		fmt.Printf("Invalid JSON: %v\n", err)
		return
	}
	if err := s.fc.AddFlow(&spec); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Created flow: %s\n", spec.ID)
}

func (s *consoleState) flowRemove(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: flow remove <id>")
		return
	}
	if err := s.fc.RemoveFlow(args[0]); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Deleted flow: %s\n", args[0])
}

// ─── Run ────────────────────────────────────────────────────────

func (s *consoleState) cmdRun(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: run [list | get <id> | create <flow-id> | start <run-id> | stop <run-id>]")
		return
	}
	switch strings.ToLower(args[0]) {
	case "list":
		s.runList(args[1:])
	case "get":
		s.runGet(args[1:])
	case "create":
		s.runCreate(args[1:])
	case "start":
		s.runStart(args[1:])
	case "stop":
		s.runStop(args[1:])
	default:
		fmt.Printf("Unknown run action: %s (use list, get, create, start, stop)\n", args[0])
	}
}

func (s *consoleState) runList(args []string) {
	if !s.requireNamespace() {
		return
	}
	items, err := s.fc.ListRuns()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if len(items) == 0 {
		fmt.Println("No runs found.")
		return
	}
	fmt.Printf("%-38s %-36s %-12s %s\n", "RUN ID", "FLOW", "STATUS", "CREATED")
	fmt.Println(strings.Repeat("-", 110))
	for _, r := range items {
		fmt.Printf("%-38s %-36s %-12s %s\n", r.ID, truncate(r.AgentFlowID, 36), string(r.Status), r.CreatedAt.Format("2006-01-02 15:04"))
	}
	fmt.Printf("(%d runs)\n", len(items))
}

func (s *consoleState) runGet(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: run get <id>")
		return
	}
	run, err := s.fc.GetRun(args[0])
	if err != nil || run == nil {
		fmt.Printf("Run not found: %s\n", args[0])
		return
	}
	printJSON(run)
}

func (s *consoleState) runCreate(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: run create <flow-id>")
		return
	}
	s.fc.SetNamespace(s.namespace)
	run, err := s.fc.CreateRun(args[0])
	if err != nil {
		fmt.Printf("Error creating run: %v\n", err)
		return
	}
	fmt.Printf("Created run: %s (status: PENDING)\n", run.ID)
	fmt.Printf("Use 'run start %s' to trigger execution.\n", run.ID)
}

func (s *consoleState) runStart(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: run start <run-id>")
		return
	}
	run, err := s.fc.StartRun(args[0])
	if err != nil {
		fmt.Printf("Error starting run: %v\n", err)
		return
	}
	fmt.Printf("Run %s started.\n", run.ID)
}

func (s *consoleState) runStop(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: run stop <run-id>")
		return
	}
	if err := s.fc.StopRun(args[0]); err != nil {
		fmt.Printf("Error stopping run: %v\n", err)
		return
	}
	fmt.Printf("Run %s cancelled.\n", args[0])
}

// ─── Agent ──────────────────────────────────────────────────────

func (s *consoleState) agentList(args []string) {
	if !s.requireNamespace() {
		return
	}
	items, err := s.fc.ListAgents()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if len(items) == 0 {
		fmt.Println("No agents found.")
		return
	}
	fmt.Printf("%-24s %-24s %s\n", "NAME", "MODEL", "CREATED")
	fmt.Println(strings.Repeat("-", 80))
	for _, a := range items {
		fmt.Printf("%-24s %-24s %s\n", a.Name, a.Model, a.CreatedAt.Format("2006-01-02 15:04"))
	}
	fmt.Printf("(%d agents)\n", len(items))
}

func (s *consoleState) agentGet(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: agent get <name>")
		return
	}
	a, err := s.fc.GetAgent(args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	printJSON(a)
}

func (s *consoleState) agentAdd(args []string) {
	if !s.requireNamespace() {
		return
	}
	s.fc.SetNamespace(s.namespace)
	fmt.Println("Enter agent JSON (end with a line containing only '.'):")
	body := readMultiline(s.rl)
	var a entities.AgentInfo
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		fmt.Printf("Invalid JSON: %v\n", err)
		return
	}
	if err := s.fc.AddAgent(&a); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Created agent: %s\n", a.Name)
}

func (s *consoleState) agentRemove(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: agent remove <name>")
		return
	}
	if err := s.fc.RemoveAgent(args[0]); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Deleted agent: %s\n", args[0])
}

// ─── MCP ────────────────────────────────────────────────────────

func (s *consoleState) mcpList(args []string) {
	if !s.requireNamespace() {
		return
	}
	items, err := s.fc.ListMCPs()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if len(items) == 0 {
		fmt.Println("No MCPs found.")
		return
	}
	fmt.Printf("%-24s %-12s %-12s %s\n", "NAME", "TYPE", "ENABLED", "COMMAND")
	fmt.Println(strings.Repeat("-", 80))
	for _, m := range items {
		fmt.Printf("%-24s %-12s %-12v %v\n", m.Name, m.Type, m.Enabled, m.Command)
	}
	fmt.Printf("(%d MCPs)\n", len(items))
}

func (s *consoleState) mcpGet(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: mcp get <name>")
		return
	}
	m, err := s.fc.GetMCP(args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	printJSON(m)
}

func (s *consoleState) mcpAdd(args []string) {
	if !s.requireNamespace() {
		return
	}
	s.fc.SetNamespace(s.namespace)
	fmt.Println("Enter MCP JSON (end with a line containing only '.'):")
	body := readMultiline(s.rl)
	var m entities.McpInfo
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		fmt.Printf("Invalid JSON: %v\n", err)
		return
	}
	if err := s.fc.AddMCP(&m); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Created MCP: %s\n", m.Name)
}

func (s *consoleState) mcpRemove(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: mcp remove <name>")
		return
	}
	if err := s.fc.RemoveMCP(args[0]); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Deleted MCP: %s\n", args[0])
}

// ─── Skill ──────────────────────────────────────────────────────

func (s *consoleState) skillList(args []string) {
	if !s.requireNamespace() {
		return
	}
	items, err := s.fc.ListSkills()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if len(items) == 0 {
		fmt.Println("No skills found.")
		return
	}
	fmt.Printf("%-38s %-8s %s\n", "ID", "VERSION", "CREATED")
	fmt.Println(strings.Repeat("-", 80))
	for _, sk := range items {
		fmt.Printf("%-38s %-8d %s\n", sk.FlowID, sk.Version, sk.CreatedAt.Format("2006-01-02 15:04"))
	}
	fmt.Printf("(%d skills)\n", len(items))
}

func (s *consoleState) skillGet(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: skill get <id>")
		return
	}
	spec, err := s.fc.GetSkill(args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	printJSON(spec)
}

func (s *consoleState) skillAdd(args []string) {
	if !s.requireNamespace() {
		return
	}
	s.fc.SetNamespace(s.namespace)
	fmt.Println("Enter skill JSON (end with a line containing only '.'):")
	body := readMultiline(s.rl)
	var spec entities.FlowInfo
	if err := json.Unmarshal([]byte(body), &spec); err != nil {
		fmt.Printf("Invalid JSON: %v\n", err)
		return
	}
	if err := s.fc.AddSkill(&spec); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Created skill: %s\n", spec.ID)
}

func (s *consoleState) skillRemove(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: skill remove <id>")
		return
	}
	if err := s.fc.RemoveSkill(args[0]); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Deleted skill: %s\n", args[0])
}

// ─── LLM ────────────────────────────────────────────────────────

func (s *consoleState) llmList(args []string) {
	if !s.requireNamespace() {
		return
	}
	items, err := s.fc.ListLLMs()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if len(items) == 0 {
		fmt.Println("No LLM providers found.")
		return
	}
	fmt.Printf("%-38s %-24s %-24s %s\n", "ID", "TYPE", "MODELS", "ENDPOINT")
	fmt.Println(strings.Repeat("-", 110))
	for _, p := range items {
		modelCount := len(p.Models)
		fmt.Printf("%-38s %-24s %-24d %s\n", p.ID, truncate(p.Provider, 24), modelCount, truncate(p.Endpoint, 40))
	}
	fmt.Printf("(%d providers)\n", len(items))
}

func (s *consoleState) llmGet(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: llm get <id>")
		return
	}
	p, err := s.fc.GetLLM(args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	printJSON(p)
}

func (s *consoleState) llmAdd(args []string) {
	if !s.requireNamespace() {
		return
	}
	s.fc.SetNamespace(s.namespace)
	fmt.Println("Enter LLM provider JSON (end with a line containing only '.'):")
	body := readMultiline(s.rl)
	var p entities.LlmProviderInfo
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		fmt.Printf("Invalid JSON: %v\n", err)
		return
	}
	if err := s.fc.AddLLM(&p); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Created LLM provider: %s\n", p.ID)
}

func (s *consoleState) llmRemove(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: llm remove <id>")
		return
	}
	if err := s.fc.RemoveLLM(args[0]); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Deleted LLM provider: %s\n", args[0])
}

// ─── Channel ────────────────────────────────────────────────────

func (s *consoleState) channelList(args []string) {
	if !s.requireNamespace() {
		return
	}
	items, err := s.fc.ListChannels()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if len(items) == 0 {
		fmt.Println("No notification channels found.")
		return
	}
	fmt.Printf("%-38s %-24s %-12s %s\n", "ID", "NAME", "TYPE", "ENABLED")
	fmt.Println(strings.Repeat("-", 90))
	for _, ch := range items {
		fmt.Printf("%-38s %-24s %-12s %v\n", ch.ID, truncate(ch.Name, 24), string(ch.ChannelType), ch.Enabled)
	}
	fmt.Printf("(%d channels)\n", len(items))
}

func (s *consoleState) channelGet(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: channel get <id>")
		return
	}
	ch, err := s.fc.GetChannel(args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	printJSON(ch)
}

func (s *consoleState) channelAdd(args []string) {
	if !s.requireNamespace() {
		return
	}
	s.fc.SetNamespace(s.namespace)
	fmt.Println("Enter channel JSON (end with a line containing only '.'):")
	body := readMultiline(s.rl)
	var ch entities.NotifyChannelInfo
	if err := json.Unmarshal([]byte(body), &ch); err != nil {
		fmt.Printf("Invalid JSON: %v\n", err)
		return
	}
	if err := s.fc.AddChannel(&ch); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Created channel: %s\n", ch.ID)
}

func (s *consoleState) channelRemove(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: channel remove <id>")
		return
	}
	if err := s.fc.RemoveChannel(args[0]); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Deleted channel: %s\n", args[0])
}

// ─── Import/Export ──────────────────────────────────────────────

func (s *consoleState) cmdExport(args []string) {
	var kinds []string
	var output string

	i := 0
	for i < len(args) {
		switch {
		case args[i] == "--kind" || args[i] == "-k":
			i++
			if i < len(args) {
				kinds = splitAndTrim(args[i])
			}
		case strings.HasPrefix(args[i], "--kind="):
			kinds = splitAndTrim(strings.TrimPrefix(args[i], "--kind="))
		case strings.HasPrefix(args[i], "-k="):
			kinds = splitAndTrim(strings.TrimPrefix(args[i], "-k="))
		case args[i] == "--output" || args[i] == "-o":
			i++
			if i < len(args) {
				output = args[i]
			}
		case strings.HasPrefix(args[i], "--output="):
			output = strings.TrimPrefix(args[i], "--output=")
		case strings.HasPrefix(args[i], "-o="):
			output = strings.TrimPrefix(args[i], "-o=")
		default:
			// positional argument — treat as output file
			if output == "" {
				output = args[i]
			}
		}
		i++
	}

	if output == "" {
		fmt.Println("Usage: export [--kind <kinds>] --output <filepath>")
		fmt.Println("  --kind, -k     Comma-separated resource kinds: llm,channel,mcp,skill,agent,flow,flowrun")
		fmt.Println("                 Default: all supported Flowgent resource kinds")
		fmt.Println("  --output, -o   Output file path (.json or .yaml/.yml)")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  export --output /tmp/backup.yaml")
		fmt.Println("  export -k mcp,llm,agent -o /tmp/partial.json")
		return
	}

	if len(kinds) > 0 {
		validKinds := map[string]bool{"llm": true, "channel": true, "mcp": true, "skill": true, "agent": true, "flow": true, "flowrun": true}
		for _, k := range kinds {
			if !validKinds[k] {
				fmt.Printf("Invalid kind %q. Valid kinds: llm, channel, mcp, skill, agent, flow, flowrun\n", k)
				return
			}
		}
	}

	data, err := s.fc.ExportKinds(kinds)
	if err != nil {
		fmt.Printf("Error collecting data: %v\n", err)
		return
	}
	fm, err := DetectFormat(output, nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	var out []byte
	switch fm {
	case "json":
		out, err = json.MarshalIndent(data, "", "  ")
	case "yaml":
		out, err = yaml.Marshal(data)
	}
	if err != nil {
		fmt.Printf("Error marshalling: %v\n", err)
		return
	}
	if err := os.WriteFile(output, out, 0644); err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		return
	}
	fmt.Printf("Exported to %s (%d llms, %d channels, %d mcps, %d skills, %d agents, %d flows, %d runs)\n",
		output, len(data.LLMs), len(data.Channels), len(data.MCPs), len(data.Skills),
		len(data.AgentDefs), len(data.AgentFlows), len(data.FlowRuns))
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func (s *consoleState) cmdImport(args []string) {
	if !s.requireNamespace() {
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: import <pattern...>    Import files/directories (supports glob patterns)")
		fmt.Println("  import config/*/*.yaml        Import all .yaml files matching glob")
		fmt.Println("  import config/agents/          Import all .yaml/.json from a directory")
		fmt.Println("  import a.yaml b.yaml           Import multiple files/patterns")
		return
	}
	s.fc.SetNamespace(s.namespace)
	imported := s.fc.ImportPaths(args, nil)
	fmt.Printf("Imported %d resources.\n", imported)
}

// ─── Helpers ────────────────────────────────────────────────────

func readMultiline(rl *liner.State) string {
	var lines []string
	for {
		line, err := rl.Prompt("... ")
		if err != nil {
			break
		}
		if line == "." {
			break
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
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
