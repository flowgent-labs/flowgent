// Package sandbox implements the secure script execution worker.
//
// The SandboxRunner consumes trigger messages via Subscribe, reads scripts from
// a workspace volume (mounted alongside TM and sandbox pods), executes them in
// isolated environments, writes results back, and publishes completion via Publish.
//
// Workspace path convention:
//
//	{workspace}/{tenant}/{definition_id}/runs/{run_id}/plans/{plan_id}/{span_id}/
//	  ├── script.{py,sh,js}   ← written by SandboxExecutor (TM)
//	  ├── result.json         ← written by SandboxRunner after execution
//	  └── status              ← PENDING | RUNNING | SUCCESS | FAILED
//
// Security is enforced via SandboxPolicy at three levels:
//
//	global (flowgent.yaml) → flow (AgentFlowSpec.sandbox_policy) → node (Node.network_policy)
//
// In distributed mode (sandbox as independent pods), the runner subscribes to:
//
//	$share/sandbox-pool/flowgent/v1/+/flows/+/runs/+/sandbox/trigger   (shared, load-balanced)
//
// and publishes results to:
//
//	flowgent/v1/{tenant}/flows/{flowId}/runs/{runId}/sandbox/result     (point-to-point)
package sandbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	model "github.com/flowgent-labs/flowgent/model/pkg"
)

// SandboxRunner consumes and executes sandbox triggers.
type SandboxRunner struct {
	ID          string
	queue       messager.IMessager
	policy      *model.SandboxPolicy
	image       string
	workspace   string
	distributed bool // true = $share subscription + hierarchical topics

	stopCh chan struct{}
}

// NewSandboxRunner creates a sandbox worker.
func NewSandboxRunner(id string, q messager.IMessager, image, workspace string, policy *model.SandboxPolicy) *SandboxRunner {
	if policy == nil {
		policy = &model.SandboxPolicy{
			Network:          model.NetworkPolicy{Mode: "none"},
			AllowedRuntimes:  []string{"python3", "bash", "node"},
			DefaultTimeout:   "120s",
			MaxTimeout:       "600s",
			DefaultResources: &model.SandboxResources{CPU: "500m", Memory: "256Mi"},
		}
	}
	if workspace == "" {
		workspace = os.TempDir()
	}
	return &SandboxRunner{
		ID:          id,
		queue:       q,
		policy:      policy,
		image:       image,
		workspace:   workspace,
		distributed: false,
		stopCh:      make(chan struct{}),
	}
}

func (w *SandboxRunner) GetID() string { return w.ID }

// SetDistributed enables distributed mode (shared subscription + hierarchical topics).
func (w *SandboxRunner) SetDistributed(v bool) { w.distributed = v }

// Start subscribes to sandbox triggers and blocks until ctx is done.
func (w *SandboxRunner) Start(ctx context.Context) error {
	// In distributed mode: shared subscription with wildcards for load-balanced consumption.
	// In standalone mode: use a single flat wildcard topic (backward compat for all-in-one).
	subTopic := "$share/sandbox-pool/" + messager.TopicPrefix + "/+/flows/+/runs/+/sandbox/trigger"
	if !w.distributed {
		// Legacy standalone: subscribe to wildcard trigger topic
		subTopic = messager.TopicPrefix + "/+/flows/+/runs/+/sandbox/trigger"
	}

	w.queue.Subscribe(ctx, subTopic, func(topic string, payload []byte) {
		var trigger model.SandboxTrigger
		if err := json.Unmarshal(payload, &trigger); err != nil {
			slog.Error("invalid sandbox trigger", "error", err)
			return
		}

		script, err := w.readScriptFromVolume(&trigger)
		if err != nil {
			w.publishError(&trigger, "read script: "+err.Error())
			return
		}

		os.WriteFile(filepath.Join(trigger.ScriptPath, "status"), []byte("RUNNING"), 0644)

		result := w.execute(ctx, &trigger, script)
		w.publishResult(&trigger, result)
	})

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.stopCh:
		return nil
	}
}

func (w *SandboxRunner) Stop() {
	select {
	case <-w.stopCh:
	default:
		close(w.stopCh)
	}
}

// ─── Helpers ─────────────────────────────────────────────────

func (w *SandboxRunner) readScriptFromVolume(trigger *model.SandboxTrigger) (string, error) {
	data, err := os.ReadFile(filepath.Join(trigger.ScriptPath, "script."+extForRuntime(trigger.Runtime)))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (w *SandboxRunner) isRuntimeAllowed(r string) bool {
	if len(w.policy.AllowedRuntimes) == 0 {
		return true
	}
	for _, a := range w.policy.AllowedRuntimes {
		if a == r {
			return true
		}
	}
	return false
}

func (w *SandboxRunner) checkBanned(script string) string {
	lower := strings.ToLower(script)
	for _, p := range w.policy.BannedCommands {
		if strings.Contains(lower, strings.ToLower(p)) {
			return p
		}
	}
	return ""
}

func (w *SandboxRunner) publishResult(trigger *model.SandboxTrigger, result *model.TaskResult) {
	payload, _ := json.Marshal(result)
	resultTopic := messager.SandboxResultTopic(trigger.FlowID, trigger.FlowID, trigger.RunID)

	_ = w.queue.Publish(context.Background(), resultTopic, &messager.InterMessage{
		ID:      trigger.PlanID,
		Payload: payload,
	})
}

func (w *SandboxRunner) publishError(trigger *model.SandboxTrigger, errStr string) {
	w.publishResult(trigger, &model.TaskResult{Error: errStr})
}

// ─── Utilities ───────────────────────────────────────────────

func extForRuntime(runtime string) string {
	switch runtime {
	case "python3":
		return "py"
	case "node":
		return "js"
	default:
		return "sh"
	}
}

func tryParseJSON(s string) (map[string]any, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") {
		return nil, false
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, false
	}
	return v, true
}

func cpuToDocker(cpu string) string {
	if cpu == "" {
		return "1.0"
	}
	return strings.TrimSuffix(cpu, "m") + "m"
}

func newSpanID() string {
	b := make([]byte, 8)
	_, _ = randRead(b)
	return hexEncodeToString(b)
}

func randRead(b []byte) (int, error) {
	t := time.Now().UnixNano()
	for i := range b {
		b[i] = byte(t >> (i * 8))
	}
	return len(b), nil
}

func hexEncodeToString(b []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hex[v>>4]
		out[i*2+1] = hex[v&0x0f]
	}
	return string(out)
}
