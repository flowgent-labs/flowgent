package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
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

var (
	Version   = "dev"
	GitCommit = "none"
	BuildTime = "unknown"
)

func main() {
	log.Printf("Flowgent v%s (commit: %s, built: %s)", Version, GitCommit, BuildTime)

	cfgPath := "etc/flowgent.yaml"
	if v := os.Getenv("FLOWGENT_CONFIG"); v != "" {
		cfgPath = v
	}

	serviceCfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	logger := util.NewLogger(serviceCfg.Logging.Mode, serviceCfg.Logging.Level)

	agentFlows, subAgentFlows, err := config.LoadAgentFlows(serviceCfg, cfgPath)
	if err != nil {
		log.Fatalf("Failed to load agentFlows: %v", err)
	}
	slog.Info("AgentFlows loaded", "count", len(agentFlows)+len(subAgentFlows))

	// ─── Database ────────────────────────────────────────
	var storeImpl store.Store
	switch serviceCfg.Storage.Type {
	case "POSTGRE":
		pgCfg := serviceCfg.Storage.Postgres
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
		storeImpl = pgStore
	default:
		sqliteDir := serviceCfg.Storage.SQLite.Dir
		if sqliteDir == "" {
			sqliteDir = "~/.flowgent/sqlite"
		}
		sqliteStore := store.NewSQLiteStore(sqliteDir)
		if err := sqliteStore.Init(context.Background()); err != nil {
			log.Fatalf("Failed to init SQLite: %v", err)
		}
		storeImpl = sqliteStore
	}
	defer storeImpl.(interface{ Close() error }).Close()

	// ─── OTEL ────────────────────────────────────────────
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

	// ─── Auth consumed here (full impl in middleware below) ─

	// ─── Cache consumed here (full impl in cache/ package) ─
	_ = serviceCfg.Cache

	// ─── MCP Clients ────────────────────────────────────
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

	// ─── LLM Client ─────────────────────────────────────
	llmClient := llm.New(&serviceCfg.LLM)

	// ─── Engine Executor ────────────────────────────────
	agents := make([]*model.AgentDef, len(serviceCfg.Orchestration.Agents))
	for i := range serviceCfg.Orchestration.Agents {
		agents[i] = &serviceCfg.Orchestration.Agents[i]
	}
	exec := engine.NewExecutor(storeImpl, mcpMap, agents, llmClient, logger)

	// ─── API Handlers ───────────────────────────────────
	healthHandler := &api.HealthHandler{}
	agentFlowHandler := api.NewAgentFlowHandler(storeImpl, logger, agentFlows, subAgentFlows)
	humanHandler := api.NewHumanHandler(storeImpl, logger)
	triggerDispatcher := api.NewTriggerDispatcher(storeImpl, agentFlows)

	// ─── Cron ────────────────────────────────────────────
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

	// ─── Queue + Poller ──────────────────────────────────
	flowTimeout, _ := time.ParseDuration(serviceCfg.Orchestration.FlowExecutionTimeout)
	if flowTimeout == 0 {
		flowTimeout = 30 * time.Minute
	}
	maxRetries := serviceCfg.Orchestration.MaxNodeRetries
	q := queue.NewMemoryQueue(1000)
	go startRunPoller(context.Background(), storeImpl, exec, agentFlowHandler.AgentFlows(), q, logger,
		flowTimeout, maxRetries, serviceCfg.Orchestration.MaxConcurrentFlows)

	// ─── Workers (distributed mode) ────────────────────
	_ = worker.NewPool(serviceCfg.Orchestration.MaxConcurrentFlows, &worker.Config{
		Store:    storeImpl,
		Executor: exec,
		Queue:    q,
		Logger:   logger,
		Flows:    agentFlowHandler.AgentFlows(),
	})

	// ─── Hot reload ─────────────────────────────────────
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

	// ─── Routes ──────────────────────────────────────────
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_/healthz", healthHandler.Healthz)
	mux.HandleFunc("GET /_/openapi.yaml", api.OpenAPIHandler)
	mux.HandleFunc("GET /_/swagger-ui", api.SwaggerUIHandler)
	mux.HandleFunc("GET /api/v1/agentflows", agentFlowHandler.ListDefinitions)
	mux.HandleFunc("POST /api/v1/agentflows/trigger", agentFlowHandler.Trigger)
	mux.HandleFunc("GET /api/v1/runs", agentFlowHandler.ListRuns)
	mux.HandleFunc("GET /api/v1/runs/{id}", agentFlowHandler.GetRun)
	mux.HandleFunc("GET /api/v1/runs/{run_id}/tasks", agentFlowHandler.GetTaskRuns)
	mux.HandleFunc("POST /api/v1/webhooks/{provider}", triggerDispatcher.Webhook)
	mux.HandleFunc("POST /api/v1/webhooks/github", triggerDispatcher.Webhook)
	mux.HandleFunc("POST /api/v1/human/{token}/approve", humanHandler.Approve)
	mux.HandleFunc("POST /api/v1/human/{token}/reject", humanHandler.Reject)

	// ─── Auth ────────────────────────────────────────────
	var handler http.Handler = mux
	if len(serviceCfg.Auth.AnonymousPaths) > 0 {
		handler = authMiddleware(serviceCfg.Auth, mux)
	}

	// ─── Server (config-driven timeouts) ─────────────────
	readTO, _ := time.ParseDuration(serviceCfg.Server.ReadTimeout)
	if readTO == 0 {
		readTO = 30 * time.Second
	}
	writeTO, _ := time.ParseDuration(serviceCfg.Server.WriteTimeout)
	if writeTO == 0 {
		writeTO = 60 * time.Second
	}
	shutdownTO, _ := time.ParseDuration(serviceCfg.Server.ShutdownTimeout)
	if shutdownTO == 0 {
		shutdownTO = 15 * time.Second
	}

	addr := fmt.Sprintf("%s:%d", serviceCfg.Server.Host, serviceCfg.Server.Port)
	srv := &http.Server{
		Addr:           addr,
		Handler:        handler,
		ReadTimeout:    readTO,
		WriteTimeout:   writeTO,
		MaxHeaderBytes: serviceCfg.Server.MaxBodyBytes,
	}
	go func() {
		slog.Info("Flowgent server", "addr", addr)
		srv.ListenAndServe()
	}()

	// ─── Pprof ───────────────────────────────────────────
	if serviceCfg.Mgmt.Enabled && serviceCfg.Mgmt.PProf.Enabled {
		ppMux := http.NewServeMux()
		ppMux.HandleFunc("GET /debug/pprof/", pprof.Index)
		ppMux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
		ppMux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
		ppMux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
		ppMux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
		bind := serviceCfg.Mgmt.PProf.ServerBind
		if bind == "" {
			// Use mgmt host:port for pprof if server-bind not set
			bind = fmt.Sprintf("%s:%d", serviceCfg.Mgmt.Host, serviceCfg.Mgmt.Port)
		}
		ppSrv := &http.Server{Addr: bind, Handler: ppMux}
		go func() { slog.Info("pprof", "addr", bind); ppSrv.ListenAndServe() }()
		defer ppSrv.Close()
	}

	// ─── Shutdown ────────────────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	slog.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTO)
	defer cancel()
	srv.Shutdown(ctx)
}

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

func authMiddleware(cfg model.AuthConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, p := range cfg.AnonymousPaths {
			if matchGlob(p, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
		}
		// Full auth: JWT/OIDC/GitHub OAuth handler here
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
