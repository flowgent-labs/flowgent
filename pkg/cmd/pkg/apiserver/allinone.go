package apiserver

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	handler "github.com/flowgent-labs/flowgent/api/pkg/handler"
	"github.com/flowgent-labs/flowgent/cmd/pkg/cmdutil"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/resourcemanager"
	"github.com/flowgent-labs/flowgent/core/pkg/llm"
	"github.com/flowgent-labs/flowgent/core/pkg/mcp"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/flowrun"
)

func initMCP(cfg *config.FlowgentConfig) (*mcp.McpManager, map[string]engine.MCPClient) {
	factory := mcp.NewMcpManager()
	for _, mcpDef := range cfg.Orchestration.MCPs {
		if mcpDef.Enabled {
			factory.Register(mcpDef.Name, mcpDef.Command, mcpDef.Args, mcpDef.Env)
		}
	}
	clients := make(map[string]engine.MCPClient)
	for _, mcpDef := range cfg.Orchestration.MCPs {
		if mcpDef.Enabled {
			clients[mcpDef.Name] = &cmdutil.McpAdapter{Factory: factory, Name: mcpDef.Name}
		}
	}
	return factory, clients
}

func initLLM(cfg *config.FlowgentConfig, s store.IStore) *llm.LlmProviderManager {
	return llm.NewLlmProviderManager(&cfg.LLM, s)
}

func initResourceManager(cfg *config.FlowgentConfig, s store.IStore,
	agents []*config.AgentDef, mcps map[string]engine.MCPClient,
	llmClient *llm.LlmProviderManager, logger *utils.Logger) resourcemanager.ResourceManager {
	rm, _ := resourcemanager.NewResourceManager(&resourcemanager.ResourceManagerConfig{
		Provider: engine.ProviderStandalone,
		PoolSize: cfg.Orchestration.MaxConcurrentFlows,
		Store:    s, Agents: agents, MCPClients: mcps, LLMClient: llmClient, Logger: logger,
	})
	return rm
}

func startHTTPServers(cfg *config.FlowgentConfig, restMux http.Handler,
	frStore flowrun.IFlowRunStore, healthHandler *handler.HealthHandler) {

	readTO, _ := time.ParseDuration(cfg.Server.ReadTimeout)
	if readTO == 0 {
		readTO = 30 * time.Second
	}
	writeTO, _ := time.ParseDuration(cfg.Server.WriteTimeout)
	if writeTO == 0 {
		writeTO = 60 * time.Second
	}

	var restHandler http.Handler = restMux
	if len(cfg.Auth.AnonymousPaths) > 0 {
		restHandler = cmdutil.AuthMiddleware(cfg.Auth, restMux)
	}

	restAddr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	restSrv := &http.Server{
		Addr:           restAddr,
		Handler:        restHandler,
		ReadTimeout:    readTO,
		WriteTimeout:   writeTO,
		MaxHeaderBytes: cfg.Server.MaxBodyBytes,
	}
	go func() {
		slog.Info("REST API server", "addr", restAddr)
		_ = restSrv.ListenAndServe()
	}()

	if cfg.A2A.Enabled {
		_ = frStore
		_ = healthHandler
	}
}
