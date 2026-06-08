// Package allinone provides the combined single-process entry point
// for local development. It starts all components in one process:
// REST API, A2A, JobManager + RunPoller, cron triggers, notifier.
package allinone

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
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/jobmanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/trigger"
	"github.com/flowgent-labs/flowgent/core/pkg/llm"
	"github.com/flowgent-labs/flowgent/core/pkg/mcp"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/agentdef"
)

func Start(cfgPath, pidFile string) error {
	if pidFile != "" {
		cmdutil.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	log.Printf("Flowgent all-in-one starting (pid=%d)", os.Getpid())
	return startAllInOne(cfgPath)
}

func Stop(pidFile string) error { return cmdutil.StopByPID(pidFile) }

func Restart(cfgPath, pidFile string) error {
	_ = cmdutil.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	return Start(cfgPath, pidFile)
}

func startAllInOne(cfgPath string) error {
	serviceCfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cmdutil.LogConfig(serviceCfg)
	logger := utils.NewLogger(serviceCfg.Logging.Mode, serviceCfg.Logging.Level)

	agentFlows, subAgentFlows, err := config.LoadAgentFlows(serviceCfg, cfgPath)
	if err != nil {
		return fmt.Errorf("load agentFlows: %w", err)
	}
	slog.Info("AgentFlows loaded", "count", len(agentFlows)+len(subAgentFlows))

	// ── Database (apiserver-owned) ──
	storeImpl := cmdutil.InitStore(serviceCfg)
	defer storeImpl.(interface{ Close() error }).Close()

	// ── FlowgentClient for internal component state access ──
	apiClient := client.NewFlowgentClient()
	tenant := cmdutil.EnvOr("FLOWGENT_TENANT", "default")
	stateClient := &client.RunStateClient{Client: apiClient, Tenant: tenant}
	taskClient := &client.TaskStateClient{Client: apiClient, Tenant: tenant}
	humanClient := &client.HumanApprovalClient{Client: apiClient}
	llmLoader := &client.LlmProviderClient{Client: apiClient, Tenant: tenant}

	// DB-backed resources (apiserver-only path)
	loadedAgents, _ := config.LoadAgents(serviceCfg, cfgPath)
	if serviceCfg.Orchestration.Agents.Standard.Enabled {
		var agStore agentdef.IAgentDefStore
		switch db := storeImpl.DB().(type) {
		case *pgxpool.Pool:
			agStore = agentdef.NewAgentDefPostgresStore(db)
		case *sql.DB:
			agStore = agentdef.NewAgentDefSQLiteStore(db)
		}
		if agStore != nil {
			if agentPage, dberr := agStore.Select(context.Background(), model.PageRequest{Page: 1, Size: 1000}); dberr == nil {
				for _, a := range agentPage.Items {
					loadedAgents = append(loadedAgents, *a)
				}
			}
		}
	}
	if serviceCfg.Orchestration.AgentFlows.Standard.Enabled {
		if dbFlows, dbSubFlows, dberr := cmdutil.LoadAgentFlowsFromDB(context.Background(), storeImpl); dberr == nil {
			agentFlows = append(agentFlows, dbFlows...)
			for k, v := range dbSubFlows {
				subAgentFlows[k] = v
			}
		}
	}

	// ── MCP + LLM ──
	mcpFactory := mcp.NewMcpManager()
	for _, d := range serviceCfg.Orchestration.MCPs {
		if d.Enabled {
			mcpFactory.Register(d.Name, d.Command, d.Args, d.Env)
		}
	}
	mcpMap := make(map[string]engine.MCPClient)
	for _, d := range serviceCfg.Orchestration.MCPs {
		if d.Enabled {
			mcpMap[d.Name] = &cmdutil.McpAdapter{Factory: mcpFactory, Name: d.Name}
		}
	}

	agentPtrs := make([]*config.AgentDef, len(loadedAgents))
	for i := range loadedAgents {
		agentPtrs[i] = &loadedAgents[i]
	}
	rm, _ := resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
		Provider:      engine.ProviderStandalone,
		PoolSize:      serviceCfg.Orchestration.MaxConcurrentFlows,
		TaskState:     taskClient,
		HumanApproval: humanClient,
		Agents:        agentPtrs, MCPClients: mcpMap,
		LLMClient: llm.NewLlmProviderManager(&serviceCfg.LLM, llmLoader),
		Logger: logger,
	})

	// ── API Handlers ──
	healthHandler := &handler.HealthHandler{}
	agentFlowHandler := handler.NewFlowDefHandler(storeImpl, logger, agentFlows, subAgentFlows)
	agentHandler := handler.NewAgentDefHandler(storeImpl, logger)
	var mqttPub handler.MQTTPublisher // nil-safe for all-in-one
	humanHandler := handler.NewHumanHandler(storeImpl, mqttPub, logger)
	runHandler := handler.NewFlowRunHandler(storeImpl, mqttPub, logger)
	notifHandler := handler.NewNotifierHandler(storeImpl, logger)
	llmProviderHandler := handler.NewLlmProviderHandler(storeImpl)

	// ── Cron ──
	cronSched := trigger.NewScheduleTrigger()
	cronSched.RegisterAgentFlows(append(agentFlows, flattenSubflows(subAgentFlows)...), func(ctx context.Context, id string) {
		run := &model.AgentFlowRun{AgentFlowID: id, Version: 1, Status: model.RunPending,
			Trigger: model.TriggerInfo{Type: "schedule", Source: "cron"}}
		_, _ = apiClient.CreateRun(ctx, tenant, run)
	})
	cronSched.Start()
	defer cronSched.Stop()

	// ── JobManager + RunPoller ──
	timeout, _ := time.ParseDuration(serviceCfg.Orchestration.FlowExecutionTimeout)
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	jm, err := jobmanager.NewJobManager(stateClient, rm, logger, &jobmanager.JobManagerConfig{
		FlowExecutionTimeout: timeout,
		MaxNodeRetries:       serviceCfg.Orchestration.MaxNodeRetries,
		MaxConcurrentFlows:   serviceCfg.Orchestration.MaxConcurrentFlows,
	})
	if err != nil {
		return fmt.Errorf("create jobmanager: %w", err)
	}
	flowMap := agentFlowHandler.AgentFlows()
	go startRunPoller(context.Background(), apiClient, tenant, jm, flowMap, "", "")

	// ── Hot reload ──
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

	// ── Notifier ──
	notifSvc := cmdutil.CreateNotifierService(apiClient, serviceCfg)
	if notifSvc != nil {
		go func() { _ = notifSvc.Start(context.Background()) }()
		defer notifSvc.Shutdown()
	}
	var wsBridge *handler.NotifierWSBridge
	if notifSvc != nil {
		wsBridge = handler.NewNotifierWSBridge(&cmdutil.NotifToWSAdapter{Svc: notifSvc})
	}

	// ── REST API Server ──
	restMux := api.RegisterRESTRoutes(healthHandler, agentFlowHandler, agentHandler,
		runHandler, humanHandler, notifHandler, wsBridge, llmProviderHandler)
	var restHandler http.Handler = restMux
	if len(serviceCfg.Auth.AnonymousPaths) > 0 {
		restHandler = cmdutil.AuthMiddleware(serviceCfg.Auth, restMux)
	}

	restAddr := fmt.Sprintf("%s:%d", serviceCfg.Server.Host, serviceCfg.Server.Port)
	restSrv := &http.Server{
		Addr: restAddr, Handler: restHandler,
		ReadTimeout: readTO, WriteTimeout: writeTO,
		MaxHeaderBytes: serviceCfg.Server.MaxBodyBytes,
	}
	go func() {
		slog.Info("REST API server", "addr", restAddr)
		_ = restSrv.ListenAndServe()
	}()

	// ── A2A Server ──
	var a2aSrv *http.Server
	if serviceCfg.A2A.Enabled {
		a2aMux := http.NewServeMux()
		a2aMux.HandleFunc("GET /.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(a2a.AgentCard{
				Name: serviceCfg.ServiceName, Description: "Flowgent orchestration engine",
				URL: fmt.Sprintf("http://%s:%d", serviceCfg.A2A.Host, serviceCfg.A2A.Port),
				Version: "dev", Capabilities: a2a.AgentCapabilities{Streaming: false},
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
			if _, err := apiClient.CreateRun(r.Context(), tenant, run); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(a2a.Task{
				ID: a2a.TaskID(run.ID), ContextID: run.ID,
				Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted},
			})
		})
		a2aMux.HandleFunc("GET /_/healthz", healthHandler.Healthz)
		a2aSrv = &http.Server{
			Addr: fmt.Sprintf("%s:%d", serviceCfg.A2A.Host, serviceCfg.A2A.Port),
			Handler: a2aMux, ReadTimeout: readTO, WriteTimeout: writeTO,
		}
		go func() {
			slog.Info("A2A server", "addr", a2aSrv.Addr)
			_ = a2aSrv.ListenAndServe()
		}()
	}

	// ── Pprof ──
	if serviceCfg.Mgmt.Enabled && serviceCfg.Mgmt.PProf.Enabled {
		ppMux := http.NewServeMux()
		ppMux.HandleFunc("GET /debug/pprof/", pprof.Index)
		ppMux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
		ppMux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
		ppMux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
		ppMux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
		ppSrv := &http.Server{Addr: fmt.Sprintf("%s:%d", serviceCfg.Mgmt.Host, serviceCfg.Mgmt.Port), Handler: ppMux}
		go func() { ppSrv.ListenAndServe() }()
		defer ppSrv.Close()
	}

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

func flattenSubflows(m map[string]model.AgentFlowSpec) []model.AgentFlowSpec {
	var out []model.AgentFlowSpec
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// startRunPoller polls for pending runs via the apiserver client.
func startRunPoller(ctx context.Context, api *client.FlowgentClient, tenant string,
	jm *jobmanager.JobManager, flows map[string]*model.AgentFlowSpec,
	namespace, agentFlowID string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			page, err := api.ListRuns(ctx, tenant, string(model.RunPending), namespace, agentFlowID, 1, 50)
			if err != nil {
				continue
			}
			for _, run := range page.Items {
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
					if apiSpec, err := api.GetFlow(ctx, tenant, run.AgentFlowID); err == nil && apiSpec != nil {
						spec = apiSpec
					}
				}
				if spec == nil {
					continue
				}
				go func(r *model.AgentFlowRun, sp *model.AgentFlowSpec) {
					_ = jm.Submit(ctx, r, sp)
				}(run, spec)
			}
		}
	}
}
