package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/store"
)

// TriggerDispatcher handles webhook triggers from external providers.
type TriggerDispatcher struct {
	store store.Store
	flows []model.AgentFlowSpec
}

// NewTriggerDispatcher creates a webhook trigger dispatcher.
func NewTriggerDispatcher(s store.Store, flows []model.AgentFlowSpec) *TriggerDispatcher {
	return &TriggerDispatcher{store: s, flows: flows}
}

// Reload updates the trigger matcher with new flow definitions.
func (d *TriggerDispatcher) Reload(flows []model.AgentFlowSpec) {
	d.flows = flows
}

// Webhook handles incoming webhook events.
func (d *TriggerDispatcher) Webhook(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if provider == "" {
		provider = "github"
	}

	eventType := detectEventType(provider, r)
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	matching := d.matchTriggers(provider, eventType)
	if len(matching) == 0 {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("no matching trigger"))
		return
	}

	normalized := normalizeWebhookPayload(provider, payload)
	trigger := model.TriggerInfo{Type: "webhook", Source: provider, Payload: normalized}

	var runIDs []string
	for _, flowID := range matching {
		run := &model.AgentFlowRun{
			AgentFlowID: flowID, Version: 1,
			Status: model.RunPending, Trigger: trigger,
		}
		if err := d.store.CreateAgentFlowRun(r.Context(), run); err != nil {
			slog.Error("webhook trigger failed", "error", err)
			continue
		}
		runIDs = append(runIDs, run.ID)
	}
	json.NewEncoder(w).Encode(map[string]any{"run_ids": runIDs, "provider": provider, "event": eventType})
}

func (d *TriggerDispatcher) matchTriggers(provider, eventType string) []string {
	var matched []string
	for _, wf := range d.flows {
		for _, t := range wf.Triggers {
			if t.Type != "webhook" {
				continue
			}
			if t.Provider != "" && t.Provider != provider {
				continue
			}
			if len(t.Events) > 0 && !containsEvent(t.Events, eventType) {
				continue
			}
			matched = append(matched, wf.ID)
			break
		}
	}
	return matched
}

func containsEvent(events []string, e string) bool {
	for _, ev := range events {
		if ev == e {
			return true
		}
	}
	return false
}

func detectEventType(provider string, r *http.Request) string {
	switch provider {
	case "github":
		return r.Header.Get("X-GitHub-Event")
	case "gitlab":
		return r.Header.Get("X-Gitlab-Event")
	default:
		return r.Header.Get("X-Event-Type")
	}
}

func normalizeWebhookPayload(provider string, payload map[string]any) map[string]any {
	n := make(map[string]any)
	for k, v := range payload {
		n[k] = v
	}
	switch provider {
	case "github":
		if repo, ok := payload["repository"].(map[string]any); ok {
			if name, ok := repo["full_name"].(string); ok {
				n["repo"] = name
			}
		}
	case "gitlab":
		if proj, ok := payload["project"].(map[string]any); ok {
			if name, ok := proj["path_with_namespace"].(string); ok {
				n["repo"] = name
			}
		}
	}
	return n
}
