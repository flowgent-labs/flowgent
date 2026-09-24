package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/tests/it"
	"github.com/flowgent-labs/flowgent/tests/it/externalmock"
)

type candidateEnvelope struct {
	Candidate entities.KnowledgeCandidate `json:"candidate"`
	Approval  entities.ApprovalInfo       `json:"approval"`
}

type publicationEnvelope struct {
	Status    string                      `json:"status"`
	Candidate entities.KnowledgeCandidate `json:"candidate"`
}

func requestJSON(t *testing.T, method, url string, payload any, expectedStatus int, target any) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal %s %s: %v", method, url, err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, url, err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, url, err)
	}
	if resp.StatusCode != expectedStatus {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, url, resp.StatusCode, expectedStatus, data)
	}
	if target != nil && len(data) > 0 {
		if err := json.Unmarshal(data, target); err != nil {
			t.Fatalf("decode %s %s: %v; body=%s", method, url, err, data)
		}
	}
}

func completedManualRun(t *testing.T, fs *it.ITRunner) string {
	t.Helper()
	runIDs := fs.TriggerManual(nil)
	if len(runIDs) != 1 {
		t.Fatalf("manual trigger returned %d runs", len(runIDs))
	}
	if status := fs.WaitRun(runIDs[0], 30*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("source run status=%s, want COMPLETED", status)
	}
	return runIDs[0]
}

func publishKnowledge(t *testing.T, fs *it.ITRunner, runID, scope, flowName, documentID,
	description, content, idempotencyKey string, expectedRevision int64, metadata map[string]any,
) entities.KnowledgeEntry {
	t.Helper()
	base := fs.APIURL + "/api/v1/" + fs.Namespace
	payload := map[string]any{
		"source_run_id": runID, "target_scope": scope, "type": "knowledge",
		"content": content, "description": description,
		"provenance":        map[string]any{"origin": "integration_test", "source_run_id": runID},
		"expected_revision": expectedRevision, "idempotency_key": idempotencyKey,
		"metadata": metadata,
	}
	if flowName != "" {
		payload["target_flow_name"] = flowName
	}
	if documentID != "" {
		payload["target_document_id"] = documentID
	}
	var created candidateEnvelope
	requestJSON(t, http.MethodPost, base+"/knowledge/candidates", payload, http.StatusCreated, &created)
	if created.Candidate.Status != "unpublished" || created.Approval.Status != "pending" || created.Approval.ID == "" {
		t.Fatalf("candidate was not frozen behind one pending approval: %+v", created)
	}
	var published publicationEnvelope
	requestJSON(t, http.MethodPost,
		base+"/runs/"+runID+"/approvals/"+created.Approval.ID+"/approve",
		map[string]any{}, http.StatusOK, &published)
	if published.Status != "published" || published.Candidate.Status != "published" {
		t.Fatalf("candidate was not published after approval: %+v", published)
	}
	publishedID, _ := published.Candidate.Metadata["published_id"].(string)
	if publishedID == "" {
		t.Fatalf("published candidate has no stable document id: %+v", published.Candidate)
	}
	var entry entities.KnowledgeEntry
	requestJSON(t, http.MethodGet, base+"/knowledge/"+publishedID, nil, http.StatusOK, &entry)
	return entry
}

func TestKnowledge_ApprovedImmutableRevisions(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "knowledge-revisions", Namespace: "test"},
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication, SummarizeEnabled: true,
		Nodes: []entities.Node{{ID: "source", Kind: entities.NoopNode}},
	}
	fs := it.New(t, flow)
	runID := completedManualRun(t, fs)
	base := fs.APIURL + "/api/v1/" + fs.Namespace + "/knowledge"

	created := publishKnowledge(t, fs, runID, "namespace", "", "",
		"SQL Injection Prevention", "Use PreparedStatement.", "it:knowledge:create", 0,
		map[string]any{"title": "SQL Injection Prevention", "tags": []string{"security", "java"}})
	if created.ID == "" || created.Revision != 1 || created.Title != "SQL Injection Prevention" {
		t.Fatalf("unexpected first immutable revision: %+v", created)
	}

	var page entities.Page[entities.KnowledgeEntry]
	requestJSON(t, http.MethodGet, base, nil, http.StatusOK, &page)
	if len(page.Items) == 0 {
		t.Fatal("published knowledge list returned no items")
	}
	var tagged entities.Page[entities.KnowledgeEntry]
	requestJSON(t, http.MethodGet, base+"?tags=security,java", nil, http.StatusOK, &tagged)
	if len(tagged.Items) == 0 {
		t.Fatal("tag-filtered knowledge list returned no items")
	}
	var tags []string
	requestJSON(t, http.MethodGet, base+"/tags", nil, http.StatusOK, &tags)
	if len(tags) < 2 {
		t.Fatalf("knowledge tags=%v, want security and java", tags)
	}

	updated := publishKnowledge(t, fs, runID, "namespace", "", created.ID,
		"SQL Injection Prevention", "Always use parameterized queries.", "it:knowledge:update", 1,
		map[string]any{"title": "SQL Injection Prevention", "tags": []string{"security", "java"}})
	if updated.ID != created.ID || updated.Revision != 2 || updated.Content != "Always use parameterized queries." {
		t.Fatalf("unexpected second immutable revision: %+v", updated)
	}
	var results []*entities.KnowledgeEntry
	requestJSON(t, http.MethodPost, base+"/search",
		map[string]any{"query": "parameterized queries", "scope": "namespace", "top_k": 5},
		http.StatusOK, &results)
	if len(results) == 0 || results[0].Revision != 2 {
		t.Fatalf("search did not return current approved revision: %+v", results)
	}

	requestJSON(t, http.MethodPut, base+"/"+created.ID, map[string]any{"content": "tampered"}, http.StatusMethodNotAllowed, nil)
	requestJSON(t, http.MethodDelete, base+"/"+created.ID, nil, http.StatusMethodNotAllowed, nil)
	var stillPublished entities.KnowledgeEntry
	requestJSON(t, http.MethodGet, base+"/"+created.ID, nil, http.StatusOK, &stillPublished)
	if stillPublished.Revision != 2 {
		t.Fatalf("published knowledge changed after rejected mutable CRUD: %+v", stillPublished)
	}
}

func TestKnowledge_RAGRetrieverWiring(t *testing.T) {
	llmLog := &externalmock.LLMCallLog{}
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "rag-wiring", Namespace: "test"},
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication, SummarizeEnabled: true,
		Vars:     map[string]any{"repo": "wl4g/rengine"},
		Triggers: []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes: []entities.Node{
			{ID: "detect", Kind: entities.AgentNode, Agent: "issue-detector", Instruction: "Apply the DevSecOps SQL Injection Best Practice."},
			{ID: "fix", Kind: entities.AgentNode, Agent: "fixer-agent", Instruction: "Use the DevSecOps SQL Injection guidance."},
		},
		Edges: []entities.Edge{{From: "detect", To: "fix"}},
	}
	fs := it.NewWithLLMLog(t, flow, llmLog)
	base := fs.APIURL + "/api/v1/" + fs.Namespace + "/knowledge"
	sourceRunID := completedManualRun(t, fs)
	published := publishKnowledge(t, fs, sourceRunID, "namespace", "", "",
		"DevSecOps SQL Injection Best Practice",
		"DevSecOps SQL Injection Best Practice: Use PreparedStatement.",
		"it:knowledge:rag", 0,
		map[string]any{"title": "DevSecOps SQL Injection Best Practice", "tags": []string{"security", "java"}})

	var results []*entities.KnowledgeEntry
	requestJSON(t, http.MethodPost, base+"/search",
		map[string]any{"query": "DevSecOps", "scope": "namespace", "top_k": 3},
		http.StatusOK, &results)
	if len(results) == 0 || results[0].ID != published.ID {
		t.Fatal("search for 'DevSecOps' returned no results — search API broken")
	}

	baselineCalls := llmLog.Count()
	ids := fs.TriggerManual(map[string]any{"question": "DevSecOps SQL Injection Best Practice"})
	if len(ids) != 1 {
		t.Fatalf("manual trigger returned %d runs", len(ids))
	}
	runID := ids[0]
	status := fs.WaitRun(runID, 60*time.Second)
	if status != string(entities.RunCompleted) {
		t.Fatalf("RAG run status=%s, want COMPLETED", status)
	}
	if llmLog.Count() <= baselineCalls {
		t.Fatal("LLM was never called — flow did not reach agent node")
	}
	if !llmLog.ContainsSystem("Use PreparedStatement.") {
		t.Fatal("approved Knowledge was not injected into the agent system prompt")
	}
	t.Logf("flow status=%s, LLM calls=%d, approved Knowledge injected", status, llmLog.Count())
	fs.ExpectTaskCount(runID, 2)
}

func TestKnowledge_PostHandle(t *testing.T) {
	flow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "knowledge-posthandle", Namespace: "test"},
		Kind:       "flow", RuntimeMode: entities.RuntimeModeApplication, SummarizeEnabled: true,
		Vars:     map[string]any{"repo": "wl4g/rengine"},
		Triggers: []entities.TriggerDef{{Type: "webhook", Provider: "github", Events: []string{"pull_request"}}},
		Nodes: []entities.Node{
			{ID: "summarizable", Kind: entities.AgentNode, Agent: "issue-detector", Instruction: "Return a safe operational summary."},
		},
	}
	fs := it.New(t, flow)
	namespace := fs.Namespace
	base := fs.APIURL + "/api/v1/" + namespace + "/knowledge"

	ids := fs.TriggerGitHubPR(44, "ghi11111")
	if len(ids) == 0 {
		t.Fatal("webhook triggered no runs")
	}
	runID := ids[0]

	status := fs.WaitRun(runID, 60*time.Second)
	t.Logf("flow final status = %s", status)
	if status != string(entities.RunCompleted) {
		t.Fatalf("run status = %q, want COMPLETED", status)
	}

	var candidateCount, pendingCount int
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		err := fs.Pool().QueryRow(context.Background(), `SELECT COUNT(*),
			COUNT(*) FILTER (WHERE c.status='unpublished' AND a.status='pending')
			FROM knw_candidate c JOIN orh_approval a ON a.id=c.approval_id
			WHERE c.source_run_id=$1`, runID).Scan(&candidateCount, &pendingCount)
		if err != nil {
			t.Fatalf("query summary candidates: %v", err)
		}
		if candidateCount > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if candidateCount == 0 || pendingCount != candidateCount {
		t.Fatalf("post-handle candidates=%d pending approvals=%d", candidateCount, pendingCount)
	}

	resp, err := http.Get(base)
	if err != nil {
		t.Fatalf("list published knowledge: %v", err)
	}
	defer resp.Body.Close()

	var page entities.Page[entities.KnowledgeEntry]
	json.NewDecoder(resp.Body).Decode(&page)
	if page.TotalCount != 0 {
		t.Fatalf("unapproved summaries became retrieval-visible: %+v", page.Items)
	}
	t.Logf("post-handle created %d candidate(s), all gated by pending approval", candidateCount)
	fs.ExpectTaskCount(runID, 1)
}
