package api

import "net/http"

// RegisterRESTRoutes returns a ServeMux with all REST API routes.
// Tenant-scoped paths use {tenant} for multi-tenant isolation.
func RegisterRESTRoutes(
	health *HealthHandler,
	agentFlows *AgentFlowHandler,
	agents *AgentHandler,
	runs *RunHandler,
	human *HumanHandler,
	triggers *TriggerDispatcher,
	notif *NotificationHandler,
	ws *WSBridge,
) *http.ServeMux {
	mux := http.NewServeMux()

	// ── Health & Spec ──────────────────────────────────────
	mux.HandleFunc("GET /_/healthz", health.Healthz)
	mux.HandleFunc("GET /_/openapi.yaml", OpenAPIHandler)
	mux.HandleFunc("GET /_/swagger-ui", SwaggerUIHandler)

	// ── Agents (tenant-scoped) ─────────────────────────────
	mux.HandleFunc("GET /api/v1/{tenant}/agents", agents.List)
	mux.HandleFunc("POST /api/v1/{tenant}/agents", agents.Create)
	mux.HandleFunc("GET /api/v1/{tenant}/agents/{name}", agents.Get)
	mux.HandleFunc("PUT /api/v1/{tenant}/agents/{name}", agents.Update)
	mux.HandleFunc("DELETE /api/v1/{tenant}/agents/{name}", agents.Delete)

	// ── AgentFlows (tenant-scoped) ─────────────────────────
	mux.HandleFunc("GET /api/v1/{tenant}/agentflows", agentFlows.ListDefinitions)
	mux.HandleFunc("POST /api/v1/{tenant}/agentflows", agentFlows.CreateDefinition)
	mux.HandleFunc("GET /api/v1/{tenant}/agentflows/{id}", agentFlows.GetDefinition)
	mux.HandleFunc("PUT /api/v1/{tenant}/agentflows/{id}", agentFlows.UpdateDefinition)
	mux.HandleFunc("DELETE /api/v1/{tenant}/agentflows/{id}", agentFlows.DeleteDefinition)
	mux.HandleFunc("POST /api/v1/{tenant}/agentflows/trigger", agentFlows.Trigger)
	mux.HandleFunc("POST /api/v1/{tenant}/agentflows/{id}/trigger", agentFlows.TriggerByID)

	// ── Runs (tenant-scoped) ───────────────────────────────
	mux.HandleFunc("GET /api/v1/{tenant}/runs", runs.List)
	mux.HandleFunc("GET /api/v1/{tenant}/runs/{id}", runs.Get)
	mux.HandleFunc("DELETE /api/v1/{tenant}/runs/{id}", runs.Delete)
	mux.HandleFunc("POST /api/v1/{tenant}/runs/{id}/cancel", runs.Cancel)
	mux.HandleFunc("GET /api/v1/{tenant}/runs/{id}/tasks", runs.ListTasks)
	mux.HandleFunc("GET /api/v1/{tenant}/runs/{id}/tasks/{task_id}", runs.GetTask)

	// ── Human Approvals (global — token is unique) ────────
	mux.HandleFunc("POST /api/v1/human/{token}/approve", human.Approve)
	mux.HandleFunc("POST /api/v1/human/{token}/reject", human.Reject)

	// ── Webhooks (global — external services, non-tenant prefix) ──
	mux.HandleFunc("POST /_/webhooks/{provider}", triggers.Webhook)
	mux.HandleFunc("POST /_/webhooks/github", triggers.Webhook)

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
