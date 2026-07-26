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
) *http.ServeMux {
	mux := http.NewServeMux()

	// ── Health & Spec ──────────────────────────────────────
	mux.HandleFunc("GET /_/healthz", health.Healthz)
	mux.HandleFunc("GET /_/openapi.yaml", swagger.OpenAPIHandler)
	mux.HandleFunc("GET /_/swagger-ui", swagger.SwaggerUIHandler)

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

	// ── Runs (namespace-scoped) ───────────────────────────────
	mux.HandleFunc("POST /api/v1/{namespace}/runs", flowRun.Create)
	mux.HandleFunc("GET /api/v1/{namespace}/runs", flowRun.List)
	mux.HandleFunc("PUT /api/v1/{namespace}/runs/{id}", flowRun.Update)
	mux.HandleFunc("GET /api/v1/{namespace}/runs/{id}", flowRun.Get)
	mux.HandleFunc("DELETE /api/v1/{namespace}/runs/{id}", flowRun.Delete)
	mux.HandleFunc("POST /api/v1/{namespace}/runs/{id}/cancel", flowRun.Cancel)
	mux.HandleFunc("POST /api/v1/{namespace}/runs/{id}/tasks", flowRun.CreateTask)
	mux.HandleFunc("GET /api/v1/{namespace}/runs/{id}/tasks", flowRun.ListTasks)
	mux.HandleFunc("GET /api/v1/{namespace}/runs/{id}/tasks/{task_id}", flowRun.GetTask)
	mux.HandleFunc("PUT /api/v1/{namespace}/runs/{id}/tasks/{task_id}", flowRun.UpdateTask)

	// ── Human Approvals ────────────────────────────────────
	mux.HandleFunc("POST /api/v1/human/approvals", human.CreateApproval)
	mux.HandleFunc("GET /api/v1/human/approvals", human.ListPendingApprovals)
	mux.HandleFunc("POST /api/v1/human/{token}/approve", human.Approve)
	mux.HandleFunc("POST /api/v1/human/{token}/reject", human.Reject)

	// ── Notification Channels (namespace-scoped) ──────────────
	mux.HandleFunc("GET /api/v1/{namespace}/notifications/channels", notif.ListChannels)
	mux.HandleFunc("POST /api/v1/{namespace}/notifications/channels", notif.CreateChannel)
	mux.HandleFunc("GET /api/v1/{namespace}/notifications/channels/{id}", notif.GetChannel)
	mux.HandleFunc("PUT /api/v1/{namespace}/notifications/channels/{id}", notif.UpdateChannel)
	mux.HandleFunc("DELETE /api/v1/{namespace}/notifications/channels/{id}", notif.DeleteChannel)
	mux.HandleFunc("POST /api/v1/{namespace}/notifications/test", notif.TestChannel)

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
