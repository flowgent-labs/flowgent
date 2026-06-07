// Package apiserver provides the API server daemon entry point.
// It handles the REST API server, A2A protocol server, and the all-in-one
// combined mode (REST + A2A + embedded JobManager + Notifier).
package apiserver

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
	"syscall"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/flowgent-labs/flowgent/api/pkg"
	handler "github.com/flowgent-labs/flowgent/api/pkg/handler"
	"github.com/flowgent-labs/flowgent/cmd/pkg/cmdutil"
	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/trigger"
	"github.com/flowgent-labs/flowgent/core/pkg/llm"
	"github.com/flowgent-labs/flowgent/core/pkg/mcp"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/agentdef"
	"github.com/flowgent-labs/flowgent/store/pkg/agentflow"
	"github.com/flowgent-labs/flowgent/store/pkg/flowrun"
)

// ─── CLI entry points ──────────────────────────────────────────

// Start launches the API server in "api" mode (REST only).
func Start(cfgPath, pidFile string) error {
	return daemonProcess("apiserver", "start", pidFile, func() error {
		return startServer(cfgPath, "api")
	})
}

// Stop stops the API server daemon.
func Stop(pidFile string) error {
	return cmdutil.StopByPID(pidFile)
}

// Restart restarts the API server daemon.
func Restart(cfgPath, pidFile string) error {
	_ = cmdutil.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	return daemonProcess("apiserver", "start", pidFile, func() error {
		return startServer(cfgPath, "api")
	})
}

// StartAllInOne launches all components in a single process.
func StartAllInOne(cfgPath, pidFile string) error {
	return daemonProcess("all-in-one", "start", pidFile, func() error {
		return startServer(cfgPath, "all")
	})
}

// StopAllInOne stops the all-in-one daemon.
func StopAllInOne(pidFile string) error {
	return cmdutil.StopByPID(pidFile)
}

// RestartAllInOne restarts the all-in-one daemon.
func RestartAllInOne(cfgPath, pidFile string) error {
	_ = cmdutil.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	return daemonProcess("all-in-one", "start", pidFile, func() error {
		return startServer(cfgPath, "all")
	})
}

// StartA2A launches the A2A protocol server (standalone mode).
func StartA2A(cfgPath, pidFile string) error {
	return daemonProcess("a2a", "start", pidFile, func() error {
		return startServer(cfgPath, "a2a")
	})
}

// StopA2A stops the A2A daemon.
func StopA2A(pidFile string) error {
	return cmdutil.StopByPID(pidFile)
}

// RestartA2A restarts the A2A daemon.
func RestartA2A(cfgPath, pidFile string) error {
	_ = cmdutil.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	return daemonProcess("a2a", "start", pidFile, func() error {
		return startServer(cfgPath, "a2a")
	})
}

// ─── PID process helper ────────────────────────────────────────

func daemonProcess(name, action, pidFile string, fn func() error) error {
	switch action {
	case "start":
		if pidFile != "" {
			cmdutil.WritePID(pidFile)
			defer os.Remove(pidFile)
		}
		log.Printf("Flowgent %s starting (pid=%d, pidfile=%s)", name, os.Getpid(), pidFile)
		return fn()
	default:
		return fmt.Errorf("unknown %s action: %s", name, action)
	}
}

// ─── Core server startup ───────────────────────────────────────

// startServer initialises all subsystems and starts the HTTP servers.
// mode: "all" (REST+A2A+JM), "api" (REST+JM), "a2a" (A2A+JM).
func startServer(cfgPath, mode string) error {
	log.Printf("Flowgent server starting (mode=%s)", mode)
	if cfgPath != "" {
		log.Printf("Config path: %s", cfgPath)
	}

	serviceCfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Flowgent server config loaded")
	cmdutil.LogConfig(serviceCfg)

	logger := utils.NewLogger(serviceCfg.Logging.Mode, serviceCfg.Logging.Level)

	agentFlows, subAgentFlows, err := config.LoadAgentFlows(serviceCfg, cfgPath)
	if err != nil {
		log.Fatalf("Failed to load agentFlows: %v", err)
	}
	slog.Info("AgentFlows loaded", "count", len(agentFlows)+len(subAgentFlows))

	// ── Database ────────────────────────────────────────
	storeImpl := cmdutil.InitStore(serviceCfg)
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
		oc, err := tracing.NewProvider(context.Background(), serviceCfg.ServiceName, "dev", otelCfg, metricsCfg)
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
			mcpMap[mcpDef.Name] = &cmdutil.McpAdapter{Factory: mcpFactory, Name: mcpDef.Name}
		}
	}

	// ── LLM Client ─────────────────────────────────────
	llmClient := llm.NewLlmProviderManager(&serviceCfg.LLM, storeImpl)

	// ── Agents ──────────────────────────────────────────
	loadedAgents, err := config.LoadAgents(serviceCfg, cfgPath)
	if err != nil {
		log.Fatalf("Failed to load agents: %v", err)
	}

	// ── DB-backed resources (Standard mode) ─────────────
	if serviceCfg.Orchestration.Agents.Standard.Enabled {
		var agStore agentdef.IAgentDefStore
		switch db := storeImpl.DB().(type) {
		case *pgxpool.Pool:
			agStore = agentdef.NewAgentDefPostgresStore(db)
		case *sql.DB:
			agStore = agentdef.NewAgentDefSQLiteStore(db)
		}
		if agStore != nil {
			agentPage, dberr := agStore.Select(context.Background(), model.PageRequest{Page: 1, Size: 1000})
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
		dbFlows, dbSubFlows, dberr := cmdutil.LoadAgentFlowsFromDB(context.Background(), storeImpl)
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
		agentFlowHandler.AgentFlows(), "", "")

	// ── Hot reload ─────────────────────────────────────
	if refreshStr := serviceCfg.Orchestration.AgentFlows.Static.Refresh; refreshStr != "" {
		if d, err := time.ParseDuration(refreshStr); err == nil && d > 0 {
			go func() {
				t := time.NewTicker(d)
				defer t.Stop()
				for range t.C {
					nf, nsf, _ := config.ReloadAgentFlows(serviceCfg, cfgPath)
					agentFlowHandler.Reload(nf, nsf)
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
	notifSvc := cmdutil.CreateNotifierService(storeImpl, serviceCfg)
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
		wsBridge = handler.NewNotifierWSBridge(&cmdutil.NotifToWSAdapter{Svc: notifSvc})
	}

	// ── REST API Server ────────────────────────────────
	restMux := api.RegisterRESTRoutes(healthHandler, agentFlowHandler, agentHandler, runHandler, humanHandler, notifHandler, wsBridge)
	var restHandler http.Handler = restMux
	if len(serviceCfg.Auth.AnonymousPaths) > 0 {
		restHandler = cmdutil.AuthMiddleware(serviceCfg.Auth, restMux)
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

	// ── A2A API Server ──────────────────────────────────
	var a2aSrv *http.Server
	if serviceCfg.A2A.Enabled && (mode == "all" || mode == "a2a") {
		a2aMux := http.NewServeMux()
		a2aMux.HandleFunc("GET /.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(a2a.AgentCard{
				Name:         serviceCfg.ServiceName,
				Description:  "Flowgent autonomous agentflow orchestration engine",
				URL:          fmt.Sprintf("http://%s:%d", serviceCfg.A2A.Host, serviceCfg.A2A.Port),
				Version:      "dev",
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

		a2aAddr := fmt.Sprintf("%s:%d", serviceCfg.A2A.Host, serviceCfg.A2A.Port)
		a2aSrv = &http.Server{
			Addr:         a2aAddr,
			Handler:      a2aMux,
			ReadTimeout:  readTO,
			WriteTimeout: writeTO,
		}
		go func() {
			slog.Info("A2A API server", "addr", a2aAddr)
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
	return nil
}

// ─── JobManager helpers ────────────────────────────────────────

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

// ─── Run Poller ────────────────────────────────────────────────

// startRunPoller polls for pending AgentFlowRuns and dispatches them via the JobManager.
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
			page, _ := frStore.Select(ctx, model.PageRequest{Page: 1, Size: 50})
			runs := page.Items
			for _, run := range runs {
				if run.Status != model.RunPending {
					continue
				}
				if namespace == "" && run.Namespace != "" {
					continue
				}
				if namespace != "" && run.Namespace != namespace {
					continue
				}
				spec := flows[run.AgentFlowID]
				if spec == nil {
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
