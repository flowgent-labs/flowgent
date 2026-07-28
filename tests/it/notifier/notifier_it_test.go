package notifier

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/tests/it"
)

func TestNotifier_ChannelCRUD(t *testing.T) {
	fs := it.New(t, it.SecurityFixerFlow())
	namespace := fs.Namespace
	base := fs.APIURL + "/api/v1/" + namespace + "/notifications/channels"

	ch := map[string]any{
		"name": "test-telegram", "provider": "telegram",
		"config":  map[string]any{"chat_id": "-1001234567890", "token": "test-bot-token"},
		"enabled": true,
	}
	b, _ := json.Marshal(ch)
	resp, err := http.Post(base, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var created struct{ ID string }
	json.NewDecoder(resp.Body).Decode(&created)
	if created.ID == "" {
		t.Fatal("create returned no id")
	}

	resp, err = http.Get(base)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	defer resp.Body.Close()
	var page entities.Page[entities.NotifyChannelInfo]
	json.NewDecoder(resp.Body).Decode(&page)
	if len(page.Items) == 0 {
		t.Fatal("list returned no channels")
	}

	resp, err = http.Get(base + "/" + created.ID)
	if err != nil {
		t.Fatalf("get channel: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d", resp.StatusCode)
	}

	update := map[string]any{"enabled": false}
	b, _ = json.Marshal(update)
	req, _ := http.NewRequest(http.MethodPut, base+"/"+created.ID, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update channel: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodDelete, base+"/"+created.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete channel: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
}

func TestNotifier_FlowCompletionNotification(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "nfy-complete", Namespace: "test"},
		Vars:       map[string]any{"repo": "wl4g/rengine"},
		Triggers:   []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes:      []entities.Node{{ID: "start", Type: entities.NoopNode}, {ID: "end", Type: entities.NoopNode}},
		Edges:      []entities.Edge{{From: "start", To: "end"}},
	}

	fs := it.New(t, flow)
	ch := map[string]any{
		"name": "flow-complete-telegram", "provider": "telegram",
		"config":  map[string]any{"chat_id": "-1001234567890", "token": "test-bot-token"},
		"enabled": true,
	}
	b, _ := json.Marshal(ch)
	resp, err := http.Post(fs.APIURL+"/api/v1/"+fs.Namespace+"/notifications/channels", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	resp.Body.Close()

	runIDs := fs.TriggerGitHubPR(70, "nfy-flow-sha")
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %q, want COMPLETED", status)
	}
}

func TestNotifier_HumanApprovalNotification(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "nfy-human", Namespace: "test"},
		Vars:       map[string]any{"repo": "wl4g/rengine"},
		Triggers:   []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes:      []entities.Node{{ID: "needs-approval", Type: entities.HumanNode, Timeout: "1s"}},
		Edges:      []entities.Edge{},
	}

	fs := it.New(t, flow)
	runIDs := fs.TriggerGitHubPR(71, "nfy-human-sha")
	if len(runIDs) != 1 {
		t.Fatalf("expected 1 run, got %v", runIDs)
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %q, want COMPLETED", status)
	}

	// Verify the approval record was persisted.
	resp, err := http.Get(fs.APIURL + "/api/v1/human/approvals")
	if err != nil {
		t.Fatalf("get approvals: %v", err)
	}
	defer resp.Body.Close()
	var approvals []*entities.ApprovalInfo
	json.NewDecoder(resp.Body).Decode(&approvals)
	if len(approvals) == 0 {
		t.Fatal("no human approval record found after human node completed")
	}
	t.Logf("human approval record count = %d", len(approvals))
}
