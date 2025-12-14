package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/flowgent-labs/flowgent/src/api"
	"github.com/flowgent-labs/flowgent/src/config"
	"github.com/flowgent-labs/flowgent/src/engine"
	"github.com/flowgent-labs/flowgent/src/llm"
	"github.com/flowgent-labs/flowgent/src/mcp"
	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/queue"
	"github.com/flowgent-labs/flowgent/src/store"
	"github.com/flowgent-labs/flowgent/src/worker"
	"github.com/flowgent-labs/flowgent/src/util"
)

// stopByPID reads a PID file and sends SIGTERM to the process.
func stopByPID(pidFile string) error {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("read PID file %s: %w (is the service running?)", pidFile, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("invalid PID file %s: %w", pidFile, err)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d not found: %w", pid, err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM to %d: %w", pid, err)
	}
	fmt.Printf("Sent SIGTERM to process %d (pidfile=%s)\n", pid, pidFile)
	os.Remove(pidFile)
	return nil
}

// daemonProcess handles daemon start/stop/restart actions.
func daemonProcess(action, pidFile string) error {
	switch action {
	case "start":
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
			return fmt.Errorf("write PID file %s: %w", pidFile, err)
		}
		defer os.Remove(pidFile)
		log.Printf("Flowgent daemon starting (pid=%d, pidfile=%s)", os.Getpid(), pidFile)
		startServer("all")
		return nil
	case "stop":
		return stopByPID(pidFile)
	case "restart":
		_ = stopByPID(pidFile) // best-effort stop
		time.Sleep(500 * time.Millisecond)
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
			return fmt.Errorf("write PID file %s: %w", pidFile, err)
		}
		defer os.Remove(pidFile)
		log.Printf("Flowgent daemon restarting (pid=%d, pidfile=%s)", os.Getpid(), pidFile)
		startServer("all")
		return nil
	default:
		return fmt.Errorf("unknown daemon action: %s", action)
	}
}

// serverProcess handles start/stop/restart for individual server components.
func serverProcess(name, action, pidFile, mode string) error {
	switch action {
	case "start":
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
			return fmt.Errorf("write PID file %s: %w", pidFile, err)
		}
		defer os.Remove(pidFile)
		log.Printf("Flowgent %s starting (pid=%d, pidfile=%s)", name, os.Getpid(), pidFile)
		startServer(mode)
		return nil
	case "stop":
		return stopByPID(pidFile)
	case "restart":
		_ = stopByPID(pidFile)
		time.Sleep(500 * time.Millisecond)
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
			return fmt.Errorf("write PID file %s: %w", pidFile, err)
		}
		defer os.Remove(pidFile)
		log.Printf("Flowgent %s restarting (pid=%d, pidfile=%s)", name, os.Getpid(), pidFile)
		startServer(mode)
		return nil
	default:
		return fmt.Errorf("unknown %s action: %s", name, action)
	}
}

// runDaemon handles daemon start/stop/restart.
func runDaemon(action, pidFile string) error {
	return daemonProcess(action, pidFile)
}

// runAPIServer handles apiserver start/stop/restart.
func runAPIServer(action, pidFile string) error {
	return serverProcess("apiserver", action, pidFile, "api")
}

// runA2AServer handles a2a start/stop/restart.
func runA2AServer(action, pidFile string) error {
	return serverProcess("a2a", action, pidFile, "a2a")
}

// startServer initialises all subsystems and starts the REST and A2A HTTP servers.
func startServer(mode string) {
	if verbose {
		log.Printf("Flowgent v%s (commit: %s, built: %s)", Version, GitCommit, BuildTime)
		log.Printf("Config path: %s", cfgPath)
		if v := os.Getenv("FLOWGENT_CONFIG_FILE"); v != "" {
			log.Printf("Config env:  FLOWGENT_CONFIG_FILE=%s", v)
		} else {
			log.Printf("Config env:  FLOWGENT_CONFIG_FILE (not set, using default)")
		}
	} else {
		log.Printf("Flowgent v%s (commit: %s, built: %s)", Version, GitCommit, BuildTime)
	}

	serviceCfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if verbose {
		logConfig(serviceCfg)
	}

	logger := util.NewLogger(serviceCfg.Logging.Mode, serviceCfg.Logging.Level)

	agentFlows, subAgentFlows, err := config.LoadAgentFlows(serviceCfg, cfgPath)
	if err != nil {
		log.Fatalf("Failed to load agentFlows: %v", err)
	}
	slog.Info("AgentFlows loaded", "count", len(agentFlows)+len(subAgentFlows))

	// ── A2A Agent Card ──────────────────────────────────
	// The agent card URL points to the A2A port (or REST port if A2A disabled).
	a2aAddr := fmt.Sprintf("%s:%d", serviceCfg.A2A.Host, serviceCfg.A2A.Port)
	if !serviceCfg.A2A.Enabled {
		a2aAddr = fmt.Sprintf("%s:%d", serviceCfg.Server.Host, serviceCfg.Server.Port)
	}
	agentCard := map[string]any{
		"name":        serviceCfg.ServiceName,
		"description": "Flowgent autonomous agentflow orchestration engine. Accepts agentflow IDs and input variables, returns execution results.",
		"url":         fmt.Sprintf("http://%s", a2aAddr),
		"version":     Version,
		"capabilities": map[string]any{
			"streaming":         false,
			"pushNotifications": false,
		},
		"skills": []map[string]any{
			{"id": "run_agentflow", "description": "Execute an agentflow by ID with input variables"},
			{"id": "query_run", "description": "Query the status of an agentflow run"},
			{"id": "list_agentflows", "description": "List all available agentflow definitions"},
		},
	}

	// ── Database ────────────────────────────────────────
	storeImpl := initStore(serviceCfg)
	defer storeImpl.(interface{ Close() error }).Close()

	// ── OTEL ────────────────────────────────────────────
	if serviceCfg.Mgmt.OTEL.Enabled {
		endpoint := serviceCfg.Mgmt.OTEL.Endpoint
		if endpoint == "" {
			endpoint = "localhost:4317"
		}
		oc, err := newOTEL(serviceCfg.ServiceName, Version, endpoint, serviceCfg.Mgmt.OTEL.Timeout)
		if err != nil {
			slog.Warn("OTEL initialization failed", "error", err)
		} else {
			slog.Info("OTEL telemetry initialized", "endpoint", endpoint)
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				oc.Shutdown(ctx)
			}()
		}
	}

	// ── MCP Clients ────────────────────────────────────
	mcpFactory := mcp.NewFactory()
	for _, mcpDef := range serviceCfg.Orchestration.MCPs {
		if mcpDef.Enabled {
			mcpFactory.Register(mcpDef.Name, mcpDef.Command, mcpDef.Args, mcpDef.Env)
		}
	}
	mcpMap := make(map[string]engine.MCPClient)
	for _, mcpDef := range serviceCfg.Orchestration.MCPs {
		if mcpDef.Enabled {
			mcpMap[mcpDef.Name] = &mcpAdapter{factory: mcpFactory, name: mcpDef.Name}
		}
	}

	// ── LLM Client ─────────────────────────────────────
	llmClient := llm.New(&serviceCfg.LLM)

	// ── Engine Executor ────────────────────────────────
	agents := make([]*config.AgentDef, len(serviceCfg.Orchestration.Agents))
	for i := range serviceCfg.Orchestration.Agents {
		agents[i] = &serviceCfg.Orchestration.Agents[i]
	}
	exec := engine.NewExecutor(storeImpl, mcpMap, agents, llmClient, logger)

	// ── API Handlers ───────────────────────────────────
	healthHandler := &api.HealthHandler{}
	agentFlowHandler := api.NewAgentFlowHandler(storeImpl, logger, agentFlows, subAgentFlows)
	humanHandler := api.NewHumanHandler(storeImpl, logger)
	triggerDispatcher := api.NewTriggerDispatcher(storeImpl, agentFlows)

	// ── Cron ───────────────────────────────────────────
	cronSched := engine.NewScheduleTrigger()
	triggerFunc := func(ctx context.Context, id string) {
		run := &model.AgentFlowRun{AgentFlowID: id, Version: 1, Status: model.RunPending,
			Trigger: model.TriggerInfo{Type: "schedule", Source: "cron"}}
		if err := storeImpl.CreateAgentFlowRun(ctx, run); err != nil {
			slog.Error("schedule trigger failed", "error", err)
		}
	}
	var allSpecs []model.AgentFlowSpec
	allSpecs = append(allSpecs, agentFlows...)
	for _, w := range subAgentFlows {
		allSpecs = append(allSpecs, w)
	}
	cronSched.RegisterAgentFlows(allSpecs, triggerFunc)
	cronSched.Start()
	defer cronSched.Stop()

	// ── Queue + Poller ─────────────────────────────────
	flowTimeout, _ := time.ParseDuration(serviceCfg.Orchestration.FlowExecutionTimeout)
	if flowTimeout == 0 {
		flowTimeout = 30 * time.Minute
	}
	maxRetries := serviceCfg.Orchestration.MaxNodeRetries
	q := queue.NewMemoryQueue(1000)
	go startRunPoller(context.Background(), storeImpl, exec, agentFlowHandler.AgentFlows(), q, logger,
		flowTimeout, maxRetries, serviceCfg.Orchestration.MaxConcurrentFlows)

	// ── Workers (distributed mode) ─────────────────────
	_ = worker.NewPool(serviceCfg.Orchestration.MaxConcurrentFlows, &worker.Config{
		Store:    storeImpl,
		Executor: exec,
		Queue:    q,
		Logger:   logger,
		Flows:    agentFlowHandler.AgentFlows(),
	})

	// ── Hot reload ─────────────────────────────────────
	if refreshStr := serviceCfg.Orchestration.AgentFlows.Static.Refresh; refreshStr != "" {
		if d, err := time.ParseDuration(refreshStr); err == nil && d > 0 {
			go func() {
				t := time.NewTicker(d)
				defer t.Stop()
				for range t.C {
					nf, nsf, _ := config.ReloadAgentFlows(serviceCfg, cfgPath)
					agentFlowHandler.Reload(nf, nsf)
					triggerDispatcher.Reload(nf)
				}
			}()
		}
	}

	// ── Shutdown timeout ───────────────────────────────
	shutdownTO, _ := time.ParseDuration(serviceCfg.Server.ShutdownTimeout)
	if shutdownTO == 0 {
		shutdownTO = 15 * time.Second
	}

	// ── REST API Server ────────────────────────────────
	restMux := http.NewServeMux()
	restMux.HandleFunc("GET /_/healthz", healthHandler.Healthz)
	restMux.HandleFunc("GET /_/openapi.yaml", api.OpenAPIHandler)
	restMux.HandleFunc("GET /_/swagger-ui", api.SwaggerUIHandler)
	restMux.HandleFunc("GET /api/v1/agentflows", agentFlowHandler.ListDefinitions)
	restMux.HandleFunc("POST /api/v1/agentflows/trigger", agentFlowHandler.Trigger)
	restMux.HandleFunc("GET /api/v1/runs", agentFlowHandler.ListRuns)
	restMux.HandleFunc("GET /api/v1/runs/{id}", agentFlowHandler.GetRun)
	restMux.HandleFunc("GET /api/v1/runs/{run_id}/tasks", agentFlowHandler.GetTaskRuns)
	restMux.HandleFunc("POST /api/v1/webhooks/{provider}", triggerDispatcher.Webhook)
	restMux.HandleFunc("POST /api/v1/webhooks/github", triggerDispatcher.Webhook)
	restMux.HandleFunc("POST /api/v1/human/{token}/approve", humanHandler.Approve)
	restMux.HandleFunc("POST /api/v1/human/{token}/reject", humanHandler.Reject)

	var restHandler http.Handler = restMux
	if len(serviceCfg.Auth.AnonymousPaths) > 0 {
		restHandler = authMiddleware(serviceCfg.Auth, restMux)
	}

	readTO, _ := time.ParseDuration(serviceCfg.Server.ReadTimeout)
	if readTO == 0 {
		readTO = 30 * time.Second
	}
	writeTO, _ := time.ParseDuration(serviceCfg.Server.WriteTimeout)
	if writeTO == 0 {
		writeTO = 60 * time.Second
	}

	var restSrv *http.Server
	if mode == "all" || mode == "api" {
		restAddr := fmt.Sprintf("%s:%d", serviceCfg.Server.Host, serviceCfg.Server.Port)
		restSrv = &http.Server{
			Addr:           restAddr,
			Handler:        restHandler,
			ReadTimeout:    readTO,
			WriteTimeout:   writeTO,
			MaxHeaderBytes: serviceCfg.Server.MaxBodyBytes,
		}
		go func() {
			slog.Info("REST API server", "addr", restAddr)
			if err := restSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("REST server: %v", err)
			}
		}()
	}

	// ── A2A API Server (separate port) ─────────────────
	var a2aSrv *http.Server
	if serviceCfg.A2A.Enabled && (mode == "all" || mode == "a2a") {
		a2aMux := http.NewServeMux()
		a2aMux.HandleFunc("GET /.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(agentCard)
		})
		a2aMux.HandleFunc("POST /a2a/tasks", func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Skill       string         `json:"skill"`
				AgentFlowID string         `json:"agentflow_id"`
				Vars        map[string]any `json:"vars,omitempty"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			flows := agentFlowHandler.AgentFlows()
			if _, ok := flows[req.AgentFlowID]; !ok {
				http.Error(w, "agentflow not found", http.StatusNotFound)
				return
			}
			run := &model.AgentFlowRun{
				AgentFlowID: req.AgentFlowID,
				Version:     1,
				Status:      model.RunPending,
				Vars:        req.Vars,
				Trigger:     model.TriggerInfo{Type: "api", Source: "a2a"},
			}
			if err := storeImpl.CreateAgentFlowRun(r.Context(), run); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"task_id":      run.ID,
				"agentflow_id": run.AgentFlowID,
				"status":       run.Status,
			})
		})
		a2aMux.HandleFunc("GET /a2a/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			run, err := storeImpl.GetAgentFlowRun(r.Context(), id)
			if err != nil || run == nil {
				http.Error(w, "run not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"task_id":      run.ID,
				"agentflow_id": run.AgentFlowID,
				"status":       run.Status,
				"output":       run.Output,
				"error":        run.Error,
			})
		})
		// A2A health
		a2aMux.HandleFunc("GET /_/healthz", healthHandler.Healthz)

		a2aAddr2 := fmt.Sprintf("%s:%d", serviceCfg.A2A.Host, serviceCfg.A2A.Port)
		a2aSrv = &http.Server{
			Addr:         a2aAddr2,
			Handler:      a2aMux,
			ReadTimeout:  readTO,
			WriteTimeout: writeTO,
		}
		go func() {
			slog.Info("A2A API server", "addr", a2aAddr2)
			if err := a2aSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("A2A server: %v", err)
			}
		}()
	}

	// ── Pprof ──────────────────────────────────────────
	if serviceCfg.Mgmt.Enabled && serviceCfg.Mgmt.PProf.Enabled {
		ppMux := http.NewServeMux()
		ppMux.HandleFunc("GET /debug/pprof/", pprof.Index)
		ppMux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
		ppMux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
		ppMux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
		ppMux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
		bind := serviceCfg.Mgmt.PProf.ServerBind
		if bind == "" {
			bind = fmt.Sprintf("%s:%d", serviceCfg.Mgmt.Host, serviceCfg.Mgmt.Port)
		}
		ppSrv := &http.Server{Addr: bind, Handler: ppMux}
		go func() { slog.Info("pprof", "addr", bind); ppSrv.ListenAndServe() }()
		defer ppSrv.Close()
	}

	// ── Shutdown ───────────────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	slog.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTO)
	defer cancel()
	if restSrv != nil {
		restSrv.Shutdown(ctx)
	}
	if a2aSrv != nil {
		a2aSrv.Shutdown(ctx)
	}
}

// initStore creates the Store implementation based on config.
func initStore(cfg *config.ServiceConfig) store.Store {
	var s store.Store
	switch cfg.Storage.Type {
	case "POSTGRE":
		pgCfg := cfg.Storage.Postgres
		sslMode := "disable"
		if pgCfg.UseSSL {
			sslMode = "require"
		}
		dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
			pgCfg.Username, pgCfg.Password, pgCfg.Host, pgCfg.Port, pgCfg.Database, sslMode)
		pgStore := store.NewPostgresStore(dsn)
		pgStore.SetPoolConfig(pgCfg.MinConnections, pgCfg.MaxConnections)
		if pgCfg.Schema != "" {
			pgStore.SetSchema(pgCfg.Schema)
		}
		if err := pgStore.Init(context.Background()); err != nil {
			log.Fatalf("Failed to init Postgres: %v", err)
		}
		s = pgStore
	default:
		sqliteDir := cfg.Storage.SQLite.Dir
		if sqliteDir == "" {
			sqliteDir = "~/.flowgent/sqlite"
		}
		sqliteStore := store.NewSQLiteStore(sqliteDir)
		if err := sqliteStore.Init(context.Background()); err != nil {
			log.Fatalf("Failed to init SQLite: %v", err)
		}
		s = sqliteStore
	}
	return s
}

// logConfig prints key configuration details (masks sensitive fields).
func logConfig(cfg *config.ServiceConfig) {
	// Storage
	switch cfg.Storage.Type {
	case "POSTGRE":
		pg := cfg.Storage.Postgres
		log.Printf("Storage:    PostgreSQL host=%s port=%d db=%s schema=%s user=%s pool_min=%d pool_max=%d ssl=%v",
			pg.Host, pg.Port, pg.Database, pg.Schema, pg.Username, pg.MinConnections, pg.MaxConnections, pg.UseSSL)
	default:
		sq := cfg.Storage.SQLite
		dir := sq.Dir
		if dir == "" {
			dir = "~/.flowgent/sqlite"
		}
		log.Printf("Storage:    SQLite dir=%s", dir)
	}

	// Cache
	log.Printf("Cache:      provider=%s", cfg.Cache.Provider)

	// Server bindings
	log.Printf("REST API:   %s:%d (context=%s)", cfg.Server.Host, cfg.Server.Port, cfg.Server.ContextPath)
	if cfg.A2A.Enabled {
		log.Printf("A2A API:    %s:%d", cfg.A2A.Host, cfg.A2A.Port)
	} else {
		log.Printf("A2A API:    disabled")
	}
	if cfg.Mgmt.Enabled {
		log.Printf("Management: %s:%d (pprof=%v, otel=%v)", cfg.Mgmt.Host, cfg.Mgmt.Port, cfg.Mgmt.PProf.Enabled, cfg.Mgmt.OTEL.Enabled)
	}

	// Orchestration
	log.Printf("Engine:     max_concurrent=%d timeout=%s max_retries=%d",
		cfg.Orchestration.MaxConcurrentFlows, cfg.Orchestration.FlowExecutionTimeout, cfg.Orchestration.MaxNodeRetries)

	// LLM providers
	for name, p := range cfg.LLM.Providers {
		models := make([]string, len(p.Models))
		for i, m := range p.Models {
			models[i] = m.Name
		}
		proxy := p.Proxy
		if proxy == "" {
			proxy = "(direct)"
		}
		log.Printf("LLM:        provider=%s endpoint=%s proxy=%s models=%v", name, p.Endpoint, proxy, models)
	}

	// MCP tools
	for _, mcp := range cfg.Orchestration.MCPs {
		if mcp.Enabled {
			log.Printf("MCP:        name=%s type=%s command=%v", mcp.Name, mcp.Type, mcp.Command)
		}
	}
}

// ── Supporting types & functions ──────────────────────────────

type mcpAdapter struct {
	factory *mcp.Factory
	name    string
}

func (a *mcpAdapter) CallTool(ctx context.Context, toolName string, args map[string]any) (map[string]any, error) {
	return a.factory.CallTool(ctx, a.name, toolName, args)
}

func startRunPoller(ctx context.Context, s engine.Store, exec *engine.Executor,
	flows map[string]*model.AgentFlowSpec, q queue.Queue, logger *util.Logger,
	flowTimeout time.Duration, maxRetries int, maxConcurrent int) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	if maxConcurrent <= 0 {
		maxConcurrent = 10
	}
	sem := make(chan struct{}, maxConcurrent)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runs, _ := s.ListAgentFlowRuns(ctx, "", 10)
			for _, run := range runs {
				if run.Status != model.RunPending {
					continue
				}
				spec := flows[run.AgentFlowID]
				if spec == nil {
					continue
				}
				sem <- struct{}{}
				rt := engine.NewAgentFlowRuntime(s, logger)
				rt.SetExecutor(exec)
				rt.SetTimeout(flowTimeout)
				if maxRetries > 0 {
					rt.SetMaxRetries(maxRetries)
				}
				go func(r model.AgentFlowRun, sp *model.AgentFlowSpec) {
					defer func() { <-sem }()
					rt.Execute(ctx, &r, sp)
				}(run, spec)
			}
		}
	}
}

func authMiddleware(cfg config.AuthConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, p := range cfg.AnonymousPaths {
			if matchGlob(p, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func matchGlob(pattern, path string) bool {
	if pattern == path {
		return true
	}
	if len(pattern) > 2 && pattern[len(pattern)-2:] == "/**" {
		pfx := pattern[:len(pattern)-2]
		return len(path) >= len(pfx) && path[:len(pfx)] == pfx
	}
	return false
}
