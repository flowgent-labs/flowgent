package console

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peterh/liner"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/agentdef"
	"github.com/flowgent-labs/flowgent/store/pkg/agentflow"
	"github.com/flowgent-labs/flowgent/store/pkg/flowrun"
	"github.com/flowgent-labs/flowgent/store/pkg/llmprovider"
	"github.com/flowgent-labs/flowgent/store/pkg/mcp"
	"github.com/flowgent-labs/flowgent/store/pkg/notifier"
	"github.com/flowgent-labs/flowgent/wallet/pkg"
	walletproviders "github.com/flowgent-labs/flowgent/wallet/pkg/providers"

	"gopkg.in/yaml.v3"
)

// ExportData is the root structure for import/export of all resources.
type ExportData struct {
	LLMs       []entities.LlmProviderInfo     `json:"llms" yaml:"llms"`
	Channels   []entities.NotifyChannelInfo `json:"channels" yaml:"channels"`
	MCPs       []entities.McpInfo          `json:"mcps" yaml:"mcps"`
	Skills     []entities.AgentFlowInfo   `json:"skills" yaml:"skills"`
	AgentDefs  []entities.AgentInfo        `json:"agentDefs" yaml:"agentDefs"`
	AgentFlows []entities.AgentFlowInfo   `json:"agentFlows" yaml:"agentFlows"`
	FlowRuns   []entities.FlowRunInfo    `json:"flowRuns" yaml:"flowRuns"`
	Wallets    []WalletExport          `json:"wallets" yaml:"wallets"`
}

// WalletExport is the exported form of a wallet key.
type WalletExport struct {
	Name       string `json:"name" yaml:"name"`
	PrivateKey string `json:"private_key" yaml:"private_key"`
}

// consoleState holds the runtime state of the interactive console.
type consoleState struct {
	cfg         *config.FlowgentConfig
	store       store.IStore
	tenant      string
	secretStore payments.SecretStoreProvider
	rl          *liner.State
	ctx         context.Context
}

// Stores initialized lazily.
type lazyStores struct {
	agents    agentdef.IAgentDefStore
	flows     agentflow.IAgentFlowStore
	runs      flowrun.IFlowRunStore
	channels  notifier.INotifierStore
	llm       llmprovider.ILlmProviderStore
	mcps      mcp.IMCPStore
}

// StartConsole runs the interactive management console.
func StartConsole(cfgPath string, verbose bool) {
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

	storeImpl := store.InitStore(serviceCfg)
	defer storeImpl.(interface{ Close() error }).Close()

	// Readline with history
	rl := liner.NewLiner()
	defer rl.Close()
	rl.SetCtrlCAborts(true)
	historyFile := filepath.Join(os.Getenv("HOME"), ".flowagent", "console_history")
	if f, err := os.Open(historyFile); err == nil {
		rl.ReadHistory(f)
		f.Close()
	}
	// Ensure history dir exists before write
	os.MkdirAll(filepath.Dir(historyFile), 0755)
	defer func() {
		if f, err := os.Create(historyFile); err == nil {
			rl.WriteHistory(f)
			f.Close()
		}
	}()

	state := &consoleState{
		cfg:   serviceCfg,
		store: storeImpl,
		rl:    rl,
		ctx:   context.Background(),
	}

	// Auto-set tenant from config
	if serviceCfg.Tenant.DefaultTenant != "" {
		state.tenant = serviceCfg.Tenant.DefaultTenant
	}

	// Init secret store for wallet commands (non-fatal if no master key)
	state.initSecretStore()

	fmt.Println("Flowgent Management Console")
	fmt.Println(`Type "help" for available commands.`)
	fmt.Println()

	for {
		tenantDisplay := state.tenant
		if tenantDisplay == "" {
			tenantDisplay = "(no tenant)"
		}
		line, err := rl.Prompt(fmt.Sprintf("flowgent [%s]> ", tenantDisplay))
		if err != nil {
			break // Ctrl-D or error
		}
		rl.AppendHistory(line)
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		state.dispatch(line)
	}
}

func (s *consoleState) initSecretStore() {
	if s.cfg.Wallet == nil {
		return
	}
	mkf := s.cfg.Wallet.SecretStore.MasterKeyFile
	if mkf == "" {
		return
	}
	db, ok := s.store.DB().(*sql.DB)
	if !ok {
		return
	}
	ss, err := walletproviders.NewDefaultSecretStoreProvider(db, mkf)
	if err != nil {
		slog.Warn("secret store init failed (wallet commands unavailable)", "err", err)
		return
	}
	s.secretStore = ss
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

	case "tenant":
		s.cmdTenant(args)

	case "llm":
		s.resourceCmd(args, "llm", s.llmList, s.llmGet, s.llmAdd, s.llmRemove)
	case "channel":
		s.resourceCmd(args, "channel", s.channelList, s.channelGet, s.channelAdd, s.channelRemove)
	case "agent":
		s.resourceCmd(args, "agent", s.agentList, s.agentGet, s.agentAdd, s.agentRemove)
	case "mcp":
		s.resourceCmd(args, "mcp", s.mcpList, s.mcpGet, s.mcpAdd, s.mcpRemove)
	case "skill":
		s.resourceCmd(args, "skill", s.skillList, s.skillGet, s.skillAdd, s.skillRemove)
	case "flow":
		s.resourceCmd(args, "flow", s.flowList, s.flowGet, s.flowAdd, s.flowRemove)
	case "run":
		s.cmdRun(args)
	case "wallet":
		s.resourceCmd(args, "wallet", s.walletList, s.walletGet, s.walletAdd, s.walletRemove)

	case "export":
		s.cmdExport(args)
	case "import":
		s.cmdImport(args)

	default:
		fmt.Printf("Unknown command: %s (type 'help' for available commands)\n", cmd)
	}
}

// ─── Tenant ─────────────────────────────────────────────────────

func (s *consoleState) cmdTenant(args []string) {
	if len(args) == 0 {
		if s.tenant == "" {
			fmt.Println("No tenant set. Usage: tenant set <tenant-id>")
		} else {
			fmt.Printf("Current tenant: %s\n", s.tenant)
		}
		return
	}
	switch strings.ToLower(args[0]) {
	case "set":
		if len(args) < 2 {
			fmt.Println("Usage: tenant set <tenant-id>")
			return
		}
		s.tenant = args[1]
		fmt.Printf("Tenant set to: %s\n", s.tenant)
	case "unset":
		s.tenant = ""
		fmt.Println("Tenant cleared.")
	default:
		fmt.Println("Usage: tenant [set <id> | unset]")
	}
}

// ─── Help ───────────────────────────────────────────────────────

func (s *consoleState) cmdHelp(args []string) {
	fmt.Print(`Management console commands (all tenant-scoped):

  tenant set <id>           Set the active tenant (required before CRUD commands)
  tenant unset              Clear the active tenant

  llm     [list | get <id> | add | remove <id>]
  channel [list | get <id> | add | remove <id>]
  agent   [list | get <name> | add | remove <name>]
  mcp     [list | get <name> | add | remove <name>]
  skill   [list | get <id> | add | remove <id>]
  flow    [list | get <id> | add | remove <id>]
  run     [list | get <id> | create <flow-id> | start <run-id> | stop <run-id>]
  wallet  [list | get <name> | add <name> | remove <name>]
                wallet add <name> — generates a new Ed25519 keypair
                wallet add <name> <hex-private-key> — imports an existing keypair

  export <filepath>        Export all resources to JSON or YAML file
  import <filepath>        Import resources from JSON or YAML file

  help                     Show this help
  exit, quit               Exit the console
`)
}

// ─── Resource dispatcher ────────────────────────────────────────

type cmdFunc func(args []string)

func (s *consoleState) requireTenant() bool {
	if s.tenant == "" {
		fmt.Println("Error: no tenant set. Use 'tenant set <id>' first.")
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

// ─── Stores (lazy init) ─────────────────────────────────────────

func (s *consoleState) getStores() *lazyStores {
	ls := &lazyStores{}
	switch db := s.store.DB().(type) {
	case *pgxpool.Pool:
		ls.agents = agentdef.NewAgentDefPostgresStore(db)
		ls.flows = agentflow.NewAgentFlowPostgresStore(db)
		ls.runs = flowrun.NewFlowRunPostgresStore(db)
		ls.channels = notifier.NewNotifierPostgresStore(db)
		ls.llm = llmprovider.NewLlmProviderPostgresStore(db)
		ls.mcps = mcp.NewMCPPostgresStore(db)
	case *sql.DB:
		ls.agents = agentdef.NewAgentDefSQLiteStore(db)
		ls.flows = agentflow.NewAgentFlowSQLiteStore(db)
		ls.runs = flowrun.NewFlowRunSQLiteStore(db)
		ls.channels = notifier.NewNotifierSQLiteStore(db)
		ls.llm = llmprovider.NewLlmProviderSQLiteStore(db)
		ls.mcps = mcp.NewMCPSQLiteStore(db)
	}
	return ls
}

// ─── LLM ────────────────────────────────────────────────────────

func (s *consoleState) llmList(args []string) {
	if !s.requireTenant() { return }
	page, err := s.getStores().llm.Select(s.ctx, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	items := page.Items
	if len(items) == 0 {
		fmt.Println("No LLM providers found.")
		return
	}
	fmt.Printf("%-38s %-24s %-24s %s\n", "ID", "TYPE", "MODELS", "ENDPOINT")
	fmt.Println(strings.Repeat("-", 110))
	for _, p := range items {
		modelCount := len(p.Models)
		fmt.Printf("%-38s %-24s %-24d %s\n", p.ID, truncate(p.Type, 24), modelCount, truncate(p.Endpoint, 40))
	}
	fmt.Printf("(%d providers)\n", len(items))
}

func (s *consoleState) llmGet(args []string) {
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: llm get <id>")
		return
	}
	p, err := s.getStores().llm.Get(s.ctx, args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	printJSON(p)
}

func (s *consoleState) llmAdd(args []string) {
	if !s.requireTenant() { return }
	fmt.Println("Enter LLM provider JSON (end with a line containing only '.'):")
	body := readMultiline(s.rl)
	var p entities.LlmProviderInfo
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		fmt.Printf("Invalid JSON: %v\n", err)
		return
	}
	p.ID = uuid.New().String()
	p.TenantID = s.tenant
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()
	if err := s.getStores().llm.Save(s.ctx, &p); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Created LLM provider: %s\n", p.ID)
}

func (s *consoleState) llmRemove(args []string) {
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: llm remove <id>")
		return
	}
	if err := s.getStores().llm.Delete(s.ctx, args[0]); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Deleted LLM provider: %s\n", args[0])
}

// ─── Channel ────────────────────────────────────────────────────

func (s *consoleState) channelList(args []string) {
	if !s.requireTenant() { return }
	page, err := s.getStores().channels.Select(s.ctx, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	items := page.Items
	if len(items) == 0 {
		fmt.Println("No notification channels found.")
		return
	}
	fmt.Printf("%-38s %-24s %-12s %s\n", "ID", "NAME", "TYPE", "ENABLED")
	fmt.Println(strings.Repeat("-", 90))
	for _, ch := range items {
		fmt.Printf("%-38s %-24s %-12s %v\n", ch.ID, truncate(ch.Name, 24), string(ch.Type), ch.Enabled)
	}
	fmt.Printf("(%d channels)\n", len(items))
}

func (s *consoleState) channelGet(args []string) {
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: channel get <id>")
		return
	}
	ch, err := s.getStores().channels.Get(s.ctx, args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	printJSON(ch)
}

func (s *consoleState) channelAdd(args []string) {
	if !s.requireTenant() { return }
	fmt.Println("Enter channel JSON (end with a line containing only '.'):")
	body := readMultiline(s.rl)
	var ch entities.NotifyChannelInfo
	if err := json.Unmarshal([]byte(body), &ch); err != nil {
		fmt.Printf("Invalid JSON: %v\n", err)
		return
	}
	ch.ID = uuid.New().String()
	ch.TenantID = s.tenant
	ch.CreatedAt = time.Now()
	ch.UpdatedAt = time.Now()
	if err := s.getStores().channels.Save(s.ctx, &ch); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Created channel: %s\n", ch.ID)
}

func (s *consoleState) channelRemove(args []string) {
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: channel remove <id>")
		return
	}
	if err := s.getStores().channels.Delete(s.ctx, args[0]); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Deleted channel: %s\n", args[0])
}

// ─── Agent ──────────────────────────────────────────────────────

func (s *consoleState) agentList(args []string) {
	if !s.requireTenant() { return }
	page, err := s.getStores().agents.Select(s.ctx, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	items := page.Items
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
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: agent get <name>")
		return
	}
	a, err := s.getStores().agents.Get(s.ctx, args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	printJSON(a)
}

func (s *consoleState) agentAdd(args []string) {
	if !s.requireTenant() { return }
	fmt.Println("Enter agent JSON (end with a line containing only '.'):")
	body := readMultiline(s.rl)
	var a entities.AgentInfo
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		fmt.Printf("Invalid JSON: %v\n", err)
		return
	}
	a.TenantID = s.tenant
	a.CreatedAt = time.Now()
	a.UpdatedAt = time.Now()
	if err := s.getStores().agents.Save(s.ctx, &a); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Created agent: %s\n", a.Name)
}

func (s *consoleState) agentRemove(args []string) {
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: agent remove <name>")
		return
	}
	if err := s.getStores().agents.Delete(s.ctx, args[0]); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Deleted agent: %s\n", args[0])
}

// ─── MCP ────────────────────────────────────────────────────────

func (s *consoleState) mcpList(args []string) {
	if !s.requireTenant() { return }
	page, err := s.getStores().mcps.Select(s.ctx, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	items := page.Items
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
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: mcp get <name>")
		return
	}
	m, err := s.getStores().mcps.Get(s.ctx, args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	printJSON(m)
}

func (s *consoleState) mcpAdd(args []string) {
	if !s.requireTenant() { return }
	fmt.Println("Enter MCP JSON (end with a line containing only '.'):")
	body := readMultiline(s.rl)
	var m entities.McpInfo
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		fmt.Printf("Invalid JSON: %v\n", err)
		return
	}
	m.TenantID = s.tenant
	m.CreatedAt = time.Now()
	m.UpdatedAt = time.Now()
	if err := s.getStores().mcps.Save(s.ctx, &m); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Created MCP: %s\n", m.Name)
}

func (s *consoleState) mcpRemove(args []string) {
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: mcp remove <name>")
		return
	}
	if err := s.getStores().mcps.Delete(s.ctx, args[0]); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Deleted MCP: %s\n", args[0])
}

// ─── Skill ──────────────────────────────────────────────────────

func (s *consoleState) skillList(args []string) {
	if !s.requireTenant() { return }
	page, err := s.getStores().flows.Select(s.ctx, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	var skills []*entities.AgentFlowVersionInfo
	for _, f := range page.Items {
		spec, _ := s.getStores().flows.GetSpec(s.ctx, f.AgentFlowID)
		if spec != nil && spec.Kind == "skill" {
			skills = append(skills, f)
		}
	}
	if len(skills) == 0 {
		fmt.Println("No skills found.")
		return
	}
	fmt.Printf("%-38s %-8s %s\n", "ID", "VERSION", "CREATED")
	fmt.Println(strings.Repeat("-", 80))
	for _, sk := range skills {
		fmt.Printf("%-38s %-8d %s\n", sk.AgentFlowID, sk.Version, sk.CreatedAt.Format("2006-01-02 15:04"))
	}
	fmt.Printf("(%d skills)\n", len(skills))
}

func (s *consoleState) skillGet(args []string) {
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: skill get <id>")
		return
	}
	spec, err := s.getStores().flows.GetSpec(s.ctx, args[0])
	if err != nil || spec == nil {
		fmt.Printf("Skill not found: %s\n", args[0])
		return
	}
	printJSON(spec)
}

func (s *consoleState) skillAdd(args []string) {
	if !s.requireTenant() { return }
	fmt.Println("Enter skill JSON (end with a line containing only '.'):")
	body := readMultiline(s.rl)
	var spec entities.AgentFlowInfo
	if err := json.Unmarshal([]byte(body), &spec); err != nil {
		fmt.Printf("Invalid JSON: %v\n", err)
		return
	}
	spec.Kind = "skill"
	if spec.ID == "" {
		spec.ID = uuid.New().String()
	}
	if err := s.getStores().flows.SaveSpec(s.ctx, &spec, "console", "added via console"); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Created skill: %s\n", spec.ID)
}

func (s *consoleState) skillRemove(args []string) {
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: skill remove <id>")
		return
	}
	if err := s.getStores().flows.Delete(s.ctx, args[0]); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Deleted skill: %s\n", args[0])
}

// ─── Flow ───────────────────────────────────────────────────────

func (s *consoleState) flowList(args []string) {
	if !s.requireTenant() { return }
	page, err := s.getStores().flows.Select(s.ctx, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	items := page.Items
	if len(items) == 0 {
		fmt.Println("No agentflows found.")
		return
	}
	fmt.Printf("%-38s %-8s %s\n", "ID", "VERSION", "CREATED")
	fmt.Println(strings.Repeat("-", 80))
	for _, f := range items {
		fmt.Printf("%-38s %-8d %s\n", f.AgentFlowID, f.Version, f.CreatedAt.Format("2006-01-02 15:04"))
	}
	fmt.Printf("(%d flows)\n", len(items))
}

func (s *consoleState) flowGet(args []string) {
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: flow get <id>")
		return
	}
	spec, err := s.getStores().flows.GetSpec(s.ctx, args[0])
	if err != nil || spec == nil {
		fmt.Printf("Flow not found: %s\n", args[0])
		return
	}
	printJSON(spec)
}

func (s *consoleState) flowAdd(args []string) {
	if !s.requireTenant() { return }
	fmt.Println("Enter agentflow JSON (end with a line containing only '.'):")
	body := readMultiline(s.rl)
	var spec entities.AgentFlowInfo
	if err := json.Unmarshal([]byte(body), &spec); err != nil {
		fmt.Printf("Invalid JSON: %v\n", err)
		return
	}
	if spec.ID == "" {
		spec.ID = uuid.New().String()
	}
	if err := s.getStores().flows.SaveSpec(s.ctx, &spec, "console", "added via console"); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Created flow: %s\n", spec.ID)
}

func (s *consoleState) flowRemove(args []string) {
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: flow remove <id>")
		return
	}
	if err := s.getStores().flows.Delete(s.ctx, args[0]); err != nil {
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
	if !s.requireTenant() { return }
	page, err := s.getStores().runs.Select(s.ctx, entities.PageRequest{Page: 1, Size: 50})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	items := page.Items
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
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: run get <id>")
		return
	}
	run, err := s.getStores().runs.Get(s.ctx, args[0])
	if err != nil || run == nil {
		fmt.Printf("Run not found: %s\n", args[0])
		return
	}
	printJSON(run)
}

func (s *consoleState) runCreate(args []string) {
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: run create <flow-id>")
		return
	}
	flowID := args[0]
	run := &entities.FlowRunInfo{
		BaseEntity:  entities.BaseEntity{ID: uuid.New().String(), TenantID: s.tenant, CreatedAt: time.Now()},
		AgentFlowID: flowID,
		Status:      entities.RunPending,
	}
	if err := s.getStores().runs.Create(s.ctx, run); err != nil {
		fmt.Printf("Error creating run: %v\n", err)
		return
	}
	fmt.Printf("Created run: %s (status: PENDING)\n", run.ID)
	fmt.Printf("Use 'run start %s' to trigger execution.\n", run.ID)
}

func (s *consoleState) runStart(args []string) {
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: run start <run-id>")
		return
	}
	run, err := s.getStores().runs.Get(s.ctx, args[0])
	if err != nil || run == nil {
		fmt.Printf("Run not found: %s\n", args[0])
		return
	}
	run.Status = entities.RunRunning
	run.StartedAt = timePtr(time.Now())
	if err := s.getStores().runs.Update(s.ctx, run); err != nil {
		fmt.Printf("Error starting run: %v\n", err)
		return
	}
	fmt.Printf("Run %s started.\n", run.ID)
}

func (s *consoleState) runStop(args []string) {
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: run stop <run-id>")
		return
	}
	if err := s.getStores().runs.Cancel(s.ctx, args[0]); err != nil {
		fmt.Printf("Error stopping run: %v\n", err)
		return
	}
	fmt.Printf("Run %s cancelled.\n", args[0])
}

// ─── Wallet ─────────────────────────────────────────────────────

func (s *consoleState) walletList(args []string) {
	if !s.requireTenant() { return }
	if s.secretStore == nil {
		fmt.Println("Error: wallet secret store not available. Check master key configuration.")
		return
	}
	keys, err := s.secretStore.ListSecrets(s.ctx, "wallet:")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if len(keys) == 0 {
		fmt.Println("No wallets found.")
		return
	}
	fmt.Printf("%-48s %s\n", "WALLET NAME", "KEY (prefix)")
	fmt.Println(strings.Repeat("-", 80))
	for _, k := range keys {
		walletName := strings.TrimPrefix(k, "wallet:")
		keyBytes, err := s.secretStore.GetSecret(s.ctx, k)
		if err != nil {
			continue
		}
		privKey := ed25519.PrivateKey(keyBytes)
		pubKey := privKey.Public().(ed25519.PublicKey)
		addr := "0x" + hex.EncodeToString(pubKey)
		fmt.Printf("%-48s %s\n", walletName, addr)
	}
	fmt.Printf("(%d wallets)\n", len(keys))
}

func (s *consoleState) walletGet(args []string) {
	if !s.requireTenant() { return }
	if s.secretStore == nil {
		fmt.Println("Error: wallet secret store not available.")
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: wallet get <name>")
		return
	}
	keyBytes, err := s.secretStore.GetSecret(s.ctx, "wallet:"+args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	privKey := ed25519.PrivateKey(keyBytes)
	pubKey := privKey.Public().(ed25519.PublicKey)
	printJSON(map[string]string{
		"name":    args[0],
		"address": "0x" + hex.EncodeToString(pubKey),
	})
}

func (s *consoleState) walletAdd(args []string) {
	if !s.requireTenant() { return }
	if s.secretStore == nil {
		fmt.Println("Error: wallet secret store not available.")
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: wallet add <name> [hex-private-key]")
		return
	}
	name := args[0]
	var privKey ed25519.PrivateKey
	if len(args) >= 2 {
		// Import existing key
		keyBytes, err := hex.DecodeString(args[1])
		if err != nil {
			fmt.Printf("Invalid hex private key: %v\n", err)
			return
		}
		if len(keyBytes) != ed25519.PrivateKeySize {
			fmt.Printf("Invalid key length: got %d, want %d\n", len(keyBytes), ed25519.PrivateKeySize)
			return
		}
		privKey = ed25519.PrivateKey(keyBytes)
	} else {
		// Generate new keypair
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fmt.Printf("Error generating key: %v\n", err)
			return
		}
		privKey = priv
		_ = pub
	}
	if err := s.secretStore.PutSecret(s.ctx, "wallet:"+name, []byte(privKey)); err != nil {
		fmt.Printf("Error storing key: %v\n", err)
		return
	}
	pubKey := privKey.Public().(ed25519.PublicKey)
	fmt.Printf("Wallet created: %s\n", name)
	fmt.Printf("Address: 0x%s\n", hex.EncodeToString(pubKey))
	if len(args) < 2 {
		fmt.Printf("Private key: %s\n", hex.EncodeToString(privKey))
		fmt.Println("Save this private key securely — it cannot be recovered.")
	}
}

func (s *consoleState) walletRemove(args []string) {
	if !s.requireTenant() { return }
	if s.secretStore == nil {
		fmt.Println("Error: wallet secret store not available.")
		return
	}
	if len(args) < 1 {
		fmt.Println("Usage: wallet remove <name>")
		return
	}
	if err := s.secretStore.DeleteSecret(s.ctx, "wallet:"+args[0]); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Deleted wallet: %s\n", args[0])
}

// ─── Import/Export ──────────────────────────────────────────────

func (s *consoleState) cmdExport(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: export <filepath>")
		fmt.Println("  Format is auto-detected from extension (.json, .yaml, .yml)")
		return
	}
	filePath := args[0]
	data, err := s.exportData()
	if err != nil {
		fmt.Printf("Error collecting data: %v\n", err)
		return
	}
	fm, err := detectFormat(filePath, args[1:])
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
	if err := os.WriteFile(filePath, out, 0644); err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		return
	}
	fmt.Printf("Exported to %s (%d llms, %d channels, %d mcps, %d skills, %d agents, %d flows, %d runs, %d wallets)\n",
		filePath, len(data.LLMs), len(data.Channels), len(data.MCPs), len(data.Skills),
		len(data.AgentDefs), len(data.AgentFlows), len(data.FlowRuns), len(data.Wallets))
}

func (s *consoleState) cmdImport(args []string) {
	if !s.requireTenant() { return }
	if len(args) < 1 {
		fmt.Println("Usage: import <filepath>")
		return
	}
	filePath := args[0]
	fm, err := detectFormat(filePath, args[1:])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	raw, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		return
	}
	var data ExportData
	switch fm {
	case "json":
		err = json.Unmarshal(raw, &data)
	case "yaml":
		err = yaml.Unmarshal(raw, &data)
	}
	if err != nil {
		fmt.Printf("Error parsing %s: %v\n", fm, err)
		return
	}
	counts, err := s.importData(&data)
	if err != nil {
		fmt.Printf("Error importing: %v\n", err)
		return
	}
	fmt.Printf("Imported from %s (%d llms, %d channels, %d mcps, %d skills, %d agents, %d flows, %d runs, %d wallets)\n",
		filePath, counts[0], counts[1], counts[2], counts[3], counts[4], counts[5], counts[6], counts[7])
}

func (s *consoleState) exportData() (*ExportData, error) {
	ls := s.getStores()
	data := &ExportData{}

	// LLMs
	if page, err := ls.llm.Select(s.ctx, entities.PageRequest{Page: 1, Size: 10000}); err == nil {
		for _, p := range page.Items {
			if p != nil {
				data.LLMs = append(data.LLMs, *p)
			}
		}
	}
	// Channels
	if page, err := ls.channels.Select(s.ctx, entities.PageRequest{Page: 1, Size: 10000}); err == nil {
		for _, ch := range page.Items {
			if ch != nil {
				data.Channels = append(data.Channels, *ch)
			}
		}
	}
	// MCPs
	if page, err := ls.mcps.Select(s.ctx, entities.PageRequest{Page: 1, Size: 10000}); err == nil {
		for _, m := range page.Items {
			if m != nil {
				data.MCPs = append(data.MCPs, *m)
			}
		}
	}
	// AgentDefs
	if page, err := ls.agents.Select(s.ctx, entities.PageRequest{Page: 1, Size: 10000}); err == nil {
		for _, a := range page.Items {
			if a != nil {
				data.AgentDefs = append(data.AgentDefs, *a)
			}
		}
	}
	// Skills & AgentFlows from flow store (separate by Kind)
	if page, err := ls.flows.Select(s.ctx, entities.PageRequest{Page: 1, Size: 10000}); err == nil {
		for _, fv := range page.Items {
			if fv == nil {
				continue
			}
			spec, _ := ls.flows.GetSpec(s.ctx, fv.AgentFlowID)
			if spec == nil {
				continue
			}
			if spec.Kind == "skill" {
				data.Skills = append(data.Skills, *spec)
			} else {
				data.AgentFlows = append(data.AgentFlows, *spec)
			}
		}
	}
	// FlowRuns
	if page, err := ls.runs.Select(s.ctx, entities.PageRequest{Page: 1, Size: 10000}); err == nil {
		for _, r := range page.Items {
			if r != nil {
				data.FlowRuns = append(data.FlowRuns, *r)
			}
		}
	}
	// Wallets
	if s.secretStore != nil {
		if keys, err := s.secretStore.ListSecrets(s.ctx, "wallet:"); err == nil {
			for _, k := range keys {
				name := strings.TrimPrefix(k, "wallet:")
				keyBytes, err := s.secretStore.GetSecret(s.ctx, k)
				if err != nil {
					continue
				}
				data.Wallets = append(data.Wallets, WalletExport{
					Name:       name,
					PrivateKey: hex.EncodeToString(keyBytes),
				})
			}
		}
	}
	return data, nil
}

func (s *consoleState) importData(data *ExportData) ([]int, error) {
	ls := s.getStores()
	counts := make([]int, 8)

	for i := range data.LLMs {
		llm := &data.LLMs[i]
		llm.TenantID = s.tenant
		llm.CreatedAt = time.Now()
		llm.UpdatedAt = time.Now()
		if llm.ID == "" {
			llm.ID = uuid.New().String()
		}
		if err := ls.llm.Save(s.ctx, llm); err != nil {
			return counts, fmt.Errorf("llm %s: %w", llm.ID, err)
		}
		counts[0]++
	}
	for i := range data.Channels {
		ch := &data.Channels[i]
		ch.TenantID = s.tenant
		ch.CreatedAt = time.Now()
		ch.UpdatedAt = time.Now()
		if ch.ID == "" {
			ch.ID = uuid.New().String()
		}
		if err := ls.channels.Save(s.ctx, ch); err != nil {
			return counts, fmt.Errorf("channel %s: %w", ch.ID, err)
		}
		counts[1]++
	}
	for i := range data.MCPs {
		m := &data.MCPs[i]
		m.TenantID = s.tenant
		m.CreatedAt = time.Now()
		m.UpdatedAt = time.Now()
		if m.ID == "" {
			m.ID = uuid.New().String()
		}
		if err := ls.mcps.Save(s.ctx, m); err != nil {
			return counts, fmt.Errorf("mcp %s: %w", m.Name, err)
		}
		counts[2]++
	}
	for _, spec := range data.Skills {
		sp := spec
		sp.Kind = "skill"
		if sp.ID == "" {
			sp.ID = uuid.New().String()
		}
		sp.TenantID = s.tenant
		if err := ls.flows.SaveSpec(s.ctx, &sp, "import", "imported from file"); err != nil {
			return counts, fmt.Errorf("skill %s: %w", sp.ID, err)
		}
		counts[3]++
	}
	for i := range data.AgentDefs {
		a := &data.AgentDefs[i]
		a.TenantID = s.tenant
		a.CreatedAt = time.Now()
		a.UpdatedAt = time.Now()
		if a.ID == "" {
			a.ID = uuid.New().String()
		}
		if err := ls.agents.Save(s.ctx, a); err != nil {
			return counts, fmt.Errorf("agent %s: %w", a.Name, err)
		}
		counts[4]++
	}
	for _, spec := range data.AgentFlows {
		sp := spec
		if sp.ID == "" {
			sp.ID = uuid.New().String()
		}
		sp.TenantID = s.tenant
		if sp.Kind == "" {
			sp.Kind = "flow"
		}
		if err := ls.flows.SaveSpec(s.ctx, &sp, "import", "imported from file"); err != nil {
			return counts, fmt.Errorf("flow %s: %w", sp.ID, err)
		}
		counts[5]++
	}
	for i := range data.FlowRuns {
		run := &data.FlowRuns[i]
		run.TenantID = s.tenant
		run.CreatedAt = time.Now()
		run.UpdatedAt = time.Now()
		if run.ID == "" {
			run.ID = uuid.New().String()
		}
		if err := ls.runs.Create(s.ctx, run); err != nil {
			return counts, fmt.Errorf("run %s: %w", run.ID, err)
		}
		counts[6]++
	}
	// Wallets
	if len(data.Wallets) > 0 && s.secretStore == nil {
		fmt.Println("Warning: secret store not available, skipping wallet import.")
	}
	for _, w := range data.Wallets {
		if s.secretStore == nil {
			break
		}
		keyBytes, err := hex.DecodeString(w.PrivateKey)
		if err != nil {
			return counts, fmt.Errorf("wallet %s: invalid hex key: %w", w.Name, err)
		}
		if err := s.secretStore.PutSecret(s.ctx, "wallet:"+w.Name, keyBytes); err != nil {
			return counts, fmt.Errorf("wallet %s: %w", w.Name, err)
		}
		counts[7]++
	}
	return counts, nil
}

func detectFormat(filePath string, args []string) (string, error) {
	// Check for --format flag
	for i, a := range args {
		if a == "--format" && i+1 < len(args) {
			f := strings.ToLower(args[i+1])
			if f == "json" || f == "yaml" || f == "yml" {
				if f == "yml" {
					f = "yaml"
				}
				return f, nil
			}
			return "", fmt.Errorf("unknown format: %s (use json or yaml)", args[i+1])
		}
	}
	// Auto-detect from extension
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".json":
		return "json", nil
	case ".yaml", ".yml":
		return "yaml", nil
	default:
		return "", fmt.Errorf("cannot determine format from extension %q, use --format json|yaml", ext)
	}
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

func timePtr(t time.Time) *time.Time { return &t }
