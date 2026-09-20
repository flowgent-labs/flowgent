package a2a

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	guardaccess "authguard/adapters/golang/access"
	guardmodel "authguard/adapters/golang/model"
	protocol "github.com/a2aproject/a2a-go/a2a"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/storage/pkg"
)

type sqliteTestStore struct{ db *sql.DB }

func (s *sqliteTestStore) DB() any      { return s.db }
func (s *sqliteTestStore) Close() error { return s.db.Close() }

func newSQLiteTaskStore(t *testing.T) *PersistentTaskStore {
	t.Helper()
	db := storage.NewSQLiteConn(t.Context(), t.TempDir())
	t.Cleanup(func() { _ = db.Close() })
	taskStore, err := NewPersistentTaskStore(&sqliteTestStore{db: db})
	if err != nil {
		t.Fatalf("NewPersistentTaskStore: %v", err)
	}
	return taskStore
}

func callerContext(principal string) context.Context {
	return guardaccess.WithRequestAccess(context.Background(), guardmodel.RequestAccess{PrincipalID: principal})
}

func TestPersistentTaskStoreIsolationAndVersioning(t *testing.T) {
	store := newSQLiteTaskStore(t)
	owner := callerContext("owner-token")
	other := callerContext("other-token")
	task := &protocol.Task{
		ID: "task-1", ContextID: "context-1",
		Status: protocol.TaskStatus{State: protocol.TaskStateSubmitted},
	}
	version, err := store.Save(owner, task, task, nil, protocol.TaskVersionMissing)
	if err != nil || version != 1 {
		t.Fatalf("first Save = %d, %v", version, err)
	}
	if _, _, err := store.Get(other, task.ID); err != protocol.ErrTaskNotFound {
		t.Fatalf("cross-caller Get err = %v, want ErrTaskNotFound", err)
	}
	task.Status.State = protocol.TaskStateCompleted
	version, err = store.Save(owner, task, task, task, version)
	if err != nil || version != 2 {
		t.Fatalf("second Save = %d, %v", version, err)
	}
	if _, err := store.Save(owner, task, task, task, 1); err != protocol.ErrConcurrentTaskModification {
		t.Fatalf("stale Save err = %v, want ErrConcurrentTaskModification", err)
	}
	page, err := store.List(owner, &protocol.ListTasksRequest{Status: protocol.TaskStateCompleted})
	if err != nil || page.TotalSize != 1 || len(page.Tasks) != 1 {
		t.Fatalf("List = %+v, %v", page, err)
	}
	otherPage, err := store.List(other, &protocol.ListTasksRequest{})
	if err != nil || otherPage.TotalSize != 0 {
		t.Fatalf("cross-caller List = %+v, %v", otherPage, err)
	}
}

func TestHTTPServerStandardProtocolUsesPrivateAPIServerChannel(t *testing.T) {
	var propagatedAuthorization string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		propagatedAuthorization = r.Header.Get("Authorization")
		if r.URL.Path != "/api/v1/team-a/flows" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer api.Close()

	cfg := &config.FlowgentConfig{ServiceName: "flowgent", Runtime: config.RuntimeConfig{APIServerURL: api.URL}}
	server, err := NewHTTPServer(cfg, newSQLiteTaskStore(t))
	if err != nil {
		t.Fatal(err)
	}
	a2aServer := httptest.NewServer(server.Handler)
	defer a2aServer.Close()

	cardResp, err := http.Get(a2aServer.URL + "/.well-known/agent.json")
	if err != nil {
		t.Fatalf("agent card: %v", err)
	}
	defer cardResp.Body.Close()
	var card protocol.AgentCard
	if err := json.NewDecoder(cardResp.Body).Decode(&card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}
	if card.ProtocolVersion != string(protocol.Version) || card.PreferredTransport != protocol.TransportProtocolJSONRPC || len(card.Skills) == 0 {
		t.Fatalf("invalid agent card: %+v", card)
	}

	payload := map[string]any{
		"jsonrpc": "2.0", "id": "request-1", "method": "message/send",
		"params": map[string]any{"message": map[string]any{
			"kind": "message", "messageId": "message-1", "role": "user",
			"parts": []any{map[string]any{"kind": "data", "data": map[string]any{
				"action": "list_flows", "namespace": "team-a",
			}}},
		}},
	}
	response := postJSON(t, a2aServer.URL, payload)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("message/send status = %d: %s", response.StatusCode, body)
	}
	var result struct {
		Result protocol.Task `json:"result"`
		Error  any           `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode message/send: %v", err)
	}
	if result.Error != nil || result.Result.Status.State != protocol.TaskStateCompleted {
		t.Fatalf("message/send result = %+v", result)
	}
	if propagatedAuthorization != "" {
		t.Fatalf("APIServer authorization = %q", propagatedAuthorization)
	}
}

func postJSON(t *testing.T, url string, payload any) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
