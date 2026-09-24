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
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/team-a/flows/flow-a/trigger" {
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
		_, _ = w.Write([]byte(`{"run_id":"run-a","status":"PENDING","namespace":"team-a","flow_id":"flow-a"}`))
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
	if result.RunID != "run-a" || result.Status != entities.RunPending || result.Namespace != "team-a" || result.FlowID != "flow-a" {
		t.Fatalf("result = %+v", result)
	}
}

func TestFlowgentClientNormalizesCanonicalRunResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/team-a/runs":
			_, _ = w.Write([]byte(`{"id":"created","flow_id":"flow-identity","flow_name":"flow-a","flow_revision":7,"input":{"proof":"create"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/team-a/runs/fetched":
			_, _ = w.Write([]byte(`{"id":"fetched","flow_id":"flow-identity","flow_name":"flow-a","flow_revision":7,"input":{"proof":"get"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/team-a/runs":
			_, _ = w.Write([]byte(`{"items":[{"id":"listed","flow_id":"flow-identity","flow_name":"flow-a","flow_revision":7,"input":{"proof":"list"}}],"total_count":1,"request":{"page":1,"size":20},"total_pages":1}`))
		default:
			t.Fatalf("unexpected request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := NewFlowgentClient(server.URL)
	created, err := client.CreateRun(context.Background(), "team-a", &entities.FlowRunInfo{})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	assertRunAliases(t, created, "create")

	fetched, err := client.GetRun(context.Background(), "team-a", "fetched")
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	assertRunAliases(t, fetched, "get")

	page, err := client.ListRuns(context.Background(), "team-a", "", "", "", "", 0, 0)
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("ListRuns() items = %d", len(page.Items))
	}
	assertRunAliases(t, page.Items[0], "list")
}

func assertRunAliases(t *testing.T, run *entities.FlowRunInfo, proof string) {
	t.Helper()
	if run == nil {
		t.Fatal("run is nil")
	}
	if run.FlowName != "flow-a" || run.AgentFlowID != "flow-a" {
		t.Fatalf("flow aliases = name %q, legacy %q", run.FlowName, run.AgentFlowID)
	}
	if run.FlowRevision != 7 || run.Version != 7 {
		t.Fatalf("revision aliases = revision %d, version %d", run.FlowRevision, run.Version)
	}
	if run.Input["proof"] != proof || run.Vars["proof"] != proof {
		t.Fatalf("input aliases = input %+v, vars %+v", run.Input, run.Vars)
	}
}

func TestFlowgentClientNormalizesNodeRunTransport(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/team-a/runs/run-a/node-runs",
			r.Method == http.MethodPut && r.URL.Path == "/api/v1/team-a/runs/run-a/node-runs/task-a":
			requests++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode node run request: %v", err)
			}
			if body["run_id"] != "run-a" || body["node_key"] != "node-a" ||
				body["execution_id"] != "exec-a" || body["parent_node_run_id"] != "parent-a" ||
				body["attempt"] != float64(2) {
				t.Fatalf("canonical node run body = %+v", body)
			}
			for _, legacy := range []string{"agentflow_run_id", "node_id", "exec_id", "parent_task_run_id"} {
				if _, found := body[legacy]; found {
					t.Fatalf("legacy field %q crossed HTTP boundary: %+v", legacy, body)
				}
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/team-a/runs/run-a/node-runs":
			_, _ = w.Write([]byte(`[{"id":"task-a","run_id":"run-a","node_key":"node-a","attempt":2,"execution_id":"exec-a","parent_node_run_id":"parent-a","status":"SUCCESS"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/team-a/runs/run-a/node-runs/task-a":
			_, _ = w.Write([]byte(`{"id":"task-a","run_id":"run-a","node_key":"node-a","attempt":2,"execution_id":"exec-a","parent_node_run_id":"parent-a","status":"SUCCESS"}`))
		default:
			t.Fatalf("unexpected request = %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := NewFlowgentClient(server.URL)
	legacy := &entities.TaskRunInfo{
		BaseEntity:     entities.BaseEntity{ID: "task-a"},
		AgentFlowRunID: "run-a", NodeID: "node-a", RetryCount: 1,
		ExecID: "exec-a", ParentTaskRunID: "parent-a", Status: entities.Success,
	}
	if err := client.CreateTaskRun(context.Background(), "team-a", "run-a", legacy); err != nil {
		t.Fatalf("CreateTaskRun() error = %v", err)
	}
	if err := client.UpdateTaskRun(context.Background(), "team-a", "run-a", "task-a", legacy); err != nil {
		t.Fatalf("UpdateTaskRun() error = %v", err)
	}
	if requests != 2 {
		t.Fatalf("node run write requests = %d", requests)
	}

	items, err := client.ListTaskRuns(context.Background(), "team-a", "run-a")
	if err != nil || len(items) != 1 {
		t.Fatalf("ListTaskRuns() items=%d error=%v", len(items), err)
	}
	assertTaskAliases(t, items[0])
	fetched, err := client.GetTaskRun(context.Background(), "team-a", "run-a", "task-a")
	if err != nil {
		t.Fatalf("GetTaskRun() error = %v", err)
	}
	assertTaskAliases(t, fetched)
}

func assertTaskAliases(t *testing.T, task *entities.TaskRunInfo) {
	t.Helper()
	if task == nil {
		t.Fatal("task is nil")
	}
	if task.RunID != "run-a" || task.AgentFlowRunID != "run-a" ||
		task.NodeKey != "node-a" || task.NodeID != "node-a" ||
		task.ExecutionID != "exec-a" || task.ExecID != "exec-a" ||
		task.ParentNodeRunID != "parent-a" || task.ParentTaskRunID != "parent-a" ||
		task.Attempt != 2 || task.RetryCount != 1 {
		t.Fatalf("node run aliases = %+v", task)
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
