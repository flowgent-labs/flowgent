package api

import (
	"net/http"

	"github.com/flowgent-labs/flowgent/api/pkg/handler"
	"github.com/flowgent-labs/flowgent/api/pkg/swagger"
)

// RegisterRESTRoutes returns a ServeMux with all REST API routes.
// Tenant-scoped paths use {tenant} for multi-tenant isolation.
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
) *http.ServeMux {
	mux := http.NewServeMux()

	// ── Health & Spec ──────────────────────────────────────
	mux.HandleFunc("GET /_/healthz", health.Healthz)
	mux.HandleFunc("GET /_/openapi.yaml", swagger.OpenAPIHandler)
	mux.HandleFunc("GET /_/swagger-ui", swagger.SwaggerUIHandler)

	// ── Agents (tenant-scoped) ─────────────────────────────
	mux.HandleFunc("GET /api/v1/{tenant}/agents", agentDef.List)
	mux.HandleFunc("POST /api/v1/{tenant}/agents", agentDef.Create)
	mux.HandleFunc("GET /api/v1/{tenant}/agents/{name}", agentDef.Get)
	mux.HandleFunc("PUT /api/v1/{tenant}/agents/{name}", agentDef.Update)
	mux.HandleFunc("DELETE /api/v1/{tenant}/agents/{name}", agentDef.Delete)

	// ── AgentFlows (tenant-scoped) ─────────────────────────
	mux.HandleFunc("GET /api/v1/{tenant}/flows", flowDef.List)
	mux.HandleFunc("GET /api/v1/{tenant}/flows/watch", flowDef.Watch)
	mux.HandleFunc("POST /api/v1/{tenant}/flows", flowDef.Create)
	mux.HandleFunc("GET /api/v1/{tenant}/flows/{id}", flowDef.Get)
	mux.HandleFunc("PUT /api/v1/{tenant}/flows/{id}", flowDef.Update)
	mux.HandleFunc("DELETE /api/v1/{tenant}/flows/{id}", flowDef.Delete)
	mux.HandleFunc("POST /api/v1/{tenant}/flows/trigger", flowDef.Trigger)
	mux.HandleFunc("POST /api/v1/{tenant}/flows/{id}/trigger", flowDef.TriggerByID)

	// ── Runs (tenant-scoped) ───────────────────────────────
	mux.HandleFunc("POST /api/v1/{tenant}/runs", flowRun.Create)
	mux.HandleFunc("GET /api/v1/{tenant}/runs", flowRun.List)
	mux.HandleFunc("PUT /api/v1/{tenant}/runs/{id}", flowRun.Update)
	mux.HandleFunc("GET /api/v1/{tenant}/runs/{id}", flowRun.Get)
	mux.HandleFunc("DELETE /api/v1/{tenant}/runs/{id}", flowRun.Delete)
	mux.HandleFunc("POST /api/v1/{tenant}/runs/{id}/cancel", flowRun.Cancel)
	mux.HandleFunc("POST /api/v1/{tenant}/runs/{id}/tasks", flowRun.CreateTask)
	mux.HandleFunc("GET /api/v1/{tenant}/runs/{id}/tasks", flowRun.ListTasks)
	mux.HandleFunc("GET /api/v1/{tenant}/runs/{id}/tasks/{task_id}", flowRun.GetTask)
	mux.HandleFunc("PUT /api/v1/{tenant}/runs/{id}/tasks/{task_id}", flowRun.UpdateTask)

	// ── Human Approvals ────────────────────────────────────
	mux.HandleFunc("POST /api/v1/human/approvals", human.CreateApproval)
	mux.HandleFunc("GET /api/v1/human/approvals", human.ListPendingApprovals)
	mux.HandleFunc("POST /api/v1/human/{token}/approve", human.Approve)
	mux.HandleFunc("POST /api/v1/human/{token}/reject", human.Reject)

	// ── Webhooks (global — external services, non-tenant prefix) ──
	mux.HandleFunc("POST /_/webhooks/{provider}", flowDef.Trigger)
	mux.HandleFunc("POST /_/webhooks/github", flowDef.Trigger)

	// ── Notification Channels (tenant-scoped) ──────────────
	mux.HandleFunc("GET /api/v1/{tenant}/notifications/channels", notif.ListChannels)
	mux.HandleFunc("POST /api/v1/{tenant}/notifications/channels", notif.CreateChannel)
	mux.HandleFunc("GET /api/v1/{tenant}/notifications/channels/{id}", notif.GetChannel)
	mux.HandleFunc("PUT /api/v1/{tenant}/notifications/channels/{id}", notif.UpdateChannel)
	mux.HandleFunc("DELETE /api/v1/{tenant}/notifications/channels/{id}", notif.DeleteChannel)
	mux.HandleFunc("POST /api/v1/{tenant}/notifications/test", notif.TestChannel)

	// ── LLM Providers (tenant-scoped) ──────────────────────
	mux.HandleFunc("GET /api/v1/{tenant}/llm/providers", llmProvider.List)
	mux.HandleFunc("POST /api/v1/{tenant}/llm/providers", llmProvider.Create)
	mux.HandleFunc("GET /api/v1/{tenant}/llm/providers/{id}", llmProvider.Get)
	mux.HandleFunc("PUT /api/v1/{tenant}/llm/providers/{id}", llmProvider.Update)
	mux.HandleFunc("DELETE /api/v1/{tenant}/llm/providers/{id}", llmProvider.Delete)

	// ── MCP Servers (tenant-scoped) ──────────────────────────
	mux.HandleFunc("GET /api/v1/{tenant}/mcp", mcpH.List)
	mux.HandleFunc("POST /api/v1/{tenant}/mcp", mcpH.Create)
	mux.HandleFunc("GET /api/v1/{tenant}/mcp/{name}", mcpH.Get)
	mux.HandleFunc("PUT /api/v1/{tenant}/mcp/{name}", mcpH.Update)
	mux.HandleFunc("DELETE /api/v1/{tenant}/mcp/{name}", mcpH.Delete)

	// ── WebSocket (tenant-scoped) ──────────────────────────
	if ws != nil {
		mux.HandleFunc("GET /api/v1/{tenant}/ws/human-approvals", ws.HandleHumanApprovals)
	}

	return mux
}
