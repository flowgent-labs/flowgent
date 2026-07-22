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
func boolPtr(b bool) *bool { return &b }

func securityFixerFlow() *entities.FlowInfo {
	return &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "security-autonomy-fixer"},
		Vars:       map[string]any{"repo": "wl4g/rengine", "project_key": "rengine"},
		Triggers: []entities.TriggerDef{
			{Type: "webhook", Provider: "github", Events: []string{"pull_request", "push"}},
		},
		Nodes: []entities.Node{
			{ID: "get-commit", Type: entities.ToolNode, Tool: "github", Input: map[string]any{"action": "get_latest_commit", "repo": "${vars.repo}"}},
			{ID: "scan-sonarqube", Type: entities.ToolNode, Tool: "sonarqube", Input: map[string]any{"action": "get_issues", "project_key": "${vars.project_key}", "severities": "BLOCKER,CRITICAL,MAJOR"}},
			{ID: "aggregate-issues", Type: entities.AgentNode, Agent: "issue-detector"},
			{ID: "generate-fixes", Type: entities.AgentNode, Agent: "fixer-agent"},
			{ID: "review-security", Type: entities.AgentNode, Agent: "security-reviewer"},
			{ID: "review-quality", Type: entities.AgentNode, Agent: "quality-reviewer"},
			{ID: "review-arch", Type: entities.AgentNode, Agent: "arch-reviewer"},
			{
				ID:       "committee",
				Type:     entities.CommitteeNode,
				Strategy: map[string]any{"type": "majority"},
				Input:    map[string]any{"votes": []any{"${review-security}", "${review-quality}", "${review-arch}"}},
			},
			{
				ID:         "is-approved",
				Type:       entities.ConditionNode,
				Expression: "${input.approved == true}",
				Input:      map[string]any{"approved": "${committee.decision}"},
			},
			{ID: "commit-fixes", Type: entities.ToolNode, Tool: "github", Input: map[string]any{"action": "commit_and_push", "branch": "fix/flowgent_sec_auto_fix"}},
			{ID: "create-pr", Type: entities.ToolNode, Tool: "github", Input: map[string]any{"action": "create_pull_request", "base": "main", "head": "fix/flowgent_sec_auto_fix"}},
			{ID: "rescan", Type: entities.ToolNode, Tool: "sonarqube", Input: map[string]any{"action": "get_jobs_by_commit", "repo": "${vars.repo}"}},
			{ID: "compare-results", Type: entities.AgentNode, Agent: "issue-detector"},
			{ID: "summary-report", Type: entities.AgentNode, Agent: "issue-detector"},
			{ID: "notify-pr", Type: entities.ToolNode, Tool: "github", Input: map[string]any{"action": "create_issue_comment", "pr_number": 4}},
			{ID: "end", Type: entities.NoopNode},
		},
		Edges: []entities.Edge{
			{From: "get-commit", To: "scan-sonarqube"},
			{From: "scan-sonarqube", To: "aggregate-issues"},
			{From: "aggregate-issues", To: "generate-fixes"},
			{From: "generate-fixes", To: "review-security"},
			{From: "generate-fixes", To: "review-quality"},
			{From: "generate-fixes", To: "review-arch"},
			{From: "review-security", To: "committee"},
			{From: "review-quality", To: "committee"},
			{From: "review-arch", To: "committee"},
			{From: "committee", To: "is-approved"},
			{From: "is-approved", To: "commit-fixes", Condition: boolPtr(true)},
			{From: "is-approved", To: "generate-fixes", Condition: boolPtr(false)},
			{From: "commit-fixes", To: "create-pr"},
			{From: "create-pr", To: "rescan"},
			{From: "rescan", To: "compare-results"},
			{From: "compare-results", To: "summary-report"},
			{From: "summary-report", To: "notify-pr"},
			{From: "notify-pr", To: "end"},
		},
	}
}

func TestNotifier_ChannelCRUD(t *testing.T) {
	fs := it.New(t, securityFixerFlow())
	tenant := fs.Tenant
	base := fs.APIURL + "/api/v1/" + tenant + "/notifications/channels"

	ch := map[string]any{
		"name": "test-telegram", "provider": "telegram",
		"config": map[string]any{"chat_id": "-1001234567890", "token": "test-bot-token"},
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
		BaseEntity: entities.BaseEntity{ID: "nfy-complete", TenantID: "test"},
		Vars:       map[string]any{"repo": "wl4g/rengine"},
		Triggers:   []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes:      []entities.Node{entities.Node{ID: "start", Type: entities.NoopNode}, entities.Node{ID: "end", Type: entities.NoopNode}},
		Edges:      []entities.Edge{entities.Edge{From: "start", To: "end"}},
	}

	fs := it.New(t, flow)
	ch := map[string]any{
		"name": "flow-complete-telegram", "provider": "telegram",
		"config": map[string]any{"chat_id": "-1001234567890", "token": "test-bot-token"},
		"enabled": true,
	}
	b, _ := json.Marshal(ch)
	resp, err := http.Post(fs.APIURL+"/api/v1/"+fs.Tenant+"/notifications/channels", "application/json", bytes.NewReader(b))
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
		BaseEntity: entities.BaseEntity{ID: "nfy-human", TenantID: "test"},
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

	resp, err := http.Get(fs.APIURL + "/api/v1/human/approvals?run_id=" + runIDs[0])
	if err != nil {
		t.Fatalf("get approvals: %v", err)
	}
	defer resp.Body.Close()
	t.Logf("human approval query status = %d", resp.StatusCode)
}
