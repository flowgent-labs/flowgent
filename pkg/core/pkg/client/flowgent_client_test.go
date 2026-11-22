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

func TestFlowgentClientTriggerRunDecodesAcknowledgement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/team-a/agent-graphs/graph-a/trigger" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var payload struct {
			Input   map[string]any       `json:"input"`
			Trigger entities.TriggerInfo `json:"trigger"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Input["proof"] != "a2a" || payload.Trigger.Type != "a2a" {
			t.Fatalf("payload = %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"graph_run_id":"run-a","status":"PENDING","namespace":"team-a","agent_graph_id":"graph-a"}`))
	}))
	defer server.Close()

	client := NewFlowgentClient(server.URL)
	result, err := client.TriggerRun(
		context.Background(),
		"team-a",
		"graph-a",
		map[string]any{"proof": "a2a"},
		entities.TriggerInfo{Type: "a2a", Source: "admin"},
	)
	if err != nil {
		t.Fatalf("TriggerRun() error = %v", err)
	}
	if result.GraphRunID != "run-a" || result.Status != entities.GraphRunPending || result.Namespace != "team-a" || result.AgentGraphID != "graph-a" {
		t.Fatalf("result = %+v", result)
	}
}

func TestFlowgentClientNormalizesCanonicalRunResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/team-a/graph-runs":
			_, _ = w.Write([]byte(`{"id":"created","agent_graph_id":"graph-identity","agent_graph_name":"graph-a","agent_graph_revision":7,"input":{"proof":"create"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/team-a/graph-runs/fetched":
			_, _ = w.Write([]byte(`{"id":"fetched","agent_graph_id":"graph-identity","agent_graph_name":"graph-a","agent_graph_revision":7,"input":{"proof":"get"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/team-a/graph-runs":
			_, _ = w.Write([]byte(`{"items":[{"id":"listed","agent_graph_id":"graph-identity","agent_graph_name":"graph-a","agent_graph_revision":7,"input":{"proof":"list"}}],"total_count":1,"request":{"page":1,"size":20},"total_pages":1}`))
		default:
			t.Fatalf("unexpected request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := NewFlowgentClient(server.URL)
	created, err := client.CreateGraphRun(context.Background(), "team-a", &entities.GraphRun{})
	if err != nil {
		t.Fatalf("CreateGraphRun() error = %v", err)
	}
	assertGraphRunFields(t, created, "create")

	fetched, err := client.GetGraphRun(context.Background(), "team-a", "fetched")
	if err != nil {
		t.Fatalf("GetGraphRun() error = %v", err)
	}
	assertGraphRunFields(t, fetched, "get")

	page, err := client.ListGraphRuns(context.Background(), "team-a", "", "", "", "", 0, 0)
	if err != nil {
		t.Fatalf("ListGraphRuns() error = %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("ListGraphRuns() items = %d", len(page.Items))
	}
	assertGraphRunFields(t, page.Items[0], "list")
}

func assertGraphRunFields(t *testing.T, run *entities.GraphRun, proof string) {
	t.Helper()
	if run == nil {
		t.Fatal("run is nil")
	}
	if run.AgentGraphName != "graph-a" {
		t.Fatalf("graph name = %q (expected %q)", run.AgentGraphName, run.AgentGraphName)
	}
	if run.AgentGraphRevision != 7 {
		t.Fatalf("revision = %d (expected %d)", run.AgentGraphRevision, run.AgentGraphRevision)
	}
	if run.Input["proof"] != proof {
		t.Fatalf("input = %+v (expected %+v)", run.Input, run.Input)
	}
}

func TestFlowgentClientNormalizesNodeRunTransport(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/team-a/graph-runs/run-a/node-runs",
			r.Method == http.MethodPut && r.URL.Path == "/api/v1/team-a/graph-runs/run-a/node-runs/noderun-a":
			requests++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode node run request: %v", err)
			}
			if body["graph_run_id"] != "run-a" || body["node_instance_id"] != "node-a" ||
				body["execution_id"] != "exec-a" || body["parent_node_run_id"] != "parent-a" ||
				body["attempt"] != float64(2) {
				t.Fatalf("canonical node run body = %+v", body)
			}
			for _, legacy := range []string{"unexpected_legacy_id", "exec_id"} {
				if _, found := body[legacy]; found {
					t.Fatalf("removed field %q crossed HTTP boundary: %+v", legacy, body)
				}
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/team-a/graph-runs/run-a/node-runs":
			_, _ = w.Write([]byte(`[{"id":"noderun-a","graph_run_id":"run-a","node_instance_id":"node-a","attempt":2,"execution_id":"exec-a","parent_node_run_id":"parent-a","status":"SUCCESS"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/team-a/graph-runs/run-a/node-runs/noderun-a":
			_, _ = w.Write([]byte(`{"id":"noderun-a","graph_run_id":"run-a","node_instance_id":"node-a","attempt":2,"execution_id":"exec-a","parent_node_run_id":"parent-a","status":"SUCCESS"}`))
		default:
			t.Fatalf("unexpected request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := NewFlowgentClient(server.URL)
	node := &entities.NodeRun{
		BaseEntity: entities.BaseEntity{ID: "noderun-a"},
		GraphRunID: "run-a", NodeInstanceID: "node-a", Attempt: 2,
		ExecutionID: "exec-a", ParentNodeRunID: "parent-a", Status: entities.NodeRunSucceeded,
	}
	if err := client.CreateNodeRun(context.Background(), "team-a", "run-a", node); err != nil {
		t.Fatalf("CreateNodeRun() error = %v", err)
	}
	if err := client.UpdateNodeRun(context.Background(), "team-a", "run-a", "noderun-a", node); err != nil {
		t.Fatalf("UpdateNodeRun() error = %v", err)
	}
	if requests != 2 {
		t.Fatalf("node run write requests = %d", requests)
	}

	items, err := client.ListNodeRuns(context.Background(), "team-a", "run-a")
	if err != nil || len(items) != 1 {
		t.Fatalf("ListNodeRuns() items=%d error=%v", len(items), err)
	}
	assertNodeRunFields(t, items[0])
	fetched, err := client.GetNodeRun(context.Background(), "team-a", "run-a", "noderun-a")
	if err != nil {
		t.Fatalf("GetNodeRun() error = %v", err)
	}
	assertNodeRunFields(t, fetched)
}

func assertNodeRunFields(t *testing.T, noderun *entities.NodeRun) {
	t.Helper()
	if noderun == nil {
		t.Fatal("noderun is nil")
	}
	if noderun.GraphRunID != "run-a" ||
		noderun.NodeInstanceID != "node-a" ||
		noderun.ExecutionID != "exec-a" ||
		noderun.ParentNodeRunID != "parent-a" ||
		noderun.Attempt != 2 || (noderun.Attempt-1) != 1 {
		t.Fatalf("node run aliases = %+v", noderun)
	}
}

func TestFlowgentClientNormalizesCanonicalRuntimeResources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/team-a/mcp":
			_, _ = w.Write([]byte(`[{"id":"mcp-id","name":"github","transport":"http","rpc_url":"https://mcp.example.test","enabled":true}]`))
		case "/api/v1/team-a/llm/providers":
			_, _ = w.Write([]byte(`[{"id":"llm-id","name":"deepseek","type":"openai","base_uri":"https://llm.example.test/v1","timeout_ms":45000}]`))
		default:
			t.Fatalf("unexpected request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := NewFlowgentClient(server.URL)
	mcps, err := client.ListMCPs(context.Background(), "team-a")
	if err != nil || len(mcps) != 1 {
		t.Fatalf("ListMCPs() items=%d error=%v", len(mcps), err)
	}
	if mcps[0].Transport != "http" || mcps[0].Type != "streamable-http" ||
		mcps[0].RPCURL != "https://mcp.example.test" || mcps[0].URL != mcps[0].RPCURL {
		t.Fatalf("MCP aliases = %+v", mcps[0])
	}

	providers, err := client.ListLLMProviders(context.Background(), "team-a")
	if err != nil || len(providers) != 1 {
		t.Fatalf("ListLLMProviders() items=%d error=%v", len(providers), err)
	}
	if providers[0].Name != "deepseek" || providers[0].Provider != "deepseek" ||
		providers[0].BaseURI != "https://llm.example.test/v1" || providers[0].Endpoint != providers[0].BaseURI ||
		providers[0].Timeout != "45000ms" {
		t.Fatalf("LLM aliases = %+v", providers[0])
	}
}

func TestRunStateClientForwardsJobMasterLifecycleTimestamps(t *testing.T) {
	started := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	finished := started.Add(time.Minute)
	var received entities.GraphRunLifecycleUpdate
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/tenant-a/graph-runs/run-a" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	state := &GraphRunStateClient{Client: NewFlowgentClient(server.URL), Namespace: "tenant-a"}
	err := state.UpdateGraphRun(context.Background(), &entities.GraphRun{
		BaseEntity: entities.BaseEntity{ID: "run-a"},
		Status:     entities.GraphRunCompleted, StartedAt: &started, FinishedAt: &finished,
	})
	if err != nil {
		t.Fatalf("UpdateGraphRun() error = %v", err)
	}
	if received.Status != entities.GraphRunCompleted || !received.StartedAt.Equal(started) || !received.FinishedAt.Equal(finished) {
		t.Fatalf("received = %+v", received)
	}
}
