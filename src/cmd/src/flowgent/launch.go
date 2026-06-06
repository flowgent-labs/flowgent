package main

import (
	"context"
	"database/sql"
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
	"sync"
	"syscall"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/uuid"

	"github.com/flowgent-labs/flowgent/api/src"
	handler "github.com/flowgent-labs/flowgent/api/src/handler"
	"github.com/flowgent-labs/flowgent/common/src/tracing"
	"github.com/flowgent-labs/flowgent/core/src/client"
	"github.com/flowgent-labs/flowgent/common/src/utils"
	"github.com/flowgent-labs/flowgent/config/src/config"
	"github.com/flowgent-labs/flowgent/core/src/engine"
	"github.com/flowgent-labs/flowgent/core/src/engine/discovery"
	"github.com/flowgent-labs/flowgent/core/src/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/core/src/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/core/src/engine/taskmanager"
	"github.com/flowgent-labs/flowgent/core/src/engine/trigger"
	"github.com/flowgent-labs/flowgent/core/src/llm"
	"github.com/flowgent-labs/flowgent/core/src/mcp"
	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/notifier/src"
	messager "github.com/flowgent-labs/flowgent/messager/src"
	"github.com/flowgent-labs/flowgent/store/src"
	"github.com/flowgent-labs/flowgent/store/src/agentflow"
	"github.com/flowgent-labs/flowgent/store/src/agentdef"
	storenf "github.com/flowgent-labs/flowgent/store/src/notifier"
	"github.com/flowgent-labs/flowgent/store/src/approval"
	"github.com/flowgent-labs/flowgent/store/src/flowrun"
	"github.com/jackc/pgx/v5/pgxpool"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// stopByPID reads a PID file and sends SIGTERM to the process.
func stopByPID(pidFile string) error {
	if pidFile == "" {
		return nil
	}
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

// allInOneProcess handles daemon start/stop/restart actions.
func allInOneProcess(action, pidFile string) error {
	switch action {
	case "start":
		if pidFile != "" {
			if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
				return fmt.Errorf("write PID file %s: %w", pidFile, err)
			}
			defer os.Remove(pidFile)
		}
		log.Printf("Flowgent all-in-one starting (pid=%d, pidfile=%s)", os.Getpid(), pidFile)
		startServer("all")
		return nil
	case "stop":
		return stopByPID(pidFile)
	case "restart":
		_ = stopByPID(pidFile) // best-effort stop
		time.Sleep(500 * time.Millisecond)
		if pidFile != "" {
			if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
				return fmt.Errorf("write PID file %s: %w", pidFile, err)
			}
			defer os.Remove(pidFile)
		}
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
		if pidFile != "" {
			if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
				return fmt.Errorf("write PID file %s: %w", pidFile, err)
			}
			defer os.Remove(pidFile)
		}
		log.Printf("Flowgent %s starting (pid=%d, pidfile=%s)", name, os.Getpid(), pidFile)
		startServer(mode)
		return nil
	case "stop":
		return stopByPID(pidFile)
	case "restart":
		_ = stopByPID(pidFile)
		time.Sleep(500 * time.Millisecond)
		if pidFile != "" {
			if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
				return fmt.Errorf("write PID file %s: %w", pidFile, err)
			}
			defer os.Remove(pidFile)
		}
		log.Printf("Flowgent %s restarting (pid=%d, pidfile=%s)", name, os.Getpid(), pidFile)
		startServer(mode)
		return nil
	default:
		return fmt.Errorf("unknown %s action: %s", name, action)
	}
}

// runAllInOne handles daemon start/stop/restart.
func runAllInOne(action, pidFile string) error {
	return allInOneProcess(action, pidFile)
}

// runAPIServer handles apiserver start/stop/restart.
func runAPIServer(action, pidFile string) error {
	return serverProcess("apiserver", action, pidFile, "api")
}

// runA2AServer handles a2a start/stop/restart.
func runA2AServer(action, pidFile string) error {
	return serverProcess("a2a", action, pidFile, "a2a")
}

func runTaskManager(action, pidFile string) error {
	switch action {
	case "start":
		if pidFile != "" {
			writePID(pidFile)
		}
		return startTaskManager()
	case "stop":
		return stopByPID(pidFile)
	case "restart":
		_ = stopByPID(pidFile)
		time.Sleep(500 * time.Millisecond)
		if pidFile != "" {
			writePID(pidFile)
		}
		return startTaskManager()
	default:
		return fmt.Errorf("unknown taskmanager action: %s", action)
	}
}

func runJobManager(action, pidFile string) error {
	switch action {
	case "start":
		if pidFile != "" {
			writePID(pidFile)
		}
		return startJobManager()
	case "stop":
		return stopByPID(pidFile)
	case "restart":
		_ = stopByPID(pidFile)
		time.Sleep(500 * time.Millisecond)
		if pidFile != "" {
			writePID(pidFile)
		}
		return startJobManager()
	default:
		return fmt.Errorf("unknown jobmanager action: %s", action)
	}
}

func writePID(pidFile string) {
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
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

	logger := utils.NewLogger(serviceCfg.Logging.Mode, serviceCfg.Logging.Level)

	agentFlows, subAgentFlows, err := config.LoadAgentFlows(serviceCfg, cfgPath)
	if err != nil {
		log.Fatalf("Failed to load agentFlows: %v", err)
	}
	slog.Info("AgentFlows loaded", "count", len(agentFlows)+len(subAgentFlows))

	// ── Database ────────────────────────────────────────
	storeImpl := initStore(serviceCfg)
	defer storeImpl.(interface{ Close() error }).Close()

	// ── Entity Stores ──────────────────────────────────────
	var localFrStore flowrun.IFlowRunStore
	switch db := storeImpl.DB().(type) {
	case *pgxpool.Pool:
		localFrStore = flowrun.NewFlowRunPostgresStore(db)
	case *sql.DB:
		localFrStore = flowrun.NewFlowRunSQLiteStore(db)
	}

	// ── OTEL ────────────────────────────────────────────
	if serviceCfg.Mgmt.OTEL.Enabled {
		endpoint := serviceCfg.Mgmt.OTEL.Endpoint
		if endpoint == "" {
			endpoint = "localhost:4317"
		}
		otelCfg := &tracing.OTELConfig{
				Enabled:    serviceCfg.Mgmt.OTEL.Enabled,
				Endpoint:   serviceCfg.Mgmt.OTEL.Endpoint,
				Protocol:   serviceCfg.Mgmt.OTEL.Protocol,
				Timeout:    serviceCfg.Mgmt.OTEL.Timeout,
				SampleRate: serviceCfg.Mgmt.OTEL.SampleRate,
			}
			metricsCfg := &tracing.MetricsConfig{
				Enabled:             serviceCfg.Mgmt.Metrics.Enabled,
				Prometheus:          serviceCfg.Mgmt.Metrics.Prometheus,
				ExportInterval:      serviceCfg.Mgmt.Metrics.ExportInterval,
				HistogramBoundaries: tracing.MetricsBoundaries{
					Task:  serviceCfg.Mgmt.Metrics.HistogramBoundaries.Task,
					LLM:   serviceCfg.Mgmt.Metrics.HistogramBoundaries.LLM,
					Queue: serviceCfg.Mgmt.Metrics.HistogramBoundaries.Queue,
				},
				Labels: serviceCfg.Mgmt.Metrics.Labels,
			}
			oc, err := tracing.NewProvider(context.Background(), serviceCfg.ServiceName, Version, otelCfg, metricsCfg)
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
	mcpFactory := mcp.NewMcpManager()
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
	llmClient := llm.NewLlmProviderManager(&serviceCfg.LLM, storeImpl)

	// ── Scheduler (owns TaskManager internally) ─────────
	loadedAgents, err := config.LoadAgents(serviceCfg, cfgPath)
	if err != nil {
		log.Fatalf("Failed to load agents: %v", err)
	}

	// ── DB-backed resources (Standard mode) ─────────────
	// Standard mode loads agent/agentflow definitions from the database
	// (written by the Flowgent UI or API). These are stored as JSON in
	// agentflow_definitions.definition (JSONB), in contrast to static
	// YAML manifests loaded from disk.
	// Both use the same model.AgentFlowSpec struct (dual-tagged json: + yaml:).
	if serviceCfg.Orchestration.Agents.Standard.Enabled {
		var agStore agentdef.IAgentDefStore
		switch db := storeImpl.DB().(type) {
		case *pgxpool.Pool:
			agStore = agentdef.NewAgentDefPostgresStore(db)
		case *sql.DB:
			agStore = agentdef.NewAgentDefSQLiteStore(db)
		}
		if agStore != nil {
			agentPage, dberr := agStore.Select(context.Background(), 1, 1000)
			if dberr != nil {
				slog.Warn("Failed to load agents from DB", "error", dberr)
			} else {
				for _, a := range agentPage.Items {
					loadedAgents = append(loadedAgents, *a)
				}
				slog.Info("Agents loaded from DB (standard mode)", "count", len(agentPage.Items))
			}
		}
	}
	if serviceCfg.Orchestration.AgentFlows.Standard.Enabled {
		dbFlows, dbSubFlows, dberr := loadAgentFlowsFromDB(context.Background(), storeImpl)
		if dberr != nil {
			slog.Warn("Failed to load agentflows from DB", "error", dberr)
		} else {
			agentFlows = append(agentFlows, dbFlows...)
			for k, v := range dbSubFlows {
				if _, exists := subAgentFlows[k]; !exists {
					subAgentFlows[k] = v
				}
			}
			slog.Info("AgentFlows loaded from DB (standard mode)", "count", len(dbFlows)+len(dbSubFlows))
		}
	}

	agentPtrs := make([]*config.AgentDef, len(loadedAgents))
	for i := range loadedAgents {
		agentPtrs[i] = &loadedAgents[i]
	}
	rm, err := resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
		Provider: engine.ProviderStandalone, PoolSize: serviceCfg.Orchestration.MaxConcurrentFlows,
		Store: storeImpl, Agents: agentPtrs, MCPClients: mcpMap, LLMClient: llmClient, Logger: logger,
	})
	if err != nil {
		log.Fatalf("Failed to create resource manager: %v", err)
	}

	// ── API Handlers ───────────────────────────────────
	healthHandler := &handler.HealthHandler{}
	agentFlowHandler := handler.NewFlowDefHandler(storeImpl, logger, agentFlows, subAgentFlows)
	agentHandler := handler.NewAgentDefHandler(storeImpl, logger)
	humanHandler := handler.NewHumanHandler(storeImpl, logger)
	triggerDispatcher := agentFlowHandler
	runHandler := handler.NewFlowRunHandler(storeImpl, logger)
	notifHandler := handler.NewNotifierHandler(storeImpl, logger)

	// ── Cron ───────────────────────────────────────────
	cronSched := trigger.NewScheduleTrigger()
	triggerFunc := func(ctx context.Context, id string) {
		run := &model.AgentFlowRun{AgentFlowID: id, Version: 1, Status: model.RunPending,
			Trigger: model.TriggerInfo{Type: "schedule", Source: "cron"}}
		if err := localFrStore.Create(ctx, run); err != nil {
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

	// ── JobManager + Poller ────────────────────────────
	jm, err := jobmanager.NewJobManager(storeImpl, rm, logger, newJobManagerConfig(serviceCfg))
	if err != nil {
		log.Fatalf("Failed to create job manager: %v", err)
	}

	go startRunPoller(context.Background(), storeImpl, jm,
		agentFlowHandler.AgentFlows(), "", "") // all-in-one: session mode, no agentFlowID filter

	// ── Hot reload ─────────────────────────────────────
	if refreshStr := serviceCfg.Orchestration.AgentFlows.Static.Refresh; refreshStr != "" {
		if d, err := time.ParseDuration(refreshStr); err == nil && d > 0 {
			go func() {
				t := time.NewTicker(d)
				defer t.Stop()
				for range t.C {
					nf, nsf, _ := config.ReloadAgentFlows(serviceCfg, cfgPath)
					agentFlowHandler.Reload(nf, nsf)
					triggerDispatcher.Reload(nf, nsf)
				}
			}()
		}
	}

	// ── Shutdown timeout ───────────────────────────────
	shutdownTO, _ := time.ParseDuration(serviceCfg.Server.ShutdownTimeout)
	if shutdownTO == 0 {
		shutdownTO = 15 * time.Second
	}

	// ── Notification Service ────────────────────────────
	notifSvc := createNotifierService(storeImpl, serviceCfg)
	if notifSvc != nil {
		go func() {
			if err := notifSvc.Start(context.Background()); err != nil {
				slog.Error("notification service", "error", err)
			}
		}()
		defer notifSvc.Shutdown()
	}

	// ── WebSocket Bridge ─────────────────────────────────
	var wsBridge *handler.NotifierWSBridge
	if notifSvc != nil {
		wsBridge = handler.NewNotifierWSBridge(&notifToWSAdapter{svc: notifSvc}, )
	}

	// ── REST API Server ────────────────────────────────
	restMux := api.RegisterRESTRoutes(healthHandler, agentFlowHandler, agentHandler, runHandler, humanHandler, notifHandler, wsBridge)
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

	// ── A2A API Server (uses official a2aproject SDK types) ──
	var a2aSrv *http.Server
	if serviceCfg.A2A.Enabled && (mode == "all" || mode == "a2a") {
		a2aMux := http.NewServeMux()
		a2aMux.HandleFunc("GET /.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(a2a.AgentCard{
				Name:         serviceCfg.ServiceName,
				Description:  "Flowgent autonomous agentflow orchestration engine",
				URL:          fmt.Sprintf("http://%s:%d", serviceCfg.A2A.Host, serviceCfg.A2A.Port),
				Version:      Version,
				Capabilities: a2a.AgentCapabilities{Streaming: false},
			})
		})
		a2aMux.HandleFunc("POST /a2a/tasks", func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				AgentFlowID string         `json:"agentflow_id"`
				Vars        map[string]any `json:"vars"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			run := &model.AgentFlowRun{
				ID: uuid.NewString(), AgentFlowID: req.AgentFlowID, Version: 1,
				Status: model.RunPending, Vars: req.Vars,
				Trigger: model.TriggerInfo{Type: "api", Source: "a2a"},
			}
			if err := localFrStore.Create(r.Context(), run); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(a2a.Task{
				ID: a2a.TaskID(run.ID), ContextID: run.ID,
				Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted},
			})
		})
		a2aMux.HandleFunc("GET /a2a/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
			run, err := localFrStore.Get(r.Context(), r.PathValue("id"))
			if err != nil || run == nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			s := a2a.TaskStateWorking
			if run.Status == model.RunCompleted {
				s = a2a.TaskStateCompleted
			}
			if run.Status == model.RunFailed {
				s = a2a.TaskStateFailed
			}
			if run.Status == model.RunCancelled {
				s = a2a.TaskStateCanceled
			}
			json.NewEncoder(w).Encode(a2a.Task{
				ID: a2a.TaskID(run.ID), ContextID: run.ID, Status: a2a.TaskStatus{State: s},
				History: []*a2a.Message{{Role: a2a.MessageRoleAgent,
					Parts: a2a.ContentParts{&a2a.TextPart{Text: fmt.Sprintf("status=%s error=%s", run.Status, run.Error)}}}},
			})
		})
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
func initStore(cfg *config.FlowgentConfig) store.IStore {
	log.Printf("initStore: storage.type=%q", cfg.Storage.Type)
	return store.NewStoreManager(cfg)
}

// logConfig prints key configuration details (masks sensitive fields).
func logConfig(cfg *config.FlowgentConfig) {
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
	for _, p := range cfg.LLM.Providers.Static {
		if !p.Enabled {
			continue
		}
		models := make([]string, len(p.Models))
		for i, m := range p.Models {
			models[i] = m.Name
		}
		proxy := p.Proxy
		if proxy == "" {
			proxy = "(direct)"
		}
		log.Printf("LLM:        id=%s type=%s endpoint=%s proxy=%s models=%v", p.ID, p.Type, p.Endpoint, proxy, models)
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
	factory *mcp.McpManager
	name    string
}

func (a *mcpAdapter) CallTool(ctx context.Context, toolName string, args map[string]any) (map[string]any, error) {
	return a.factory.CallTool(ctx, a.name, toolName, args)
}

// loadAgentFlowsFromDB reads agentflow definitions from the database (Standard mode).
// DB-stored definitions use JSON format (agentflow_definitions.definition JSONB column)
// vs static manifests which use YAML. Both deserialize to model.AgentFlowSpec.
func loadAgentFlowsFromDB(ctx context.Context, s engine.Store) ([]model.AgentFlowSpec, map[string]model.AgentFlowSpec, error) {
	var flows []model.AgentFlowSpec
	subFlows := make(map[string]model.AgentFlowSpec)

	var afStore agentflow.IAgentFlowStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		afStore = agentflow.NewAgentFlowPostgresStore(db)
	case *sql.DB:
		afStore = agentflow.NewAgentFlowSQLiteStore(db)
	}

	page, err := afStore.Select(ctx, 1, 1000)
	versions := page.Items
	if err != nil {
		return flows, subFlows, fmt.Errorf("list agentflow definitions: %w", err)
	}

	// Deduplicate: only take the latest version per agentflow_id.
	// Select returns (agentflow_id, version DESC) ordered.
	seen := make(map[string]bool)
	for _, v := range versions {
		if seen[v.AgentFlowID] {
			continue
		}
		seen[v.AgentFlowID] = true

		var spec model.AgentFlowSpec
		if err := json.Unmarshal(v.Definition, &spec); err != nil {
			slog.Warn("Skipping invalid agentflow definition", "agentflow_id", v.AgentFlowID, "error", err)
			continue
		}
		if spec.ID == "" {
			slog.Warn("Skipping agentflow definition with empty ID", "agentflow_id", v.AgentFlowID)
			continue
		}
		if spec.Kind == "skill" {
			subFlows[spec.ID] = spec
		} else {
			flows = append(flows, spec)
		}
	}

	return flows, subFlows, nil
}

// startRunPoller polls for pending AgentFlowRuns and dispatches them
// via the JobManager. Each run is dispatched in a goroutine.
//
// Mode routing:
//   - Session JM (agentFlowID=""): polls ALL PENDING runs with namespace=""
//     across the entire tenant. Shared pool — handles any flow.
//   - Application JM (agentFlowID set): polls PENDING runs ONLY for its
//     assigned flow, with matching namespace. Dedicated per-flow — single
//     responsibility, minimizes DB scan noise.
//
// Namespace filtering is a safety net to prevent cross-contamination.
func startRunPoller(ctx context.Context, s engine.Store, jm *jobmanager.JobManager,
	flows map[string]*model.AgentFlowSpec, namespace, agentFlowID string) {
	var frStore flowrun.IFlowRunStore
	var afStore agentflow.IAgentFlowStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		frStore = flowrun.NewFlowRunPostgresStore(db)
		afStore = agentflow.NewAgentFlowPostgresStore(db)
	case *sql.DB:
		frStore = flowrun.NewFlowRunSQLiteStore(db)
		afStore = agentflow.NewAgentFlowSQLiteStore(db)
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Session: agentFlowID="" → DB returns ALL runs (tenant-wide scan)
			// Application: agentFlowID="<flow>" → DB returns only that flow's runs
			page, _ := frStore.Select(ctx, 1, 50)
	runs := page.Items
			for _, run := range runs {
				if run.Status != model.RunPending {
					continue
				}
				// Safety net — namespace routing
				if namespace == "" && run.Namespace != "" {
					continue
				}
				if namespace != "" && run.Namespace != namespace {
					continue
				}
				spec := flows[run.AgentFlowID]
				if spec == nil {
					// Fallback: load from store for flows created via API
					if dbSpec, err := afStore.GetSpec(ctx, run.AgentFlowID); err == nil && dbSpec != nil {
						spec = dbSpec
						log.Printf("[poller] loaded flow spec from DB: %s (nodes=%d)", run.AgentFlowID, len(spec.Nodes))
					}
				}
				if spec == nil {
					continue
				}
				log.Printf("[poller] dispatch run=%s flow=%s priority=%s", run.ID[:8], run.AgentFlowID, run.Priority)
				go func(r *model.AgentFlowRun, sp *model.AgentFlowSpec) {
					_ = jm.Submit(ctx, r, sp)
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

// ─── TaskManager subcommand ─────────────────────────────

// ─── TaskManager subcommand ─────────────────────────────

func startTaskManager() error {
	svcCfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logMode, logLevel := svcCfg.Logging.Mode, svcCfg.Logging.Level
	logger := utils.NewLogger(logMode, logLevel)

	// Build tmID with mode prefix for heartbeat topic differentiation.
	// session: "session-tm-{hostname}-{hash}"  application: "app-{tenant}-{flowId}-tm-{hostname}-{hash}"
	mode := "session"
	if svcCfg != nil && svcCfg.Deployment.Mode != "" {
		mode = svcCfg.Deployment.Mode
	}
	defaultTMID := mode + "-tm-" + hostname()
	if flowID := envOr("FLOWGENT_AGENTFLOW_ID", ""); flowID != "" && mode == "application" {
		defaultTMID = mode + "-" + svcCfg.Tenant.DefaultTenant + "-" + flowID + "-tm-" + hostname()
	}
	tmID := envOr("FLOWGENT_TM_ID", defaultTMID)
	slotCount := envIntOr("FLOWGENT_TM_SLOTS", 4)

	q := newQueueFromConfig(svcCfg, tmID)
	defer q.Close()

	dbStore := store.NewStoreManager(svcCfg)

	var agentPtrs []*config.AgentDef
	if svcCfg != nil {
		if agents, err := config.LoadAgents(svcCfg, cfgPath); err == nil {
			agentPtrs = make([]*config.AgentDef, len(agents))
			for i := range agents {
				agentPtrs[i] = &agents[i]
			}
		}
	}

	// ── MCP Clients ────────────────────────────────────
	mcpFactory := mcp.NewMcpManager()
	for _, mcpDef := range svcCfg.Orchestration.MCPs {
		if mcpDef.Enabled {
			mcpFactory.Register(mcpDef.Name, mcpDef.Command, mcpDef.Args, mcpDef.Env)
		}
	}
	mcpMap := make(map[string]engine.MCPClient)
	for _, mcpDef := range svcCfg.Orchestration.MCPs {
		if mcpDef.Enabled {
			mcpMap[mcpDef.Name] = &mcpAdapter{factory: mcpFactory, name: mcpDef.Name}
		}
	}

	tm, err := taskmanager.NewTaskManager(&taskmanager.TaskManagerConfig{
		ID: tmID, SlotCount: slotCount, Queue: q, Store: dbStore,
		Agents: agentPtrs, MCPClients: mcpMap, Logger: logger,
		SandboxQueue:     q,
		SandboxPolicy:    svcCfg.Sandbox.Policy,
		SandboxWorkspace: svcCfg.Sandbox.Workspace,
	})
	if err != nil {
		return fmt.Errorf("create taskmanager: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := tm.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	log.Printf("TaskManager %s started (slots=%d)", tmID, slotCount)
	waitSignal()
	cancel()
	time.Sleep(2 * time.Second)
	return nil
}

// ─── JobManager subcommand ───────────────────────────────

func startJobManager() error {
	svcCfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := utils.NewLogger(svcCfg.Logging.Mode, svcCfg.Logging.Level)
	jmID := "jm-" + hostname()

	q := newQueueFromConfig(svcCfg, jmID)
	defer q.Close()

	storeImpl := store.NewStoreManager(svcCfg)

	var rm resourcemanager.ResourceManager
	jmNamespace := envOr("FLOWGENT_NAMESPACE", "")
	// Resolve deployment mode: config → env override (Controller sets FLOWGENT_DEPLOYMENT_MODE=application)
	mode := "session"
	if svcCfg != nil && svcCfg.Deployment.Mode != "" {
		mode = svcCfg.Deployment.Mode
	}
	appMode := mode == "application"

	// Session + Application: K8sRM with MQTT dispatch to TM pods.
	// Session: AutoScale=false (fixed 2 TM replicas, admin-managed).
	// Application: AutoScale=true (elastic scaling by queue depth).
	// All-in-one (no K8s): StandaloneRM for in-process execution.
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		rm, _ = resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
			Provider: engine.ProviderKubernetes, SlotsPerTM: 4, MinTMs: 2, MaxTMs: 10,
			K8sNamespace:      envOr("KUBERNETES_NAMESPACE", "default"),
			K8sDeploymentName: envOr("FLOWGENT_TM_DEPLOY", "flowgent-taskmanager"),
			Store:             storeImpl, Logger: logger, Queue: q,
			AutoScale: appMode,
		})
	}
	if rm == nil {
		var agentPtrs []*config.AgentDef
		if agents, err := config.LoadAgents(svcCfg, cfgPath); err == nil {
			for i := range agents { agentPtrs = append(agentPtrs, &agents[i]) }
		}
		mcpFactory := mcp.NewMcpManager()
		for _, mcpDef := range svcCfg.Orchestration.MCPs {
			if mcpDef.Enabled { mcpFactory.Register(mcpDef.Name, mcpDef.Command, mcpDef.Args, mcpDef.Env) }
		}
		mcpMap := make(map[string]engine.MCPClient)
		for _, mcpDef := range svcCfg.Orchestration.MCPs {
			if mcpDef.Enabled { mcpMap[mcpDef.Name] = &mcpAdapter{factory: mcpFactory, name: mcpDef.Name} }
		}
		rm, _ = resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
			Provider: engine.ProviderStandalone, PoolSize: 10, Store: storeImpl,
			Agents: agentPtrs, MCPClients: mcpMap, LLMClient: llm.NewLlmProviderManager(&svcCfg.LLM, storeImpl),
			Logger: logger, Queue: q,
		})
	}

	jm, err := jobmanager.NewJobManager(storeImpl, rm, logger, newJobManagerConfig(svcCfg))
	if err != nil {
		return fmt.Errorf("create jobmanager: %w", err)
	}

	agentFlowID := envOr("FLOWGENT_AGENTFLOW_ID", "")

	flows := make(map[string]*model.AgentFlowSpec)
	if appMode && agentFlowID != "" {
		// Application mode: load ONLY the assigned flow from DB.
		// Controller already wrote it to agentflow_definitions before creating this JM.
		if svcCfg != nil && svcCfg.Orchestration.AgentFlows.Standard.Enabled {
			if dbF, dbSF, err := loadAgentFlowsFromDB(context.Background(), storeImpl); err == nil {
				for i := range dbF {
					if dbF[i].ID == agentFlowID {
						flows[dbF[i].ID] = &dbF[i]
						break
					}
				}
				if sf, ok := dbSF[agentFlowID]; ok {
					flows[agentFlowID] = &sf
				}
			}
		}
		// If DB loading fails or flow not found, JM will retry on next poll — the
		// runPoller queries by flow ID and will pick it up once the flow spec exists.
	} else if svcCfg != nil {
		// Session mode: load ALL flows (tenant-wide shared pool).
		if f, sf, err := config.LoadAgentFlows(svcCfg, cfgPath); err == nil {
			for i := range f {
				flows[f[i].ID] = &f[i]
			}
			for k, v := range sf {
				flows[k] = &v
			}
		}
		if svcCfg.Orchestration.AgentFlows.Standard.Enabled {
			if dbF, dbSF, err := loadAgentFlowsFromDB(context.Background(), storeImpl); err == nil {
				for i := range dbF {
					flows[dbF[i].ID] = &dbF[i]
				}
				for k, v := range dbSF {
					flows[k] = &v
				}
			}
		}
	}

	log.Printf("[jm] loaded %d flows (agentFlowID=%s, appMode=%v)", len(flows), agentFlowID, appMode)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go startRunPoller(ctx, storeImpl, jm, flows, jmNamespace, agentFlowID)
	log.Printf("JobManager started (scheduler=%s, namespace=%s, agentFlow=%s, autoScale=%v)", rm.Provider(), jmNamespace, agentFlowID, appMode)
	waitSignal()
	cancel()
	time.Sleep(2 * time.Second)
	return nil
}

// ─── Helpers ──────────────────────────────────────────────

func waitSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func hostname() string {
	h, _ := os.Hostname()
	if h == "" {
		h = "unknown"
	}
	return h
}

// notifToWSAdapter adapts notifier.Service to the api.WSBridge interface.
type notifToWSAdapter struct {
	svc *notifier.Service
}

func (a *notifToWSAdapter) RegisterWS(ctx context.Context, agentFlowID string) (handler.WSConn, error) {
	conn, err := a.svc.RegisterWS(ctx, agentFlowID)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (a *notifToWSAdapter) PodID() string { return a.svc.PodID() }

// notifierStoreAdapter combines entity stores to satisfy notifier.Store.
type notifierStoreAdapter struct {
	apStore  approval.IApprovalStore
	ntStore  storenf.INotifierStore
	routesMu sync.Mutex
	routes   map[string]*model.SubscriptionRoute
}

func newNotifierStoreAdapter(s store.IStore) *notifierStoreAdapter {
	var apStore approval.IApprovalStore
	var ntStore storenf.INotifierStore
	if s != nil {
		switch db := s.DB().(type) {
		case *pgxpool.Pool:
			apStore = approval.NewApprovalPostgresStore(db)
			ntStore = storenf.NewNotifierPostgresStore(db)
		case *sql.DB:
			apStore = approval.NewApprovalSQLiteStore(db)
			ntStore = storenf.NewNotifierSQLiteStore(db)
		}
	}
	return &notifierStoreAdapter{
		apStore: apStore,
		ntStore: ntStore,
		routes:  make(map[string]*model.SubscriptionRoute),
	}
}

func (a *notifierStoreAdapter) ListPendingApprovals(ctx context.Context) ([]model.HumanApproval, error) {
	items, err := a.apStore.ListPending(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]model.HumanApproval, len(items))
	for i, item := range items {
		if item != nil {
			result[i] = *item
		}
	}
	return result, nil
}

func (a *notifierStoreAdapter) ListChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error) {
	page, err := a.ntStore.Select(ctx, 1, 1000)
	items := page.Items
	if err != nil {
		return nil, err
	}
	_ = tenantID
	result := make([]model.NotifierChannel, len(items))
	for i, item := range items {
		if item != nil {
			result[i] = *item
		}
	}
	return result, nil
}

func (a *notifierStoreAdapter) SaveRoute(ctx context.Context, route *model.SubscriptionRoute) error {
	a.routesMu.Lock()
	defer a.routesMu.Unlock()
	a.routes[route.ID] = route
	return nil
}

func (a *notifierStoreAdapter) GetRoutesByFlow(ctx context.Context, agentFlowID string) ([]model.SubscriptionRoute, error) {
	a.routesMu.Lock()
	defer a.routesMu.Unlock()
	var result []model.SubscriptionRoute
	for _, route := range a.routes {
		if route.AgentFlowID == agentFlowID {
			result = append(result, *route)
		}
	}
	return result, nil
}

func (a *notifierStoreAdapter) DeleteRoute(ctx context.Context, id string) error {
	a.routesMu.Lock()
	defer a.routesMu.Unlock()
	delete(a.routes, id)
	return nil
}

func (a *notifierStoreAdapter) CleanupOrphanedRoutes(ctx context.Context, podID string, maxAge time.Duration) (int64, error) {
	a.routesMu.Lock()
	defer a.routesMu.Unlock()
	var deleted int64
	for id, route := range a.routes {
		if route.PodID == podID && time.Since(route.CreatedAt) > maxAge {
			delete(a.routes, id)
			deleted++
		}
	}
	return deleted, nil
}

// createNotifierService builds a notifier.Service from config, or nil if disabled.
func createNotifierService(s store.IStore, cfg *config.FlowgentConfig) *notifier.Service {
	if !cfg.Notifier.Enabled {
		return nil
	}
	adapter := newNotifierStoreAdapter(s)
	svc := notifier.NewService(adapter, nil) // MQTT client wired when available
	for _, chCfg := range cfg.Notifier.Channels {
		if !chCfg.Enabled {
			continue
		}
		// Channels from config are persisted to the store on first startup
		// so the API can manage them dynamically.
	}
	return svc
}

// ─── Controller ───────────────────────────────────────────────

// isTerminalStatus returns true if the run has reached a terminal state.
func isTerminalStatus(s model.RunStatus) bool {
	return s == model.RunCompleted || s == model.RunFailed || s == model.RunCancelled
}

// Controller is the distributed flow driver. It polls agentflow_definitions from PG,
// shards flows across controller pods via hash-mod partitioning, and dispatches
// executions in either session mode (shared JM) or application mode (dedicated JM+TM).
//
// Architecture (like Flink's Dispatcher + ResourceManager):
//
//	┌──────────────┐  ┌──────────────┐  ┌──────────────┐
//	│ Controller-0 │  │ Controller-1 │  │ Controller-2 │   ← K8s Deployment (replicas=N)
//	│ shard 0,3,6  │  │ shard 1,4,7  │  │ shard 2,5,8  │   ← hash(flow_id) % N
//	└──────┬───────┘  └──────┬───────┘  └──────┬───────┘
//	       │                 │                 │
//	       └─────────────────┼─────────────────┘
//	                         │
//	           ┌─────────────┴─────────────┐
//	           │   PostgreSQL (shared)      │
//	           │   agentflow_definitions    │
//	           │   agentflow_runs           │
//	           └───────────────────────────┘
//
// Session mode (priority=low/medium/high):
//
//	→ POST to shared JM's REST API → JM dispatches to shared TM pool
//
// Application mode (priority=grade):
//
//	→ Create dedicated K8s Namespace + JM Deployment + TM Deployment
//	→ Flow runs in isolated cluster (like Flink Application Mode)
type Controller struct {
	store    store.IStore
	frStore  flowrun.IFlowRunStore
	afStore  agentflow.IAgentFlowStore
	rm       resourcemanager.ResourceManager
	logger   *utils.Logger
	cfg      *config.FlowgentConfig
	cfgPath  string

	// discovery: pluggable service discovery (K8s or static env-based)
	discovery    discovery.IDiscoveryClient
	pollInterval time.Duration

	mu      sync.Mutex
	running map[string]context.CancelFunc // flow_id → cancel
}

// NewController creates a Controller instance.
func NewController(s store.IStore, rm resourcemanager.ResourceManager, logger *utils.Logger,
	cfg *config.FlowgentConfig, cfgPath string, disc discovery.IDiscoveryClient) *Controller {
	var frStore flowrun.IFlowRunStore
	var afStore agentflow.IAgentFlowStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		frStore = flowrun.NewFlowRunPostgresStore(db)
		afStore = agentflow.NewAgentFlowPostgresStore(db)
	case *sql.DB:
		frStore = flowrun.NewFlowRunSQLiteStore(db)
		afStore = agentflow.NewAgentFlowSQLiteStore(db)
	}
	return &Controller{
		store:        s,
		frStore:      frStore,
		afStore:      afStore,
		rm:           rm,
		logger:       logger,
		cfg:          cfg,
		cfgPath:      cfgPath,
		discovery:    disc,
		pollInterval: 10 * time.Second,
		running:      make(map[string]context.CancelFunc),
	}
}

// ─── Pod Discovery & Sharding ──────────────────────────────

// getPeers discovers peer controller pods and returns total count + self index.
// Uses IDiscoveryClient (K8s or static) for pluggable service discovery.
func (c *Controller) getPeers(ctx context.Context) (peers []discovery.Peer, selfIndex int, err error) {
	labelSelector := os.Getenv("FLOWGENT_CONTROLLER_LABEL")
	if labelSelector == "" {
		labelSelector = "app.kubernetes.io/component=controller"
	}

	peers, err = c.discovery.DiscoverPeers(ctx, labelSelector)
	if err != nil {
		return nil, 0, fmt.Errorf("discover peers: %w", err)
	}

	self := c.discovery.Self()
	for i, p := range peers {
		if p.Name == self.Name {
			selfIndex = i
			break
		}
	}

	return peers, selfIndex, nil
}

// ownsFlow returns true if this controller pod is responsible for the given flow ID.
// Uses discovery.ShardIndex for consistent hash-mod partitioning.
func (c *Controller) ownsFlow(ctx context.Context, flowID string) bool {
	peers, _, err := c.getPeers(ctx)
	if err != nil || len(peers) == 0 {
		return true // if discovery fails, claim ownership (don't lose flows)
	}
	self := c.discovery.Self()
	shard := discovery.ShardIndex(flowID, len(peers))
	for i, p := range peers {
		if p.Name == self.Name && i == shard {
			return true
		}
	}
	return false
}

// ─── Main Loop ──────────────────────────────────────────────

// Run starts the controller's main reconciliation loop.
func (c *Controller) Run(ctx context.Context) error {
	peers, idx, err := c.getPeers(ctx)
	if err != nil {
		c.logger.Warn("Initial pod discovery failed, retrying", "error", err)
		peers = []discovery.Peer{c.discovery.Self()}
		idx = 0
	}

	c.logger.Info("Controller starting",
		"pod", c.discovery.Self().Name,
		"shard", fmt.Sprintf("%d/%d", idx, len(peers)),
		"poll_interval", c.pollInterval)

	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	// Immediate first poll
	c.reconcile(ctx)

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Controller shutting down")
			c.stopAllFlows()
			return nil
		case <-ticker.C:
			c.reconcile(ctx)
		}
	}
}

// reconcile polls PG for agentflow definitions and dispatches newly discovered flows.
func (c *Controller) reconcile(ctx context.Context) {
	page, err := c.afStore.Select(ctx, 1, 1000)
	versions := page.Items
	if err != nil {
		c.logger.Error("Failed to list agentflow definitions", "error", err)
		return
	}

	// Deduplicate by agentflow_id (latest version)
	seen := make(map[string]*model.AgentFlowSpec)
	for _, v := range versions {
		if _, exists := seen[v.AgentFlowID]; exists {
			continue
		}
		var spec model.AgentFlowSpec
		if err := json.Unmarshal(v.Definition, &spec); err != nil {
			c.logger.Warn("Skipping invalid agentflow definition", "agentflow_id", v.AgentFlowID, "error", err)
			continue
		}
		if spec.ID == "" {
			continue
		}
		seen[v.AgentFlowID] = &spec
	}

	for flowID, spec := range seen {
		// Only process flows in this controller's shard
		if !c.ownsFlow(ctx, flowID) {
			continue
		}

		// Don't re-dispatch flows already running on this controller
		c.mu.Lock()
		_, alreadyRunning := c.running[flowID]
		c.mu.Unlock()
		if alreadyRunning {
			continue
		}

		c.logger.Info("Controller dispatching flow",
			"flow_id", flowID,
			"priority", spec.Priority,
			"mode", spec.EffectiveMode())

		// Dispatch based on execution mode
		go c.dispatchFlow(ctx, spec)
	}
}

// dispatchFlow launches a flow in the appropriate mode.
func (c *Controller) dispatchFlow(ctx context.Context, spec *model.AgentFlowSpec) {
	flowCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.running[spec.ID] = cancel
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.running, spec.ID)
		c.mu.Unlock()
		cancel()
	}()

	mode := spec.EffectiveMode()
	switch mode {
	case model.ModeApplication:
		c.dispatchApplicationMode(flowCtx, spec)
	default:
		c.dispatchSessionMode(flowCtx, spec)
	}
}

// dispatchSessionMode creates a pending run for the shared JM to pick up.
// The shared JM polls agentflow_runs and dispatches to the shared TM pool.
func (c *Controller) dispatchSessionMode(ctx context.Context, spec *model.AgentFlowSpec) {
	c.logger.Info("Session mode dispatch", "flow_id", spec.ID)

	// Create a pending run — the shared JM's runPoller will pick it up
	run := &model.AgentFlowRun{
		ID:          fmt.Sprintf("%s-%d", spec.ID, time.Now().UnixNano()),
		AgentFlowID: spec.ID,
		Version:     1,
		Status:      model.RunPending,
		Priority:    spec.Priority,
		TenantID:    spec.TenantID,
		Namespace:   spec.Namespace,
		Vars:        spec.Vars,
		Trigger:     model.TriggerInfo{Type: "schedule", Source: "controller"},
	}

	if err := c.frStore.Create(ctx, run); err != nil {
		c.logger.Error("Failed to create session run", "flow_id", spec.ID, "error", err)
		return
	}

	c.logger.Info("Session run created", "flow_id", spec.ID, "run_id", run.ID)

	// Wait for completion (poll agentflow_runs)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r, err := c.frStore.Get(ctx, run.ID)
			if err != nil || r == nil {
				continue
			}
			if isTerminalStatus(r.Status) {
				c.logger.Info("Session run completed", "flow_id", spec.ID, "run_id", run.ID, "status", r.Status)
				return
			}
		}
	}
}

// dispatchApplicationMode creates a dedicated JM pod for this flow.
//
// Key design: the dedicated JM runs the EXACT SAME binary + code path as the
// shared session JM — `flowgent jobmanager start`. The only difference is
// the FLOWGENT_NAMESPACE env var, which makes the JM's runPoller only pick up
// runs in its own namespace. This ensures the poller→DAG→ExecutionPlan→submit
// logic is unified across both modes.
//
// Architecture:
//
//	Controller (this pod)
//	  │
//	  │ 1. kubectl create deployment flowgent-jobmanager-{flow_id}
//	  │    → JM starts, runPoller runs (same code as session JM)
//	  │    → JM only processes runs where namespace == flowgent-{flow_id}
//	  │
//	  │ 2. INSERT INTO agentflow_runs (namespace=flowgent-{flow_id})
//	  │    → JM's runPoller picks it up (namespace match)
//	  │    → JM.BuildGraph(spec) → JM.Submit(run, spec)
//	  │    → TaskManagers execute plans via MQTT
//	  │
//	  ▼
//	[COMPLETED]
func (c *Controller) dispatchApplicationMode(ctx context.Context, spec *model.AgentFlowSpec) {
	c.logger.Info("Application mode dispatch", "flow_id", spec.ID, "namespace", spec.Namespace)

	ns := spec.Namespace
	if ns == "" {
		ns = fmt.Sprintf("%s-%s", c.cfg.Tenant.NamespacePrefix, spec.ID)
	}

	// Try K8s-based application deployment
	cfg, err := rest.InClusterConfig()
	if err != nil {
		c.logger.Warn("Not in K8s cluster, falling back to session mode",
			"flow_id", spec.ID, "error", err)
		c.dispatchSessionMode(ctx, spec)
		return
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		c.logger.Warn("Failed to create K8s client, falling back to session",
			"flow_id", spec.ID, "error", err)
		c.dispatchSessionMode(ctx, spec)
		return
	}

	// Create dedicated JM — runs the same `flowgent jobmanager start` binary,
	// with FLOWGENT_NAMESPACE=<ns> so runPoller only processes runs in this ns
	tenantID := spec.TenantID
	if tenantID == "" {
		tenantID = "default"
	}
	jmName := fmt.Sprintf("flowgent-jobmanager-%s-%s", tenantID, spec.ID)
	jmDeployment := c.buildJMDeployment(jmName, ns, tenantID, spec)

	_, err = clientset.AppsV1().Deployments(ns).Create(ctx, jmDeployment, metav1.CreateOptions{})
	if err != nil {
		c.logger.Warn("Failed to create dedicated JM deployment, falling back to session",
			"flow_id", spec.ID, "error", err)
		c.dispatchSessionMode(ctx, spec)
		return
	}

	c.logger.Info("Dedicated JM deployment created",
		"flow_id", spec.ID, "namespace", ns, "deployment", jmName)

	// Insert pending run — dedicated JM's runPoller picks it up (namespace match)
	run := &model.AgentFlowRun{
		ID:          fmt.Sprintf("%s-%d", spec.ID, time.Now().UnixNano()),
		AgentFlowID: spec.ID,
		Version:     1,
		Status:      model.RunPending,
		Priority:    model.PriorityGrade,
		TenantID:    spec.TenantID,
		Namespace:   ns,
		Vars:        spec.Vars,
		Trigger:     model.TriggerInfo{Type: "schedule", Source: "controller"},
	}
	if err := c.frStore.Create(ctx, run); err != nil {
		c.logger.Error("Failed to create application run", "flow_id", spec.ID, "error", err)
	}
}

// buildJMDeployment creates a K8s Deployment spec for a dedicated JM.
// Pod names follow: flowgent-jobmanager-{tenant}-{flow}-{hash}
func (c *Controller) buildJMDeployment(name, namespace, tenantID string, spec *model.AgentFlowSpec) *appsv1.Deployment {
	replicas := int32(1)
	labels := map[string]string{
		"app":                "flowgent-jobmanager",
		"flowgent.io/tenant": tenantID,
		"flowgent.io/flow":   spec.ID,
		"flowgent.io/mode":   "application",
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "jobmanager",
						Image: os.Getenv("FLOWGENT_JM_IMAGE"),
						Args:  []string{"jobmanager", "start", "-c", "/etc/flowgent/flowgent.yaml", "--flow-id", spec.ID},
						Env: []corev1.EnvVar{
							{Name: "FLOWGENT_DEPLOYMENT_MODE", Value: "application"},
							{Name: "FLOWGENT_NAMESPACE", Value: namespace},
							{Name: "FLOWGENT_AGENTFLOW_ID", Value: spec.ID},
						},
					}},
				},
			},
		},
	}

}

// stopAllFlows cancels all running flow dispatches.
func (c *Controller) stopAllFlows() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for flowID, cancel := range c.running {
		c.logger.Info("Stopping flow dispatch", "flow_id", flowID)
		cancel()
	}
	c.running = make(map[string]context.CancelFunc)
}

// ─── CLI entry points ──────────────────────────────────────

func runController(action, pidFile string) error {
	switch action {
	case "start":
		if pidFile != "" {
			writePID(pidFile)
			defer os.Remove(pidFile)
		}
		log.Printf("Flowgent Controller starting (pid=%d, pidfile=%s)", os.Getpid(), pidFile)
		return startController()
	case "stop":
		return stopByPID(pidFile)
	case "restart":
		_ = stopByPID(pidFile)
		time.Sleep(500 * time.Millisecond)
		if pidFile != "" {
			writePID(pidFile)
			defer os.Remove(pidFile)
		}
		log.Printf("Flowgent Controller restarting (pid=%d, pidfile=%s)", os.Getpid(), pidFile)
		return startController()
	default:
		return fmt.Errorf("unknown controller action: %s", action)
	}
}

func startController() error {
	svcCfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logMode, logLevel := "JSON", "DEBUG"
	if svcCfg != nil {
		logMode, logLevel = svcCfg.Logging.Mode, svcCfg.Logging.Level
	}
	logger := utils.NewLogger(logMode, logLevel)

	// Init store (requires PG for distributed mode)
	storeImpl := store.NewStoreManager(svcCfg)
	// Controller requires PG for distributed coordination
	if _, ok := storeImpl.DB().(*pgxpool.Pool); !ok {
		return fmt.Errorf("controller requires PostgreSQL storage (set FLOWGENT_DATABASE_URL or configure storage.type=POSTGRE)")
	}

	// Load agent definitions and build resource manager
	loadedAgents, _ := config.LoadAgents(svcCfg, cfgPath)
	agentPtrs := make([]*config.AgentDef, len(loadedAgents))
	for i := range loadedAgents {
		agentPtrs[i] = &loadedAgents[i]
	}

	rm, err := resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
		Provider:   engine.ProviderStandalone,
		PoolSize:   svcCfg.Orchestration.MaxConcurrentFlows,
		Store:      storeImpl,
		Agents:     agentPtrs,
		MCPClients: make(map[string]engine.MCPClient),
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("create resource manager: %w", err)
	}

	// Create discovery client: try K8s, fall back to static
	var disc discovery.IDiscoveryClient
	if k8sDisc, err := discovery.NewK8sDiscoveryClient(); err == nil {
		disc = k8sDisc
		logger.Info("Controller using K8s discovery client")
	} else {
		disc = discovery.NewStaticDiscoveryClient()
		logger.Info("Controller using static discovery client (env vars)")
	}

	ctrl := NewController(storeImpl, rm, logger, svcCfg, cfgPath, disc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigCh
		slog.Info("Controller received shutdown signal")
		cancel()
	}()

	if err := ctrl.Run(ctx); err != nil {
		return fmt.Errorf("controller run: %w", err)
	}
	return nil
}
func runNotifier(action, pidFile string) error {
	switch action {
	case "start":
		if pidFile != "" {
			if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
				return fmt.Errorf("write PID file %s: %w", pidFile, err)
			}
			defer os.Remove(pidFile)
		}
		log.Printf("Flowgent notification service starting (pid=%d)", os.Getpid())
		return startNotifierService()
	case "stop":
		return stopByPID(pidFile)
	case "restart":
		_ = stopByPID(pidFile)
		time.Sleep(500 * time.Millisecond)
		if pidFile != "" {
			if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
				return fmt.Errorf("write PID file %s: %w", pidFile, err)
			}
			defer os.Remove(pidFile)
		}
		log.Printf("Flowgent notification service restarting (pid=%d)", os.Getpid())
		return startNotifierService()
	default:
		return fmt.Errorf("unknown notification action: %s", action)
	}
}

func newJobManagerConfig(cfg *config.FlowgentConfig) *jobmanager.JobManagerConfig {
	timeout, _ := time.ParseDuration(cfg.Orchestration.FlowExecutionTimeout)
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	return &jobmanager.JobManagerConfig{
		FlowExecutionTimeout: timeout,
		MaxNodeRetries:       cfg.Orchestration.MaxNodeRetries,
		MaxConcurrentFlows:   cfg.Orchestration.MaxConcurrentFlows,
	}
}

func newQueueFromConfig(cfg *config.FlowgentConfig, clientID string) messager.IMessager {
	// Determine if this is a distributed deployment (Helm — session or application mode).
	// In distributed mode, MQTT is mandatory; failing to connect is a fatal error.
	distributed := cfg != nil && (cfg.Deployment.Mode == "session" || cfg.Deployment.Mode == "application")

	qc := cfg.Messaging
	if qc.Type == "mqtt" && qc.MQTT.Broker != "" {
		mqc := &messager.MQTTConfig{
			Broker:   qc.MQTT.Broker,
			ClientID: clientID,
			Username: qc.MQTT.Username,
			Password: qc.MQTT.Password,
		}
		mq, err := messager.NewMQTTMessager(mqc)
		if err == nil {
			return mq
		}
		if distributed {
			log.Fatalf("FATAL: MQTT connect failed in %s mode: %v — broker=%s", cfg.Deployment.Mode, err, qc.MQTT.Broker)
		}
		log.Printf("WARNING: MQTT connect failed (%v), falling back to memory queue", err)
	}
	if broker := os.Getenv("FLOWGENT_MQTT_BROKER"); broker != "" {
		mq, err := messager.NewMQTTMessager(&messager.MQTTConfig{Broker: broker, ClientID: clientID})
		if err == nil {
			return mq
		}
		if distributed {
			log.Fatalf("FATAL: MQTT (env) connect failed in %s mode: %v — broker=%s", cfg.Deployment.Mode, err, broker)
		}
		log.Printf("WARNING: MQTT (env) connect failed (%v), using memory queue", err)
	}
	if distributed {
		log.Fatalf("FATAL: MQTT broker not configured. In %s mode, set queue.mqtt.broker in flowgent.yaml or FLOWGENT_MQTT_BROKER env var.", cfg.Deployment.Mode)
	}
	log.Printf("WARNING: Using in-memory queue (local dev mode — not suitable for distributed deployment)")
	return messager.NewLocalMessager(1000)
}

func startNotifierService() error {
	serviceCfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	api := client.NewFlowgentClient()
	log.Printf("[notifier] using apiserver at %s (NO direct DB)", api.BaseURL)

	notifSvc := createNotifierService(nil, serviceCfg) // channels via apiserver REST, not PG
	if notifSvc == nil {
		log.Println("Notification service is disabled in config")
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		return nil
	}
	defer notifSvc.Shutdown()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("Notification service shutting down")
	return nil
}

func runSandbox(action, pidFile string) error {
	switch action {
	case "start":
		if pidFile != "" { writePID(pidFile) }
		log.Printf("Flowgent sandbox worker starting (pid=%d)", os.Getpid())
		return startSandboxService()
	case "stop": return stopByPID(pidFile)
	case "restart":
		_ = stopByPID(pidFile); time.Sleep(500 * time.Millisecond)
		if pidFile != "" { writePID(pidFile) }
		return startSandboxService()
	default: return fmt.Errorf("unknown sandbox action: %s", action)
	}
}

func startSandboxService() error {
	log.Printf("Sandbox worker starting (pid=%d)", os.Getpid())
	log.Println("Sandbox runner initializing — waiting for tasks via queue")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("Sandbox worker shutting down")
	return nil
}
