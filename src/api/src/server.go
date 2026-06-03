package api

import (
	"net/http"

	"github.com/flowgent-labs/flowgent/api/src/handler"
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
) *http.ServeMux {
	mux := http.NewServeMux()

	// ── Health & Spec ──────────────────────────────────────
	mux.HandleFunc("GET /_/healthz", health.Healthz)
	mux.HandleFunc("GET /_/openapi.yaml", OpenAPIHandler)
	mux.HandleFunc("GET /_/swagger-ui", SwaggerUIHandler)

	// ── Agents (tenant-scoped) ─────────────────────────────
	mux.HandleFunc("GET /api/v1/{tenant}/agents", agentDef.List)
	mux.HandleFunc("POST /api/v1/{tenant}/agents", agentDef.Create)
	mux.HandleFunc("GET /api/v1/{tenant}/agents/{name}", agentDef.Get)
	mux.HandleFunc("PUT /api/v1/{tenant}/agents/{name}", agentDef.Update)
	mux.HandleFunc("DELETE /api/v1/{tenant}/agents/{name}", agentDef.Delete)

	// ── AgentFlows (tenant-scoped) ─────────────────────────
	mux.HandleFunc("GET /api/v1/{tenant}/agentflows", flowDef.List)
	mux.HandleFunc("GET /api/v1/{tenant}/agentflows/watch", flowDef.Watch)
	mux.HandleFunc("POST /api/v1/{tenant}/agentflows", flowDef.Create)
	mux.HandleFunc("GET /api/v1/{tenant}/agentflows/{id}", flowDef.Get)
	mux.HandleFunc("PUT /api/v1/{tenant}/agentflows/{id}", flowDef.Update)
	mux.HandleFunc("DELETE /api/v1/{tenant}/agentflows/{id}", flowDef.Delete)
	mux.HandleFunc("POST /api/v1/{tenant}/agentflows/trigger", flowDef.Trigger)
	mux.HandleFunc("POST /api/v1/{tenant}/agentflows/{id}/trigger", flowDef.TriggerByID)

	// ── Runs (tenant-scoped) ───────────────────────────────
	mux.HandleFunc("GET /api/v1/{tenant}/runs", flowRun.List)
	mux.HandleFunc("GET /api/v1/{tenant}/runs/{id}", flowRun.Get)
	mux.HandleFunc("DELETE /api/v1/{tenant}/runs/{id}", flowRun.Delete)
	mux.HandleFunc("POST /api/v1/{tenant}/runs/{id}/cancel", flowRun.Cancel)
	mux.HandleFunc("GET /api/v1/{tenant}/runs/{id}/tasks", flowRun.ListTasks)
	mux.HandleFunc("GET /api/v1/{tenant}/runs/{id}/tasks/{task_id}", flowRun.GetTask)
	mux.HandleFunc("PUT /api/v1/{tenant}/runs/{id}/tasks/{task_id}", flowRun.UpdateTask)

	// ── Human Approvals (global — token is unique) ────────
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

	// ── WebSocket (tenant-scoped) ──────────────────────────
	if ws != nil {
		mux.HandleFunc("GET /api/v1/{tenant}/ws/human-approvals", ws.HandleHumanApprovals)
	}

	return mux
}
