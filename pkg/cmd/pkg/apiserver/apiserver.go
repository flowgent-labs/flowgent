// Package apiserver provides the API server daemon entry point.
// Per architecture: the apiserver is the sole DB client. It serves
// RESTful CRUD for agents, flows, runs, tasks, and notifications.
// It does NOT start JM, TM, A2A, MCP, LLM, or cron — those are
// separate components that call the apiserver REST API for state.
package apiserver

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

	"github.com/flowgent-labs/flowgent/api/pkg"
	handler "github.com/flowgent-labs/flowgent/api/pkg/handler"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/agentflow"
)

func Start(cfgPath, pidFile string) error {
	if pidFile != "" {
		utils.WritePID(pidFile)
		defer os.Remove(pidFile)
	}
	return startServer(cfgPath)
}

func Stop(pidFile string) error { return utils.StopByPID(pidFile) }

func Restart(cfgPath, pidFile string) error {
	_ = utils.StopByPID(pidFile)
	time.Sleep(500 * time.Millisecond)
	return Start(cfgPath, pidFile)
}

// startServer: DB + cache + REST handlers + HTTP server. Nothing else.
func startServer(cfgPath string) error {
	serviceCfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log.Printf("Flowgent API Server — sole DB client, RESTful CRUD only")
	config.LogConfig(serviceCfg)
	logger := utils.NewLogger(serviceCfg.Logging.Mode, serviceCfg.Logging.Level)

	// ── Database (sole DB connection per architecture) ──
	storeImpl := store.InitStore(serviceCfg)
	defer storeImpl.(interface{ Close() error }).Close()

	// DB-backed agentflow definitions (Standard mode)
	var agentFlows []model.AgentFlowSpec
	subAgentFlows := make(map[string]model.AgentFlowSpec)
	if serviceCfg.Orchestration.AgentFlows.Standard.Enabled {
		dbFlows, dbSubFlows, dberr := agentflow.LoadFromDB(context.Background(), storeImpl)
		if dberr != nil {
			slog.Warn("Failed to load agentflows from DB", "error", dberr)
		} else {
			agentFlows = append(agentFlows, dbFlows...)
			for k, v := range dbSubFlows {
				subAgentFlows[k] = v
			}
		}
	}

	// ── MQTT for lifecycle event publishing (optional; nil-safe handlers) ──
	var mqttPublisher handler.MQTTPublisher
	log.Printf("[apiserver] MQTT lifecycle publishing not yet wired (nil-safe)")

	// ── REST API Handlers ──
	healthHandler := &handler.HealthHandler{}
	agentFlowHandler := handler.NewFlowDefHandler(storeImpl, logger, agentFlows, subAgentFlows)
	agentHandler := handler.NewAgentDefHandler(storeImpl, logger)
	humanHandler := handler.NewHumanHandler(storeImpl, mqttPublisher, logger)
	runHandler := handler.NewFlowRunHandler(storeImpl, mqttPublisher, logger)
	notifHandler := handler.NewNotifierHandler(storeImpl, logger)
	llmProviderHandler := handler.NewLlmProviderHandler(storeImpl)

	slog.Info("AgentFlows registered", "count", len(agentFlows)+len(subAgentFlows))

	// ── REST HTTP Server ──
	restMux := api.RegisterRESTRoutes(healthHandler, agentFlowHandler, agentHandler,
		runHandler, humanHandler, notifHandler, nil, llmProviderHandler)
	var restHandler http.Handler = restMux
	if len(serviceCfg.Auth.AnonymousPaths) > 0 {
		restHandler = config.AuthMiddleware(serviceCfg.Auth, restMux)
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

	restAddr := fmt.Sprintf("%s:%d", serviceCfg.Server.Host, serviceCfg.Server.Port)
	restSrv := &http.Server{
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

	// ── Pprof ──
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

	// ── Shutdown ──
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	slog.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTO)
	defer cancel()
	if restSrv != nil {
		restSrv.Shutdown(ctx)
	}
	return nil
}
