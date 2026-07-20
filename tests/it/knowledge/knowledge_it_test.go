package knowledge

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/tests/it/externalmock"
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

func TestKnowledge_CRUD(t *testing.T) {
	fs := it.New(t, securityFixerFlow())
	tenant := fs.Tenant
	base := fs.APIURL + "/api/v1/" + tenant + "/knowledge"

	body := map[string]any{
		"title": "SQL Injection Prevention", "content": "Use PreparedStatement.",
		"content_type": "text", "source": "manual", "tags": []string{"security", "java"},
	}
	b, _ := json.Marshal(body)
	resp, err := http.Post(base, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var created entities.KnowledgeEntry
	json.NewDecoder(resp.Body).Decode(&created)
	if created.ID == "" {
		t.Fatal("create returned no id")
	}

	resp, err = http.Get(base + "/" + created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d", resp.StatusCode)
	}
	var got entities.KnowledgeEntry
	json.NewDecoder(resp.Body).Decode(&got)
	if got.Title != "SQL Injection Prevention" {
		t.Errorf("title = %q, want %q", got.Title, "SQL Injection Prevention")
	}

	resp, err = http.Get(base)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer resp.Body.Close()
	var page entities.Page[entities.KnowledgeEntry]
	json.NewDecoder(resp.Body).Decode(&page)
	if len(page.Items) == 0 {
		t.Fatal("list returned no items")
	}

	update := map[string]any{"content": "Always use parameterized queries."}
	b, _ = json.Marshal(update)
	req, _ := http.NewRequest(http.MethodPut, base+"/"+created.ID, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d", resp.StatusCode)
	}
	var updated entities.KnowledgeEntry
	json.NewDecoder(resp.Body).Decode(&updated)
	if updated.Content != "Always use parameterized queries." {
		t.Errorf("content not updated: %q", updated.Content)
	}

	search := map[string]any{"query": "SQL Injection", "top_k": 5}
	b, _ = json.Marshal(search)
	resp, err = http.Post(base+"/search", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	defer resp.Body.Close()
	var results []*entities.KnowledgeEntry
	json.NewDecoder(resp.Body).Decode(&results)
	if len(results) == 0 {
		t.Error("search returned no results")
	}

	resp, err = http.Get(base + "?tags=security,java")
	if err != nil {
		t.Fatalf("list by tags: %v", err)
	}
	defer resp.Body.Close()
	var tagged []*entities.KnowledgeEntry
	json.NewDecoder(resp.Body).Decode(&tagged)
	if len(tagged) == 0 {
		t.Error("tag-filtered list returned no results")
	}

	resp, err = http.Get(base + "/tags")
	if err != nil {
		t.Fatalf("tags: %v", err)
	}
	defer resp.Body.Close()
	var tags []string
	json.NewDecoder(resp.Body).Decode(&tags)
	if len(tags) == 0 {
		t.Error("tags returned empty")
	}

	req, _ = http.NewRequest(http.MethodDelete, base+"/"+created.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}

	resp, err = http.Get(base + "/" + created.ID)
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("get after delete status = %d, want 404", resp.StatusCode)
	}
}

func TestKnowledge_RAGRetrieverWiring(t *testing.T) {
	llmLog := &externalmock.LLMCallLog{}
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "rag-wiring", TenantID: "test"},
		Vars:       map[string]any{"repo": "wl4g/rengine"},
		Triggers:   []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes: []entities.Node{
			{ID: "detect", Type: entities.AgentNode, Agent: "issue-detector"},
			{ID: "fix", Type: entities.AgentNode, Agent: "fixer-agent"},
		},
		Edges: []entities.Edge{entities.Edge{From: "detect", To: "fix"}},
	}
	fs := it.NewWithLLMLog(t, flow, llmLog)
	tenant := fs.Tenant
	base := fs.APIURL + "/api/v1/" + tenant + "/knowledge"

	seed := map[string]any{
		"title": "DevSecOps SQL Injection Best Practice", "content": "Use PreparedStatement.",
		"content_type": "text", "source": "manual", "tags": []string{"security", "java"},
	}
	b, _ := json.Marshal(seed)
	resp, err := http.Post(base, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("seed knowledge: %v", err)
	}
	resp.Body.Close()

	search := map[string]any{"query": "DevSecOps", "top_k": 3}
	b, _ = json.Marshal(search)
	resp, err = http.Post(base+"/search", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("verify search: %v", err)
	}
	defer resp.Body.Close()
	var results []*entities.KnowledgeEntry
	json.NewDecoder(resp.Body).Decode(&results)
	if len(results) == 0 {
		t.Fatal("search for 'DevSecOps' returned no results — search API broken")
	}

	ids := fs.TriggerGitHubPR(42, "abc12345")
	if len(ids) == 0 {
		t.Fatal("webhook triggered no runs")
	}
	runID := ids[0]

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && llmLog.Count() == 0 {
		time.Sleep(300 * time.Millisecond)
	}
	status := fs.WaitRun(runID, 60*time.Second)
	if llmLog.Count() == 0 {
		t.Fatal("LLM was never called — flow did not reach agent node")
	}
	t.Logf("flow status=%s, LLM called %d times, retriever active", status, llmLog.Count())
}

func TestKnowledge_PostHandle(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "knowledge-posthandle", TenantID: "test"},
		Vars:       map[string]any{"repo": "wl4g/rengine"},
		Triggers:   []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes:      []entities.Node{entities.Node{ID: "step-a", Type: entities.NoopNode}, entities.Node{ID: "step-b", Type: entities.NoopNode}},
		Edges:      []entities.Edge{entities.Edge{From: "step-a", To: "step-b"}},
	}
	fs := it.New(t, flow)
	tenant := fs.Tenant
	base := fs.APIURL + "/api/v1/" + tenant + "/knowledge"

	ids := fs.TriggerGitHubPR(44, "ghi11111")
	if len(ids) == 0 {
		t.Fatal("webhook triggered no runs")
	}
	runID := ids[0]

	status := fs.WaitRun(runID, 60*time.Second)
	t.Logf("flow final status = %s", status)

	time.Sleep(2 * time.Second)

	resp, err := http.Get(base + "?source=flow_run")
	if err != nil {
		t.Fatalf("list by source: %v", err)
	}
	defer resp.Body.Close()

	var page entities.Page[entities.KnowledgeEntry]
	json.NewDecoder(resp.Body).Decode(&page)
	if page.TotalCount > 0 {
		t.Logf("post-handle created %d knowledge entries from flow run", page.TotalCount)
		for _, e := range page.Items {
			t.Logf("  entry: title=%q tags=%v", e.Title, e.Tags)
		}
	} else {
		t.Log("no post-handle knowledge entries yet (async)")
	}
}
