package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func TestNotifierClientUsesDefaultNamespaceAndDecodesPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/default/notifications/runtime/channels" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"total_count":0,"request":{"page":1,"size":1000},"total_pages":0}`))
	}))
	defer server.Close()

	client := NewNotifierClient(NewFlowgentClient(server.URL), "default")
	channels, err := client.ListChannels(context.Background(), "")
	if err != nil {
		t.Fatalf("ListChannels() error = %v", err)
	}
	if len(channels) != 0 {
		t.Fatalf("channels = %+v", channels)
	}
}

func TestFlowgentClientSendsBearerToken(t *testing.T) {
	const token = "workload-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewFlowgentClientWithToken(server.URL, token)
	if _, err := client.ListFlows(context.Background(), "default"); err != nil {
		t.Fatalf("ListFlows() error = %v", err)
	}
}

func TestFlowgentClientTriggerRunDecodesAcknowledgement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/team-a/flows/trigger" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var payload struct {
			AgentFlowID string               `json:"agentflow_id"`
			Vars        map[string]any       `json:"vars"`
			Trigger     entities.TriggerInfo `json:"trigger"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.AgentFlowID != "flow-a" || payload.Vars["proof"] != "a2a" || payload.Trigger.Type != "a2a" {
			t.Fatalf("payload = %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"run-a","status":"PENDING","namespace":"team-a","agentflow_id":"flow-a"}`))
	}))
	defer server.Close()

	client := NewFlowgentClient(server.URL)
	result, err := client.TriggerRun(
		context.Background(),
		"team-a",
		"flow-a",
		map[string]any{"proof": "a2a"},
		entities.TriggerInfo{Type: "a2a", Source: "admin"},
	)
	if err != nil {
		t.Fatalf("TriggerRun() error = %v", err)
	}
	if result.RunID != "run-a" || result.Status != entities.RunPending || result.Namespace != "team-a" || result.AgentFlowID != "flow-a" {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunStateClientForwardsJobMasterLifecycleTimestamps(t *testing.T) {
	started := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	finished := started.Add(time.Minute)
	var received entities.RunLifecycleUpdate
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/tenant-a/runs/run-a" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	state := &RunStateClient{Client: NewFlowgentClient(server.URL), Namespace: "tenant-a"}
	err := state.UpdateRun(context.Background(), &entities.FlowRunInfo{
		BaseEntity: entities.BaseEntity{ID: "run-a"},
		Status:     entities.RunCompleted, StartedAt: &started, FinishedAt: &finished,
	})
	if err != nil {
		t.Fatalf("UpdateRun() error = %v", err)
	}
	if received.Status != entities.RunCompleted || !received.StartedAt.Equal(started) || !received.FinishedAt.Equal(finished) {
		t.Fatalf("received = %+v", received)
	}
}
