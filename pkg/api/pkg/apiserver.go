package api

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

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/flowgent-labs/flowgent/api/pkg/auth"
	"github.com/flowgent-labs/flowgent/api/pkg/auth/ldap"
	"github.com/flowgent-labs/flowgent/api/pkg/auth/oidc"
	handler "github.com/flowgent-labs/flowgent/api/pkg/handler"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storepkg "github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/flow"
)

// FlowgentApiServer is the sole DB client and REST API server. It serves
// CRUD endpoints for agents, flows, runs, tasks, and notifications.
type FlowgentApiServer struct {
	cfg   *config.FlowgentConfig
	store storepkg.IStore

	restServer *http.Server
	ppServer   *http.Server

	// Handlers (set during construction)
	agentFlowHandler *handler.FlowDefHandler
}

// NewFlowgentApiServer initializes the store, loads flows from DB, creates
// all handlers, and registers REST routes. It does not start listening.
func NewFlowgentApiServer(cfg *config.FlowgentConfig) (*FlowgentApiServer, error) {
	slog.Info("Flowgent API Server — sole DB client, RESTful CRUD only")
	config.LogConfig(cfg)

	// ── Database ──
	storeImpl := storepkg.InitStore(cfg)

	// ── Load agentflows from DB ──
	var agentFlows []entities.FlowInfo
	subAgentFlows := make(map[string]entities.FlowInfo)
	if dbFlows, dbSubFlows, dberr := flow.LoadFromDB(context.Background(), storeImpl); dberr != nil {
		slog.Warn("Failed to load agentflows from DB", "error", dberr)
	} else {
		agentFlows = append(agentFlows, dbFlows...)
		for k, v := range dbSubFlows {
			subAgentFlows[k] = v
		}
	}

	logger := utils.NewLogger(cfg.Logging.Mode, cfg.Logging.Level)

	// ── MQTT Publisher (optional) ──
	var mqttPub handler.MQTTPublisher
	if cfg.Messager.Type == "mqtt" && cfg.Messager.MQTT.Broker != "" {
		mqttPub = newMQTTPublisher(cfg.Messager.MQTT.Broker,
			cfg.Messager.MQTT.ClientID,
			cfg.Messager.MQTT.Username,
			cfg.Messager.MQTT.Password)
	}

	// ── Handlers ──
	healthHandler := &handler.HealthHandler{}
	agentFlowHandler := handler.NewFlowDefHandler(storeImpl, logger, agentFlows, subAgentFlows, cfg.Tenant.NamespacePrefix, cfg.Tenant.DefaultTenant, mqttPub)
	agentHandler := handler.NewAgentDefHandler(storeImpl, logger)
	humanHandler := handler.NewHumanHandler(storeImpl, mqttPub, logger)
	runHandler := handler.NewFlowRunHandler(storeImpl, mqttPub, logger)
	notifHandler := handler.NewNotifierHandler(storeImpl, logger)
	llmProviderHandler := handler.NewLlmProviderHandler(storeImpl)
	mcpHandler := handler.NewMcpHandler(storeImpl)

	slog.Info("AgentFlows registered", "count", len(agentFlows)+len(subAgentFlows))

	// ── Routes ──
	restMux := RegisterRESTRoutes(healthHandler, agentFlowHandler, agentHandler,
		runHandler, humanHandler, notifHandler, nil, llmProviderHandler, mcpHandler)
	var restHandler http.Handler = restMux
	authSvc, err := auth.NewService(cfg.Auth)
	if err != nil {
		return nil, fmt.Errorf("auth service: %w", err)
	}
	authSvc.Register(oidc.NewService(cfg.Auth.OIDC, authSvc.TokenService()))
	authSvc.Register(ldap.NewService(cfg.Auth.LDAP, authSvc.TokenService()))
	restHandler = authSvc.Middleware()(restMux)

	readTO, _ := time.ParseDuration(cfg.Server.ReadTimeout)
	if readTO == 0 {
		readTO = 30 * time.Second
	}
	writeTO, _ := time.ParseDuration(cfg.Server.WriteTimeout)
	if writeTO == 0 {
		writeTO = 60 * time.Second
	}

	restAddr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	restSrv := &http.Server{
		Addr:           restAddr,
		Handler:        restHandler,
		ReadTimeout:    readTO,
		WriteTimeout:   writeTO,
		MaxHeaderBytes: cfg.Server.MaxBodyBytes,
	}

	srv := &FlowgentApiServer{
		cfg:              cfg,
		store:            storeImpl,
		restServer:       restSrv,
		agentFlowHandler: agentFlowHandler,
	}

	// ── Pprof ──
	if cfg.Mgmt.Enabled && cfg.Mgmt.PProf.Enabled {
		ppMux := http.NewServeMux()
		ppMux.HandleFunc("GET /debug/pprof/", pprof.Index)
		ppMux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
		ppMux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
		ppMux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
		ppMux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
		bind := cfg.Mgmt.PProf.ServerBind
		if bind == "" {
			bind = fmt.Sprintf("%s:%d", cfg.Mgmt.Host, cfg.Mgmt.Port)
		}
		srv.ppServer = &http.Server{Addr: bind, Handler: ppMux}
	}

	return srv, nil
}

// Start begins listening on the REST and pprof ports, then blocks until a
// shutdown signal is received. It handles graceful shutdown internally.
func (s *FlowgentApiServer) Start(ctx context.Context) error {
	go func() {
		slog.Info("REST API server", "addr", s.restServer.Addr)
		if err := s.restServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("REST server: %v", err)
		}
	}()

	if s.ppServer != nil {
		go func() {
			slog.Info("pprof", "addr", s.ppServer.Addr)
			if err := s.ppServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Warn("pprof server", "error", err)
			}
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		slog.Info("context cancelled, shutting down...")
	case sig := <-sigCh:
		slog.Info("signal received, shutting down...", "signal", sig)
	}

	return s.Shutdown()
}

// Shutdown gracefully stops the REST and pprof servers.
func (s *FlowgentApiServer) Shutdown() error {
	shutdownTO := 15 * time.Second
	if s.cfg != nil {
		if d, err := time.ParseDuration(s.cfg.Server.ShutdownTimeout); err == nil && d > 0 {
			shutdownTO = d
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTO)
	defer cancel()

	if s.restServer != nil {
		if err := s.restServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("REST server shutdown", "error", err)
		}
	}
	if s.ppServer != nil {
		s.ppServer.Close()
	}
	if s.store != nil {
		if closer, ok := s.store.(interface{ Close() error }); ok {
			closer.Close()
		}
	}
	return nil
}

// newMQTTPublisher creates a direct MQTT publisher that sends raw payload
// bytes without wrapping in InterMessage.
func newMQTTPublisher(broker, clientID, username, password string) handler.MQTTPublisher {
	if broker == "" {
		return nil
	}
	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID(clientID).
		SetCleanSession(true).
		SetKeepAlive(30 * time.Second).
		SetPingTimeout(10 * time.Second).
		SetConnectTimeout(10 * time.Second).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(30 * time.Second)
	if username != "" {
		opts.SetUsername(username)
	}
	if password != "" {
		opts.SetPassword(password)
	}
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.WaitTimeout(15*time.Second) && token.Error() != nil {
		slog.Warn("mqtt broker not available, lifecycle events disabled", "error", token.Error())
		return nil
	}
	slog.Info("MQTT lifecycle event publishing enabled", "broker", broker)
	return &rawMQTTPublisher{client: client}
}

type rawMQTTPublisher struct {
	client mqtt.Client
}

func (p *rawMQTTPublisher) Publish(ctx context.Context, topic string, payload []byte) error {
	token := p.client.Publish(topic, 1, false, payload)
	if token.WaitTimeout(5*time.Second) && token.Error() != nil {
		return token.Error()
	}
	return nil
}
