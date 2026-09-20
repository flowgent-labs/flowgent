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
	flowDef *handler.FlowDefHandler,
	agentDef *handler.AgentDefHandler,
	flowRun *handler.FlowRunHandler,
	human *handler.HumanHandler,
	notif *handler.NotifierHandler,
	ws *handler.NotifierWSBridge,
	llmProvider *handler.LlmProviderHandler,
	mcpH *handler.McpHandler,
	webhook *handler.WebhookHandler,
	knowledgeHandler *handler.KnowledgeHandler,
	traceHandler *handler.TraceHandler,
	flowRelease *handler.FlowReleaseHandler,
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

	// ── AgentFlows (namespace-scoped) ─────────────────────────
	mux.HandleFunc("GET /api/v1/{namespace}/flows", flowDef.List)
	mux.HandleFunc("GET /api/v1/{namespace}/flows/watch", flowDef.Watch)
	mux.HandleFunc("POST /api/v1/{namespace}/flows", flowDef.Create)
	mux.HandleFunc("GET /api/v1/{namespace}/flows/{id}", flowDef.Get)
	mux.HandleFunc("PUT /api/v1/{namespace}/flows/{id}", flowDef.Update)
	mux.HandleFunc("DELETE /api/v1/{namespace}/flows/{id}", flowDef.Delete)
	mux.HandleFunc("POST /api/v1/{namespace}/flows/trigger", flowDef.Trigger)
	mux.HandleFunc("POST /api/v1/{namespace}/flows/{id}/trigger", flowDef.TriggerByID)

	// Flow-scoped run aliases make exact-Flow grants cover its complete run,
	// task, approval, and trace surface without trusting query parameters.
	mux.HandleFunc("GET /api/v1/{namespace}/flows/{flow_id}/runs", flowRun.List)
	mux.HandleFunc("GET /api/v1/{namespace}/flows/{flow_id}/runs/{run_id}", flowRun.Get)
	mux.HandleFunc("DELETE /api/v1/{namespace}/flows/{flow_id}/runs/{run_id}", flowRun.Delete)
	mux.HandleFunc("POST /api/v1/{namespace}/flows/{flow_id}/runs/{run_id}/cancel", flowRun.Cancel)
	mux.HandleFunc("GET /api/v1/{namespace}/flows/{flow_id}/runs/{run_id}/tasks", flowRun.ListTasks)
	mux.HandleFunc("GET /api/v1/{namespace}/flows/{flow_id}/runs/{run_id}/tasks/{task_id}", flowRun.GetTask)
	mux.HandleFunc("GET /api/v1/{namespace}/flows/{flow_id}/runs/{run_id}/approvals", human.ListRunApprovals)
	mux.HandleFunc("POST /api/v1/{namespace}/flows/{flow_id}/runs/{run_id}/approvals", human.CreateRunApproval)
	mux.HandleFunc("POST /api/v1/{namespace}/flows/{flow_id}/runs/{run_id}/approvals/{token}/{decision}", human.ResolveRunApproval)
	if traceHandler != nil {
		mux.HandleFunc("GET /api/v1/{namespace}/flows/{flow_id}/runs/{run_id}/trace", traceHandler.GetRunTrace)
	}
	if runtimeConfig != nil {
		mux.HandleFunc("GET /api/v1/{namespace}/runtime-config", runtimeConfig.GetNamespace)
		mux.HandleFunc("PUT /api/v1/{namespace}/runtime-config/environment", runtimeConfig.UpdateNamespaceEnvironment)
		mux.HandleFunc("PUT /api/v1/{namespace}/runtime-config/secrets", runtimeConfig.UpdateNamespaceSecrets)
		mux.HandleFunc("GET /api/v1/{namespace}/flows/{flow_id}/runtime-config", runtimeConfig.GetFlow)
		mux.HandleFunc("PUT /api/v1/{namespace}/flows/{flow_id}/runtime-config/environment", runtimeConfig.UpdateFlowEnvironment)
		mux.HandleFunc("PUT /api/v1/{namespace}/flows/{flow_id}/runtime-config/secrets", runtimeConfig.UpdateFlowSecrets)
		mux.HandleFunc("GET /api/v1/{namespace}/flows/{flow_id}/runtime-config/resolved", runtimeConfig.ResolveFlow)
	}
	// ── Runtime Skills (kind=skill Flow definitions) ───────────
	mux.HandleFunc("GET /api/v1/{namespace}/skills", flowDef.ListSkills)
	mux.HandleFunc("POST /api/v1/{namespace}/skills", flowDef.CreateSkill)
	mux.HandleFunc("GET /api/v1/{namespace}/skills/{id}", flowDef.GetSkill)
	mux.HandleFunc("PUT /api/v1/{namespace}/skills/{id}", flowDef.UpdateSkill)
	mux.HandleFunc("DELETE /api/v1/{namespace}/skills/{id}", flowDef.DeleteSkill)

	// ── Runs (namespace-scoped) ───────────────────────────────
	mux.HandleFunc("POST /api/v1/{namespace}/runs", flowRun.Create)
	mux.HandleFunc("GET /api/v1/{namespace}/runs", flowRun.List)
	mux.HandleFunc("GET /api/v1/{namespace}/runs/metrics", flowRun.Metrics)
	mux.HandleFunc("PUT /api/v1/{namespace}/runs/{id}", flowRun.Update)
	mux.HandleFunc("GET /api/v1/{namespace}/runs/{id}", flowRun.Get)
	mux.HandleFunc("DELETE /api/v1/{namespace}/runs/{id}", flowRun.Delete)
	mux.HandleFunc("POST /api/v1/{namespace}/runs/{id}/cancel", flowRun.Cancel)
	mux.HandleFunc("POST /api/v1/{namespace}/runs/{id}/tasks", flowRun.CreateTask)
	mux.HandleFunc("GET /api/v1/{namespace}/runs/{id}/tasks", flowRun.ListTasks)
	mux.HandleFunc("GET /api/v1/{namespace}/runs/{id}/tasks/{task_id}", flowRun.GetTask)
	mux.HandleFunc("PUT /api/v1/{namespace}/runs/{id}/tasks/{task_id}", flowRun.UpdateTask)
	mux.HandleFunc("GET /api/v1/{namespace}/runs/{id}/approvals", human.ListRunApprovals)
	mux.HandleFunc("POST /api/v1/{namespace}/runs/{id}/approvals", human.CreateRunApproval)
	mux.HandleFunc("POST /api/v1/{namespace}/runs/{id}/approvals/{token}/{decision}", human.ResolveRunApproval)
	if traceHandler != nil {
		mux.HandleFunc("GET /api/v1/{namespace}/runs/{id}/trace", traceHandler.GetRunTrace)
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
	mux.HandleFunc("GET /api/v1/{namespace}/knowledge", knowledgeHandler.List)
	mux.HandleFunc("POST /api/v1/{namespace}/knowledge", knowledgeHandler.Create)
	mux.HandleFunc("GET /api/v1/{namespace}/knowledge/{id}", knowledgeHandler.Get)
	mux.HandleFunc("PUT /api/v1/{namespace}/knowledge/{id}", knowledgeHandler.Update)
	mux.HandleFunc("DELETE /api/v1/{namespace}/knowledge/{id}", knowledgeHandler.Delete)
	mux.HandleFunc("POST /api/v1/{namespace}/knowledge/search", knowledgeHandler.Search)

	// ── Immutable Flow releases and consumer installations ───
	if flowRelease != nil {
		mux.HandleFunc("GET /api/v1/{namespace}/flow-releases", flowRelease.List)
		mux.HandleFunc("POST /api/v1/{namespace}/flow-releases", flowRelease.Publish)
		mux.HandleFunc("GET /api/v1/{namespace}/flow-releases/{id}", flowRelease.Get)
		mux.HandleFunc("POST /api/v1/{namespace}/flow-releases/{id}/revoke", flowRelease.Revoke)
		mux.HandleFunc("GET /api/v1/{namespace}/flow-releases/{id}/grants", flowRelease.ListGrants)
		mux.HandleFunc("POST /api/v1/{namespace}/flow-releases/{id}/grants", flowRelease.CreateGrant)
		mux.HandleFunc("DELETE /api/v1/{namespace}/flow-releases/{id}/grants/{grant_id}", flowRelease.DeleteGrant)
		mux.HandleFunc("POST /api/v1/{namespace}/flow-releases/{id}/install", flowRelease.Install)
		mux.HandleFunc("GET /api/v1/{namespace}/flow-installations", flowRelease.ListInstallations)
	}
	// ── WebSocket (namespace-scoped) ──────────────────────────
	if ws != nil {
		mux.HandleFunc("GET /api/v1/{namespace}/ws/human-approvals", ws.HandleHumanApprovals)
	}

	// ── SCM Webhooks (GitHub / GitLab / Gitea) ─────────────
	// Global (not namespace-scoped in the URL): SCM providers cannot include a
	// namespace path segment. The provider is the last path segment and the
	// per-provider body adapter normalizes the payload; the matched flow's
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
