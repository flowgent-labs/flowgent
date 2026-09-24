package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/flowgent-labs/flowgent/common/pkg/tracing"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var webhookTracer = tracing.Tracer("flowgent/api/webhook")

// maxWebhookBody caps the request body a webhook provider may POST (1 MiB),
// well above any realistic GitHub/GitLab/Gitea push/PR payload but small
// enough to protect the externally reachable delivery endpoint from memory
// exhaustion even after AuthGuard has authorized the request.
const maxWebhookBody = 1 << 20

// WebhookHandler receives SCM (GitHub / GitLab / Gitea) webhook deliveries at
// POST /api/v1/webhook/{provider} and turns them into flow runs.
//
// This is the sync half of Phase 1 (docs/01-L1-Engine-Architecture.md §2.4
// Path A): it normalizes each provider's differently-shaped request body into
// a single canonical WebhookEvent, finds every flow that declares a matching
// `triggers: [{type: webhook, provider: ..., events: [...]}]`, and — for each
// match — creates a PENDING FlowRunInfo (persisted to PG by the sole DB
// client) and publishes an `ctrl/run/created` lifecycle event on MQTT. From
// there execution is fully async: the flow's dedicated JM run-poller picks up
// the PENDING run, JobMaster parses the DAG, and dispatches top-level task
// plans to the TM in topological order (§3.4).
//
// The handler is deliberately provider-agnostic downstream of parseEvent: all
// provider-specific decoding lives in the per-provider adapters so adding a
// new provider is a single new case, never a change to the trigger/dispatch
// logic.
type WebhookHandler struct {
	trigger          *FlowDefHandler
	logger           *utils.Logger
	defaultNamespace string
}

// NewWebhookHandler wires a WebhookHandler to the shared FlowDefHandler (which
// owns the flow cache + run store + MQTT publisher used to actually create the
// run). defaultNamespace is the namespace runs are created under when a provider
// payload can't be attributed to a specific namespace (SCM webhooks are not
// namespace-scoped in the URL).
func NewWebhookHandler(trigger *FlowDefHandler, logger *utils.Logger, defaultNamespace string) *WebhookHandler {
	return &WebhookHandler{
		trigger:          trigger,
		logger:           logger,
		defaultNamespace: coalesceNamespace(defaultNamespace),
	}
}

// WebhookEvent is the canonical, provider-neutral shape every adapter
// normalizes to. Downstream trigger-matching and run creation only ever see
// this struct, never a provider's raw JSON.
type WebhookEvent struct {
	Provider  string         // "github" | "gitlab" | "gitea"
	Event     string         // canonical event name: "push" | "pull_request" | "merge_request" ...
	Repo      string         // "owner/name" (or GitLab path_with_namespace)
	Ref       string         // e.g. "refs/heads/main" or the PR/MR source branch
	CommitSHA string         // head commit / MR SHA when available
	PRNumber  int            // pull-/merge-request number (0 if N/A)
	Sender    string         // actor login/username
	Payload   map[string]any // full raw payload, forwarded into run vars/trigger
}

// providerEvent extracts the canonical event name from the provider-specific
// delivery header. GitHub/Gitea send X-GitHub-Event / X-Gitea-Event; GitLab
// sends X-Gitlab-Event: "Push Hook" / "Merge Request Hook".
func providerEvent(provider string, r *http.Request) string {
	switch provider {
	case "github":
		return r.Header.Get("X-GitHub-Event")
	case "gitea":
		if e := r.Header.Get("X-Gitea-Event"); e != "" {
			return e
		}
		return r.Header.Get("X-Github-Event")
	case "gitlab":
		return normalizeGitLabEvent(r.Header.Get("X-Gitlab-Event"))
	}
	return ""
}

// normalizeGitLabEvent maps GitLab's "<Name> Hook" header form onto the
// canonical snake_case event names shared with GitHub/Gitea.
func normalizeGitLabEvent(h string) string {
	switch strings.TrimSpace(h) {
	case "Push Hook", "Tag Push Hook":
		return "push"
	case "Merge Request Hook":
		return "merge_request"
	case "Note Hook":
		return "note"
	case "Pipeline Hook":
		return "pipeline"
	}
	return strings.ToLower(strings.ReplaceAll(strings.TrimSuffix(strings.TrimSpace(h), " Hook"), " ", "_"))
}

// Handle is the POST /api/v1/webhook/{provider} entry point.
func (h *WebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	provider := strings.ToLower(r.PathValue("provider"))
	ctx, span := webhookTracer.Start(r.Context(), "WebhookHandler.Handle",
		trace.WithAttributes(attribute.String("webhook.provider", provider)))
	defer span.End()

	if !isSupportedProvider(provider) {
		http.Error(w, fmt.Sprintf("unsupported webhook provider: %q", provider), http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	evt, err := parseEvent(provider, providerEvent(provider, r), body)
	if err != nil {
		http.Error(w, fmt.Sprintf("parse %s webhook: %v", provider, err), http.StatusBadRequest)
		return
	}
	span.SetAttributes(
		attribute.String("webhook.event", evt.Event),
		attribute.String("webhook.repo", evt.Repo),
	)

	runIDs := h.dispatch(ctx, evt)

	w.Header().Set("Content-Type", "application/json")
	// A webhook that matched no trigger is not an error — most deliveries
	// (e.g. every non-configured event type) legitimately match nothing.
	// Return 202 Accepted with an empty triggered list so the SCM records a
	// successful delivery and doesn't retry/disable the hook.
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{
		"provider":  evt.Provider,
		"event":     evt.Event,
		"repo":      evt.Repo,
		"triggered": runIDs,
	})
}

// dispatch finds every cached flow with a matching webhook trigger and creates
// a PENDING run for it, returning the created run IDs.
func (h *WebhookHandler) dispatch(ctx context.Context, evt WebhookEvent) []string {
	flows := h.trigger.AgentFlows()
	var runIDs []string
	for _, spec := range flows {
		if spec == nil || !matchesWebhookTrigger(spec, evt) {
			continue
		}
		namespace := spec.Namespace
		if namespace == "" {
			namespace = h.defaultNamespace
		}
		vars := webhookVars(spec, evt)
		trig := entities.TriggerInfo{
			Type:   "webhook",
			Source: evt.Provider,
			Payload: map[string]any{
				"provider":   evt.Provider,
				"event":      evt.Event,
				"repo":       evt.Repo,
				"ref":        evt.Ref,
				"commit_sha": evt.CommitSHA,
				"pr_number":  evt.PRNumber,
				"sender":     evt.Sender,
			},
		}
		runID, err := h.trigger.CreateRunFromTrigger(ctx, spec.ID, namespace, vars, trig)
		if err != nil {
			h.logger.Warn("webhook: failed to create run",
				"provider", evt.Provider, "event", evt.Event, "flow_id", spec.ID, "error", err)
			continue
		}
		h.logger.Info("webhook triggered flow run",
			"provider", evt.Provider, "event", evt.Event, "repo", evt.Repo,
			"flow_id", spec.ID, "run_id", runID)
		runIDs = append(runIDs, runID)
	}
	if len(runIDs) == 0 {
		slog.Debug("webhook matched no flow trigger",
			"provider", evt.Provider, "event", evt.Event, "repo", evt.Repo)
	}
	return runIDs
}

// webhookVars overlays the incoming event's routing fields onto a copy of the
// flow's own default vars, so a webhook-triggered run can reference
// ${vars.commit_sha} / ${vars.ref} / ${vars.pr_number} etc. without clobbering
// the flow's static configuration.
func webhookVars(spec *entities.FlowInfo, evt WebhookEvent) map[string]any {
	vars := make(map[string]any, len(spec.Vars)+6)
	for k, v := range spec.Vars {
		vars[k] = v
	}
	vars["webhook_provider"] = evt.Provider
	vars["webhook_event"] = evt.Event
	if evt.Repo != "" {
		vars["webhook_repo"] = evt.Repo
	}
	if evt.Ref != "" {
		vars["ref"] = evt.Ref
	}
	if evt.CommitSHA != "" {
		vars["commit_sha"] = evt.CommitSHA
	}
	if evt.PRNumber != 0 {
		vars["pr_number"] = evt.PRNumber
	}
	return vars
}

// matchesWebhookTrigger reports whether a flow declares a webhook trigger for
// this event's provider and (if the trigger constrains events) event name.
// A trigger with no `events` list matches any event from that provider.
func matchesWebhookTrigger(spec *entities.FlowInfo, evt WebhookEvent) bool {
	for _, t := range spec.Triggers {
		if !strings.EqualFold(t.Type, "webhook") {
			continue
		}
		if !strings.EqualFold(t.Provider, evt.Provider) {
			continue
		}
		if len(t.Events) == 0 {
			return true
		}
		for _, e := range t.Events {
			if strings.EqualFold(e, evt.Event) {
				return true
			}
		}
	}
	return false
}

func isSupportedProvider(p string) bool {
	switch p {
	case "github", "gitlab", "gitea":
		return true
	}
	return false
}

// ─── Per-provider body adapters ───────────────────────────────────
//
// Each SCM ships a differently-shaped JSON body. parseEvent dispatches on the
// provider and decodes only the fields the trigger/run needs, normalizing them
// into the provider-neutral WebhookEvent. Adding a provider is a single case
// here — nothing downstream changes.

func parseEvent(provider, headerEvent string, body []byte) (WebhookEvent, error) {
	switch provider {
	case "github":
		return parseGitHub(headerEvent, body)
	case "gitea":
		return parseGitea(headerEvent, body)
	case "gitlab":
		return parseGitLab(headerEvent, body)
	}
	return WebhookEvent{}, fmt.Errorf("unsupported provider")
}

// GitHub / Gitea share the same push/PR payload layout (Gitea intentionally
// mirrors GitHub's schema), so their adapters differ only in the default event
// name and are otherwise the same decoder.
func parseGitHub(headerEvent string, body []byte) (WebhookEvent, error) {
	return parseGitHubLike("github", headerEvent, body)
}

func parseGitea(headerEvent string, body []byte) (WebhookEvent, error) {
	return parseGitHubLike("gitea", headerEvent, body)
}

func parseGitHubLike(provider, headerEvent string, body []byte) (WebhookEvent, error) {
	var raw struct {
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Action     string `json:"action"`
		Number     int    `json:"number"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		HeadCommit *struct {
			ID string `json:"id"`
		} `json:"head_commit"`
		PullRequest *struct {
			Number int `json:"number"`
			Head   struct {
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			} `json:"head"`
		} `json:"pull_request"`
		Sender struct {
			Login string `json:"login"`
		} `json:"sender"`
	}
	generic := map[string]any{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &raw); err != nil {
			return WebhookEvent{}, err
		}
		_ = json.Unmarshal(body, &generic)
	}

	event := headerEvent
	if event == "" {
		// No delivery header (e.g. a manual test POST): infer from payload.
		if raw.PullRequest != nil {
			event = "pull_request"
		} else if raw.Ref != "" {
			event = "push"
		}
	}

	evt := WebhookEvent{
		Provider: provider,
		Event:    event,
		Repo:     raw.Repository.FullName,
		Ref:      raw.Ref,
		Sender:   raw.Sender.Login,
		Payload:  generic,
	}
	if raw.HeadCommit != nil {
		evt.CommitSHA = raw.HeadCommit.ID
	}
	if evt.CommitSHA == "" {
		evt.CommitSHA = raw.After
	}
	if raw.PullRequest != nil {
		evt.PRNumber = raw.PullRequest.Number
		if evt.PRNumber == 0 {
			evt.PRNumber = raw.Number
		}
		if evt.CommitSHA == "" {
			evt.CommitSHA = raw.PullRequest.Head.SHA
		}
		if evt.Ref == "" {
			evt.Ref = raw.PullRequest.Head.Ref
		}
	}
	return evt, nil
}

// GitLab uses a distinct schema: object_kind identifies the event,
// project.path_with_namespace is the repo, and merge-request details live
// under object_attributes.
func parseGitLab(headerEvent string, body []byte) (WebhookEvent, error) {
	var raw struct {
		ObjectKind  string `json:"object_kind"`
		Ref         string `json:"ref"`
		CheckoutSHA string `json:"checkout_sha"`
		Project     struct {
			PathWithNamespace string `json:"path_with_namespace"`
		} `json:"project"`
		User struct {
			Username string `json:"username"`
		} `json:"user"`
		ObjectAttributes *struct {
			IID          int    `json:"iid"`
			SourceBranch string `json:"source_branch"`
			LastCommit   *struct {
				ID string `json:"id"`
			} `json:"last_commit"`
		} `json:"object_attributes"`
	}
	generic := map[string]any{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &raw); err != nil {
			return WebhookEvent{}, err
		}
		_ = json.Unmarshal(body, &generic)
	}

	event := headerEvent
	if event == "" {
		event = raw.ObjectKind
	}

	evt := WebhookEvent{
		Provider:  "gitlab",
		Event:     event,
		Repo:      raw.Project.PathWithNamespace,
		Ref:       raw.Ref,
		CommitSHA: raw.CheckoutSHA,
		Sender:    raw.User.Username,
		Payload:   generic,
	}
	if raw.ObjectAttributes != nil {
		evt.PRNumber = raw.ObjectAttributes.IID
		if evt.Ref == "" {
			evt.Ref = raw.ObjectAttributes.SourceBranch
		}
		if evt.CommitSHA == "" && raw.ObjectAttributes.LastCommit != nil {
			evt.CommitSHA = raw.ObjectAttributes.LastCommit.ID
		}
	}
	return evt, nil
}
