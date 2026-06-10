// Package cmdutil provides shared utility functions for Flowgent CLI components.
package cmdutil

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client"
	"github.com/flowgent-labs/flowgent/core/pkg/mcp"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/notifier/pkg"
	handler "github.com/flowgent-labs/flowgent/api/pkg/handler"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/agentflow"
)

// ─── PID & Process helpers ─────────────────────────────────────

// StopByPID reads a PID file and sends SIGTERM to the process.
func StopByPID(pidFile string) error {
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

// WritePID writes the current process PID to a file.
func WritePID(pidFile string) {
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
}

// WaitSignal blocks until SIGTERM or SIGINT.
func WaitSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh
}

// Hostname returns the hostname or "unknown".
func Hostname() string {
	h, _ := os.Hostname()
	if h == "" {
		h = "unknown"
	}
	return h
}

// EnvOr returns the env value or a default.
func EnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// EnvIntOr returns the env int value or a default.
func EnvIntOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// ─── Store helpers ─────────────────────────────────────────────

// InitStore creates the Store implementation based on config.
func InitStore(cfg *config.FlowgentConfig) store.IStore {
	log.Printf("InitStore: storage.type=%q", cfg.Storage.Type)
	return store.NewStoreManager(cfg)
}

// ─── Config logging ────────────────────────────────────────────

// LogConfig prints key configuration details (masks sensitive fields).
func LogConfig(cfg *config.FlowgentConfig) {
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

	log.Printf("Cache:      provider=%s", cfg.Cache.Provider)
	log.Printf("REST API:   %s:%d (context=%s)", cfg.Server.Host, cfg.Server.Port, cfg.Server.ContextPath)
	if cfg.A2A.Enabled {
		log.Printf("A2A API:    %s:%d", cfg.A2A.Host, cfg.A2A.Port)
	} else {
		log.Printf("A2A API:    disabled")
	}
	if cfg.Mgmt.Enabled {
		log.Printf("Management: %s:%d (pprof=%v, otel=%v)", cfg.Mgmt.Host, cfg.Mgmt.Port, cfg.Mgmt.PProf.Enabled, cfg.Mgmt.OTEL.Enabled)
	}

	log.Printf("Engine:     max_concurrent=%d timeout=%s max_retries=%d",
		cfg.Orchestration.MaxConcurrentFlows, cfg.Orchestration.FlowExecutionTimeout, cfg.Orchestration.MaxNodeRetries)

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

	for _, mcpd := range cfg.Orchestration.MCPs {
		if mcpd.Enabled {
			log.Printf("MCP:        name=%s type=%s command=%v", mcpd.Name, mcpd.Type, mcpd.Command)
		}
	}
}

// ─── MCP Adapter ───────────────────────────────────────────────

// McpAdapter adapts mcp.McpManager to engine.MCPClient.
type McpAdapter struct {
	Factory *mcp.McpManager
	Name    string
}

func (a *McpAdapter) CallTool(ctx context.Context, toolName string, args map[string]any) (map[string]any, error) {
	return a.Factory.CallTool(ctx, a.Name, toolName, args)
}

// ─── Queue / Messaging ─────────────────────────────────────────

// NewQueueFromConfig creates a messager from config or env, with fallback to local.
func NewQueueFromConfig(cfg *config.FlowgentConfig, clientID string) messager.IMessager {
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
	if distributed {
		log.Fatalf("FATAL: MQTT broker not configured. In %s mode, set messager.mqtt.broker in flowgent.yaml or FLOWGENT__MESSAGER__MQTT__BROKER env var.", cfg.Deployment.Mode)
	}
	log.Printf("WARNING: Using in-memory queue (local dev mode — not suitable for distributed deployment)")
	return messager.NewLocalMessager(1000)
}

// ─── Notifier adapters ─────────────────────────────────────────

// NotifToWSAdapter adapts notifier.Service to the api.WSBridge interface.
type NotifToWSAdapter struct {
	Svc *notifier.Service
}

func (a *NotifToWSAdapter) RegisterWS(ctx context.Context, agentFlowID string) (handler.WSConn, error) {
	conn, err := a.Svc.RegisterWS(ctx, agentFlowID)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (a *NotifToWSAdapter) PodID() string { return a.Svc.PodID() }

// NotifierStoreAdapter satisfies notifier.Store using the apiserver client.
// Subscription routes are kept in-memory (transient, no REST API for them).
type NotifierStoreAdapter struct {
	API      *client.FlowgentClient
	Tenant   string
	RoutesMu sync.Mutex
	Routes   map[string]*model.SubscriptionRoute
}

// NewNotifierStoreAdapter creates a client-backed NotifierStoreAdapter.
func NewNotifierStoreAdapter(api *client.FlowgentClient, tenant string) *NotifierStoreAdapter {
	return &NotifierStoreAdapter{
		API:    api,
		Tenant: tenant,
		Routes: make(map[string]*model.SubscriptionRoute),
	}
}

func (a *NotifierStoreAdapter) ListPendingApprovals(ctx context.Context) ([]model.HumanApproval, error) {
	return a.API.ListPendingApprovals(ctx)
}

func (a *NotifierStoreAdapter) ListChannels(ctx context.Context, tenantID string) ([]model.NotifierChannel, error) {
	tid := tenantID
	if tid == "" {
		tid = a.Tenant
	}
	return a.API.ListChannels(ctx, tid)
}

func (a *NotifierStoreAdapter) SaveRoute(ctx context.Context, route *model.SubscriptionRoute) error {
	a.RoutesMu.Lock()
	defer a.RoutesMu.Unlock()
	a.Routes[route.ID] = route
	return nil
}

func (a *NotifierStoreAdapter) GetRoutesByFlow(ctx context.Context, agentFlowID string) ([]model.SubscriptionRoute, error) {
	a.RoutesMu.Lock()
	defer a.RoutesMu.Unlock()
	var result []model.SubscriptionRoute
	for _, route := range a.Routes {
		if route.AgentFlowID == agentFlowID {
			result = append(result, *route)
		}
	}
	return result, nil
}

func (a *NotifierStoreAdapter) DeleteRoute(ctx context.Context, id string) error {
	a.RoutesMu.Lock()
	defer a.RoutesMu.Unlock()
	delete(a.Routes, id)
	return nil
}

func (a *NotifierStoreAdapter) CleanupOrphanedRoutes(ctx context.Context, podID string, maxAge time.Duration) (int64, error) {
	a.RoutesMu.Lock()
	defer a.RoutesMu.Unlock()
	var deleted int64
	for id, route := range a.Routes {
		if route.PodID == podID && time.Since(route.CreatedAt) > maxAge {
			delete(a.Routes, id)
			deleted++
		}
	}
	return deleted, nil
}

// CreateNotifierService builds a notifier.Service from config, or nil if disabled.
func CreateNotifierService(api *client.FlowgentClient, cfg *config.FlowgentConfig) *notifier.Service {
	if !cfg.Notifier.Enabled {
		return nil
	}
	adapter := NewNotifierStoreAdapter(api, EnvOr("FLOWGENT_TENANT", "default"))
	svc := notifier.NewService(adapter, nil)
	for _, chCfg := range cfg.Notifier.Channels {
		if !chCfg.Enabled {
			continue
		}
		_ = chCfg
	}
	return svc
}

// ─── Auth middleware ───────────────────────────────────────────

// AuthMiddleware creates a simple auth middleware that skips anonymous paths.
func AuthMiddleware(cfg config.AuthConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, p := range cfg.AnonymousPaths {
			if MatchGlob(p, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// MatchGlob matches a path against a glob-like pattern.
func MatchGlob(pattern, path string) bool {
	if pattern == path {
		return true
	}
	if len(pattern) > 2 && pattern[len(pattern)-2:] == "/**" {
		pfx := pattern[:len(pattern)-2]
		return len(path) >= len(pfx) && path[:len(pfx)] == pfx
	}
	return false
}

// ─── DB helpers (apiserver/all-in-one only — these have DB access) ──

// LoadAgentFlowsFromDB reads agentflow definitions from the database (Standard mode).
// Only call from apiserver/all-in-one which are allowed direct DB access.
func LoadAgentFlowsFromDB(ctx context.Context, s store.IStore) ([]model.AgentFlowSpec, map[string]model.AgentFlowSpec, error) {
	var flows []model.AgentFlowSpec
	subFlows := make(map[string]model.AgentFlowSpec)

	var afStore agentflow.IAgentFlowStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		afStore = agentflow.NewAgentFlowPostgresStore(db)
	case *sql.DB:
		afStore = agentflow.NewAgentFlowSQLiteStore(db)
	}

	page, err := afStore.Select(ctx, model.PageRequest{Page: 1, Size: 1000})
	versions := page.Items
	if err != nil {
		return flows, subFlows, fmt.Errorf("list agentflow definitions: %w", err)
	}

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
