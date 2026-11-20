package api

import (
	"net/http"

	"github.com/flowgent-labs/flowgent/api/pkg/handler"
	"github.com/flowgent-labs/flowgent/api/pkg/swagger"
)

// RegisterRESTRoutes returns a ServeMux with all REST API routes.
// Namespace-scoped paths use {namespace} for multi-namespace isolation.
func RegisterRESTRoutes(
	health *handler.HealthHandler,
	graphDef *handler.AgentGraphHandler,
	agentDef *handler.AgentDefHandler,
	skillDef *handler.SkillHandler,
	graphRun *handler.GraphRunHandler,
	human *handler.HumanHandler,
	notif *handler.NotifierHandler,
	ws *handler.NotifierWSBridge,
	llmProvider *handler.LlmProviderHandler,
	mcpH *handler.McpHandler,
	webhook *handler.WebhookHandler,
	knowledgeHandler *handler.KnowledgeHandler,
	traceHandler *handler.TraceHandler,
	graphRelease *handler.AgentGraphReleaseHandler,
	runtimeConfig *handler.RuntimeConfigHandler,
) *http.ServeMux {
	mux := http.NewServeMux()

	// ── Health & Spec ──────────────────────────────────────
	mux.HandleFunc("GET /_/healthz", health.Healthz)
	swaggerConfig := swagger.DefaultSwaggerConfig()
	mux.HandleFunc("GET /_/openapi.yaml", swagger.NewOpenAPIHandler(swaggerConfig))
	mux.HandleFunc("GET /_/swagger-ui", swagger.NewSwaggerUIHandler(swaggerConfig))

	// ── Agents (namespace-scoped) ─────────────────────────────
	mux.HandleFunc("GET /api/v1/{namespace}/agents", agentDef.List)
	mux.HandleFunc("POST /api/v1/{namespace}/agents", agentDef.Create)
	mux.HandleFunc("GET /api/v1/{namespace}/agents/{name}", agentDef.Get)
	mux.HandleFunc("PUT /api/v1/{namespace}/agents/{name}", agentDef.Update)
	mux.HandleFunc("DELETE /api/v1/{namespace}/agents/{name}", agentDef.Delete)

	// ── Reusable Skill Definitions (namespace-scoped) ───────────
	mux.HandleFunc("GET /api/v1/{namespace}/skill-definitions", skillDef.List)
	mux.HandleFunc("POST /api/v1/{namespace}/skill-definitions", skillDef.Create)
	mux.HandleFunc("GET /api/v1/{namespace}/skill-definitions/{name}", skillDef.Get)
	mux.HandleFunc("PUT /api/v1/{namespace}/skill-definitions/{name}", skillDef.Update)
	mux.HandleFunc("DELETE /api/v1/{namespace}/skill-definitions/{name}", skillDef.Delete)
	mux.HandleFunc("POST /api/v1/{namespace}/skill-definitions/{name}/assets", skillDef.UploadAsset)
	mux.HandleFunc("POST /api/v1/{namespace}/skill-definitions/{name}/scripts", skillDef.UploadScript)

	// ── AgentGraphs (namespace-scoped) ─────────────────────────
	mux.HandleFunc("GET /api/v1/{namespace}/agent-graphs", graphDef.List)
	mux.HandleFunc("GET /api/v1/{namespace}/agent-graphs/watch", graphDef.Watch)
	mux.HandleFunc("POST /api/v1/{namespace}/agent-graphs", graphDef.Create)
	mux.HandleFunc("GET /api/v1/{namespace}/agent-graphs/{id}", graphDef.Get)
	mux.HandleFunc("PUT /api/v1/{namespace}/agent-graphs/{id}", graphDef.Update)
	mux.HandleFunc("DELETE /api/v1/{namespace}/agent-graphs/{id}", graphDef.Delete)
	mux.HandleFunc("POST /api/v1/{namespace}/agent-graphs/{id}/trigger", graphDef.TriggerByID)

	// GraphRun has one canonical REST resource; ownership is enforced in SQL.
	if traceHandler != nil {
	}
	if runtimeConfig != nil {
		mux.HandleFunc("GET /api/v1/{namespace}/runtime-config", runtimeConfig.GetNamespace)
		mux.HandleFunc("PUT /api/v1/{namespace}/runtime-config/environment", runtimeConfig.UpdateNamespaceEnvironment)
		mux.HandleFunc("PUT /api/v1/{namespace}/runtime-config/secrets", runtimeConfig.UpdateNamespaceSecrets)
		mux.HandleFunc("GET /api/v1/{namespace}/agent-graphs/{agent_graph_id}/runtime-config", runtimeConfig.GetAgentGraph)
		mux.HandleFunc("PUT /api/v1/{namespace}/agent-graphs/{agent_graph_id}/runtime-config/environment", runtimeConfig.UpdateAgentGraphEnvironment)
		mux.HandleFunc("PUT /api/v1/{namespace}/agent-graphs/{agent_graph_id}/runtime-config/secrets", runtimeConfig.UpdateAgentGraphSecrets)
		mux.HandleFunc("GET /api/v1/{namespace}/agent-graphs/{agent_graph_id}/runtime-config/resolved", runtimeConfig.ResolveAgentGraph)
	}
	// ── Runtime Skills (kind=skill AgentGraph definitions) ───────────
	mux.HandleFunc("GET /api/v1/{namespace}/skills", graphDef.ListSkills)
	mux.HandleFunc("POST /api/v1/{namespace}/skills", graphDef.CreateSkill)
	mux.HandleFunc("GET /api/v1/{namespace}/skills/{id}", graphDef.GetSkill)
	mux.HandleFunc("PUT /api/v1/{namespace}/skills/{id}", graphDef.UpdateSkill)
	mux.HandleFunc("DELETE /api/v1/{namespace}/skills/{id}", graphDef.DeleteSkill)

	// ── Runs (namespace-scoped) ───────────────────────────────
	mux.HandleFunc("POST /api/v1/{namespace}/graph-runs", graphRun.Create)
	mux.HandleFunc("GET /api/v1/{namespace}/graph-runs", graphRun.List)
	mux.HandleFunc("GET /api/v1/{namespace}/graph-runs/metrics", graphRun.Metrics)
	mux.HandleFunc("PUT /api/v1/{namespace}/graph-runs/{graph_run_id}", graphRun.Update)
	mux.HandleFunc("GET /api/v1/{namespace}/graph-runs/{graph_run_id}", graphRun.Get)
	mux.HandleFunc("DELETE /api/v1/{namespace}/graph-runs/{graph_run_id}", graphRun.Delete)
	mux.HandleFunc("POST /api/v1/{namespace}/graph-runs/{graph_run_id}/cancel", graphRun.Cancel)
	mux.HandleFunc("POST /api/v1/{namespace}/graph-runs/{graph_run_id}/node-runs/{node_run_id}/claim", graphRun.ClaimNodeRun)
	mux.HandleFunc("GET /api/v1/{namespace}/graph-runs/{graph_run_id}/agents/{agent_name}", graphRun.AgentSnapshot)
	mux.HandleFunc("GET /api/v1/{namespace}/graph-runs/{graph_run_id}/snapshot", graphRun.GraphSnapshot)
	mux.HandleFunc("POST /api/v1/{namespace}/graph-runs/{graph_run_id}/node-runs/{node_run_id}/dispatch", graphRun.DispatchNodeRun)
	mux.HandleFunc("POST /api/v1/{namespace}/graph-runs/{graph_run_id}/checkpoints", graphRun.CreateCheckpoint)
	mux.HandleFunc("GET /api/v1/{namespace}/graph-runs/{graph_run_id}/recovery", graphRun.Recovery)
	mux.HandleFunc("POST /api/v1/{namespace}/graph-runs/{graph_run_id}/node-runs/{node_run_id}/sandbox-dispatch", graphRun.DispatchSandbox)
	mux.HandleFunc("POST /api/v1/{namespace}/graph-runs/{graph_run_id}/node-runs/{node_run_id}/sandbox-result", graphRun.CommitSandboxResult)
	mux.HandleFunc("PUT /api/v1/{namespace}/graph-runs/{graph_run_id}/node-runs/{node_run_id}/checkpoint", graphRun.CommitNodeCheckpoint)
	mux.HandleFunc("POST /api/v1/{namespace}/graph-runs/{graph_run_id}/subgraphs/initialize", graphRun.InitializeSubGraph)
	mux.HandleFunc("POST /api/v1/{namespace}/graph-runs/{graph_run_id}/graph-patches", graphRun.ApplyGraphPatch)
	mux.HandleFunc("POST /api/v1/{namespace}/graph-runs/{graph_run_id}/node-runs", graphRun.CreateNodeRun)
	mux.HandleFunc("GET /api/v1/{namespace}/graph-runs/{graph_run_id}/node-runs", graphRun.ListNodeRuns)
	mux.HandleFunc("GET /api/v1/{namespace}/graph-runs/{graph_run_id}/node-runs/{node_run_id}", graphRun.GetNodeRun)
	mux.HandleFunc("PUT /api/v1/{namespace}/graph-runs/{graph_run_id}/node-runs/{node_run_id}", graphRun.UpdateNodeRun)
	mux.HandleFunc("GET /api/v1/{namespace}/graph-runs/{graph_run_id}/approvals", human.ListRunApprovals)
	mux.HandleFunc("POST /api/v1/{namespace}/graph-runs/{graph_run_id}/approvals", human.CreateRunApproval)
	mux.HandleFunc("POST /api/v1/{namespace}/graph-runs/{graph_run_id}/approvals/{approval_id}/{decision}", human.ResolveRunApproval)
	if traceHandler != nil {
		mux.HandleFunc("GET /api/v1/{namespace}/graph-runs/{graph_run_id}/trace", traceHandler.GetRunTrace)
	}

	// ── Namespace approval feed for the notifier ───────────
	mux.HandleFunc("GET /api/v1/{namespace}/approvals", human.ListNamespaceApprovals)

	// ── Notification Channels (namespace-scoped) ──────────────
	mux.HandleFunc("GET /api/v1/{namespace}/notifications/channels", notif.ListChannels)
	mux.HandleFunc("POST /api/v1/{namespace}/notifications/channels", notif.CreateChannel)
	mux.HandleFunc("GET /api/v1/{namespace}/notifications/channels/{id}", notif.GetChannel)
	mux.HandleFunc("PUT /api/v1/{namespace}/notifications/channels/{id}", notif.UpdateChannel)
	mux.HandleFunc("DELETE /api/v1/{namespace}/notifications/channels/{id}", notif.DeleteChannel)
	mux.HandleFunc("POST /api/v1/{namespace}/notifications/test", notif.TestChannel)
	mux.HandleFunc("GET /api/v1/{namespace}/notifications/runtime/channels", notif.ListRuntimeChannels)

	// ── LLM Providers (namespace-scoped) ──────────────────────
	mux.HandleFunc("GET /api/v1/{namespace}/llm/providers", llmProvider.List)
	mux.HandleFunc("POST /api/v1/{namespace}/llm/providers", llmProvider.Create)
	mux.HandleFunc("GET /api/v1/{namespace}/llm/providers/{id}", llmProvider.Get)
	mux.HandleFunc("PUT /api/v1/{namespace}/llm/providers/{id}", llmProvider.Update)
	mux.HandleFunc("DELETE /api/v1/{namespace}/llm/providers/{id}", llmProvider.Delete)

	// ── MCP Servers (namespace-scoped) ──────────────────────────
	mux.HandleFunc("GET /api/v1/{namespace}/mcp", mcpH.List)
	mux.HandleFunc("POST /api/v1/{namespace}/mcp", mcpH.Create)
	mux.HandleFunc("GET /api/v1/{namespace}/mcp/{name}", mcpH.Get)
	mux.HandleFunc("PUT /api/v1/{namespace}/mcp/{name}", mcpH.Update)
	mux.HandleFunc("DELETE /api/v1/{namespace}/mcp/{name}", mcpH.Delete)

	// ── Knowledge Entries (namespace-scoped) ─────────────────
	mux.HandleFunc("GET /api/v1/{namespace}/knowledge/tags", knowledgeHandler.ListTags)
	mux.HandleFunc("POST /api/v1/{namespace}/knowledge/candidates", knowledgeHandler.CreateCandidate)
	mux.HandleFunc("GET /api/v1/{namespace}/knowledge", knowledgeHandler.List)
	mux.HandleFunc("GET /api/v1/{namespace}/knowledge/{id}", knowledgeHandler.Get)
	mux.HandleFunc("POST /api/v1/{namespace}/knowledge/search", knowledgeHandler.Search)

	// ── Immutable AgentGraph releases and consumer installations ───
	if graphRelease != nil {
		mux.HandleFunc("GET /api/v1/{namespace}/graph-releases", graphRelease.List)
		mux.HandleFunc("POST /api/v1/{namespace}/graph-releases", graphRelease.Publish)
		mux.HandleFunc("GET /api/v1/{namespace}/graph-releases/{id}", graphRelease.Get)
		mux.HandleFunc("POST /api/v1/{namespace}/graph-releases/{id}/revoke", graphRelease.Revoke)
		mux.HandleFunc("GET /api/v1/{namespace}/graph-releases/{id}/grants", graphRelease.ListGrants)
		mux.HandleFunc("POST /api/v1/{namespace}/graph-releases/{id}/grants", graphRelease.CreateGrant)
		mux.HandleFunc("DELETE /api/v1/{namespace}/graph-releases/{id}/grants/{grant_id}", graphRelease.DeleteGrant)
		mux.HandleFunc("POST /api/v1/{namespace}/graph-releases/{id}/install", graphRelease.Install)
		mux.HandleFunc("GET /api/v1/{namespace}/graph-installations", graphRelease.ListInstallations)
	}
	// ── WebSocket (namespace-scoped) ──────────────────────────
	if ws != nil {
		mux.HandleFunc("GET /api/v1/{namespace}/ws/human-approvals", ws.HandleHumanApprovals)
	}

	// ── SCM Webhooks (GitHub / GitLab / Gitea) ─────────────
	// Global (not namespace-scoped in the URL): SCM providers cannot include a
	// namespace path segment. The provider is the last path segment and the
	// per-provider body adapter normalizes the payload; the matched graph's
	// own namespace_id scopes the created run. See handler/webhook.go.
	//
	// The route MUST be registered on a separate, composed mux: on the shared
	// mux, "POST /api/v1/webhook/{provider}" is ambiguous with the
	// "POST /api/v1/{namespace}/..." routes (both match e.g.
	// "/api/v1/webhook/agents"), which makes net/http panic at registration.
	// Mounting the namespace mux under a catch-all behind the literal webhook
	// route makes the literal "webhook" segment win without changing the URL.
	if webhook != nil {
		outer := http.NewServeMux()
		outer.HandleFunc("POST /api/v1/webhook/{provider}", webhook.Handle)
		outer.Handle("/", mux)
		return outer
	}

	return mux
}
