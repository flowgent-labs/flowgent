package handler

import (
	"net/http"
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// TestProviderEvent verifies each provider's delivery header is mapped onto
// the canonical event name (GitLab's "<Name> Hook" form in particular must be
// normalized to the same snake_case names GitHub/Gitea use).
func TestProviderEvent(t *testing.T) {
	cases := []struct {
		provider string
		header   string
		value    string
		want     string
	}{
		{"github", "X-GitHub-Event", "pull_request", "pull_request"},
		{"github", "X-GitHub-Event", "push", "push"},
		{"gitea", "X-Gitea-Event", "push", "push"},
		{"gitea", "X-Github-Event", "pull_request", "pull_request"}, // Gitea also sends GitHub-style header
		{"gitlab", "X-Gitlab-Event", "Push Hook", "push"},
		{"gitlab", "X-Gitlab-Event", "Merge Request Hook", "merge_request"},
		{"gitlab", "X-Gitlab-Event", "Tag Push Hook", "push"},
	}
	for _, c := range cases {
		r, _ := http.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set(c.header, c.value)
		if got := providerEvent(c.provider, r); got != c.want {
			t.Errorf("providerEvent(%s, %s=%q) = %q, want %q", c.provider, c.header, c.value, got, c.want)
		}
	}
}

// TestParseGitHub verifies GitHub push + pull_request bodies normalize to the
// canonical WebhookEvent.
func TestParseGitHub(t *testing.T) {
	push := []byte(`{
		"ref": "refs/heads/main",
		"after": "abc123",
		"repository": {"full_name": "wl4g/rengine"},
		"head_commit": {"id": "abc123"},
		"sender": {"login": "octocat"}
	}`)
	evt, err := parseEvent("github", "push", push)
	if err != nil {
		t.Fatalf("parse github push: %v", err)
	}
	if evt.Event != "push" || evt.Repo != "wl4g/rengine" || evt.Ref != "refs/heads/main" || evt.CommitSHA != "abc123" || evt.Sender != "octocat" {
		t.Errorf("github push normalization wrong: %+v", evt)
	}

	pr := []byte(`{
		"action": "opened",
		"number": 4,
		"repository": {"full_name": "wl4g/rengine"},
		"pull_request": {"number": 4, "head": {"ref": "fix/flowgent_sec_auto_fix", "sha": "deadbeef"}},
		"sender": {"login": "octocat"}
	}`)
	evt, err = parseEvent("github", "pull_request", pr)
	if err != nil {
		t.Fatalf("parse github PR: %v", err)
	}
	if evt.Event != "pull_request" || evt.PRNumber != 4 || evt.CommitSHA != "deadbeef" || evt.Ref != "fix/flowgent_sec_auto_fix" {
		t.Errorf("github PR normalization wrong: %+v", evt)
	}
}

// TestParseGitLab verifies GitLab's distinct schema (object_kind,
// path_with_namespace, object_attributes) normalizes correctly.
func TestParseGitLab(t *testing.T) {
	mr := []byte(`{
		"object_kind": "merge_request",
		"project": {"path_with_namespace": "group/rengine"},
		"user": {"username": "dev1"},
		"object_attributes": {"iid": 7, "source_branch": "feature/x", "last_commit": {"id": "cafe"}}
	}`)
	evt, err := parseEvent("gitlab", "merge_request", mr)
	if err != nil {
		t.Fatalf("parse gitlab MR: %v", err)
	}
	if evt.Provider != "gitlab" || evt.Event != "merge_request" || evt.Repo != "group/rengine" ||
		evt.PRNumber != 7 || evt.Ref != "feature/x" || evt.CommitSHA != "cafe" || evt.Sender != "dev1" {
		t.Errorf("gitlab MR normalization wrong: %+v", evt)
	}
}

// TestParseGitea verifies Gitea (GitHub-compatible schema) push decoding.
func TestParseGitea(t *testing.T) {
	push := []byte(`{
		"ref": "refs/heads/dev",
		"after": "gitea-sha",
		"repository": {"full_name": "team/proj"},
		"sender": {"login": "giteauser"}
	}`)
	evt, err := parseEvent("gitea", "push", push)
	if err != nil {
		t.Fatalf("parse gitea push: %v", err)
	}
	if evt.Provider != "gitea" || evt.Event != "push" || evt.Repo != "team/proj" || evt.CommitSHA != "gitea-sha" {
		t.Errorf("gitea push normalization wrong: %+v", evt)
	}
}

// TestMatchesWebhookTrigger verifies provider + event matching, including the
// "no events list = match any event from that provider" rule and
// case-insensitivity.
func TestMatchesWebhookTrigger(t *testing.T) {
	spec := &entities.FlowInfo{
		Triggers: []entities.TriggerDef{
			{Type: "schedule", Cron: "0 0 * * *"},
			{Type: "webhook", Provider: "github", Events: []string{"push", "pull_request"}},
			{Type: "webhook", Provider: "gitlab"}, // no events → any
		},
	}
	cases := []struct {
		provider string
		event    string
		want     bool
	}{
		{"github", "push", true},
		{"github", "pull_request", true},
		{"github", "issues", false}, // not in events list
		{"gitlab", "merge_request", true},
		{"gitlab", "anything", true}, // no events list → match any
		{"gitea", "push", false},     // no gitea trigger
	}
	for _, c := range cases {
		got := matchesWebhookTrigger(spec, WebhookEvent{Provider: c.provider, Event: c.event})
		if got != c.want {
			t.Errorf("matchesWebhookTrigger(%s/%s) = %v, want %v", c.provider, c.event, got, c.want)
		}
	}
}

// TestWebhookVars verifies event routing fields are overlaid onto a copy of
// the flow's static vars without mutating the flow spec.
func TestWebhookVars(t *testing.T) {
	spec := &entities.FlowInfo{Vars: map[string]any{"repo": "rengine", "max_iterations": 3}}
	evt := WebhookEvent{Provider: "github", Event: "pull_request", Repo: "wl4g/rengine", Ref: "fix/x", CommitSHA: "sha1", PRNumber: 4}
	vars := webhookVars(spec, evt)

	if vars["repo"] != "rengine" || vars["max_iterations"] != 3 {
		t.Errorf("static vars not preserved: %+v", vars)
	}
	if vars["commit_sha"] != "sha1" || vars["ref"] != "fix/x" || vars["pr_number"] != 4 {
		t.Errorf("event routing vars not overlaid: %+v", vars)
	}
	// The flow's own Vars map must not be mutated.
	if _, ok := spec.Vars["commit_sha"]; ok {
		t.Error("webhookVars mutated the flow spec's Vars map")
	}
}
