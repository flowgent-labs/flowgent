package apiserver

import (
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/tests/it"
)

// TestSecurityFixer_WebhookToPR drives the full security-autonomy-fixer pipeline
// using in-process MCP bridges (:13080/:13081) that translate MCP JSON-RPC to
// fixed-port REST mocks (:19001/:19002). No Docker required — the MCP bridge
// is a lightweight Go HTTP server started by NewWithExternalMocks.
func TestSecurityFixer_WebhookToPR(t *testing.T) {
	fs := it.NewWithExternalMocks(t, it.SecurityFixerFlow())

	runIDs := fs.TriggerGitHubPR(4, "deadbeefcafe")
	if len(runIDs) != 1 {
		t.Fatalf("webhook should have triggered exactly 1 run, got %v", runIDs)
	}

	if status := fs.WaitRun(runIDs[0], 90*time.Second); status != string(entities.RunCompleted) {
		t.Fatalf("run status = %q, want COMPLETED", status)
	}
	fs.ExpectTaskCount(runIDs[0], 12)
}

func TestSecurityFixer_UnmatchedWebhookNoRun(t *testing.T) {
	fs := it.NewWithExternalMocks(t, it.SecurityFixerFlow())
	payloadRuns := fs.TriggerGitHubEvent("issues", 0, "")
	if len(payloadRuns) != 0 {
		t.Fatalf("unmatched webhook should trigger no runs, got %v", payloadRuns)
	}
}
