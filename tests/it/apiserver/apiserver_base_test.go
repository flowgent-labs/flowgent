package apiserver

import (
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

// TestSecurityFixer_WebhookToPR drives a real GitHub PR webhook through the
// full Flowgent engine. External services (LLM) use local mocks; MCP servers
// are real Docker containers (start separately via docker compose) whose
// upstream endpoints point to mock REST APIs on fixed ports.
// TestSecurityFixer_WebhookToPR drives the full security-autonomy-fixer pipeline
// using in-process MCP bridges (:13080/:13081) that translate MCP JSON-RPC to
// fixed-port REST mocks (:19001/:19002). No Docker required — the MCP bridge
// is a lightweight Go HTTP server started by NewWithExternalMocks.
func TestSecurityFixer_WebhookToPR(t *testing.T) {
	fs := it.NewWithExternalMocks(t, securityFixerFlow())

	runIDs := fs.TriggerGitHubPR(4, "deadbeefcafe")
	if len(runIDs) != 1 {
		t.Fatalf("webhook should have triggered exactly 1 run, got %v", runIDs)
	}

	if status := fs.WaitRun(runIDs[0], 90*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %q, want COMPLETED", status)
	}
}

func TestSecurityFixer_UnmatchedWebhookNoRun(t *testing.T) {
	fs := it.NewWithExternalMocks(t, securityFixerFlow())
	payloadRuns := fs.TriggerGitHubEvent("issues", 0, "")
	if len(payloadRuns) != 0 {
		t.Fatalf("unmatched webhook should trigger no runs, got %v", payloadRuns)
	}
}
