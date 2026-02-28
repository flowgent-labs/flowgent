// Package sandbox implements the secure script execution worker.
//
// The SandboxRunner consumes lightweight trigger messages from a queue,
// reads scripts from a workspace volume (mounted alongside TaskManager pods),
// executes them in isolated environments, writes results back, and publishes
// completion via the queue.
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
package sandbox

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/messaging/src"
)

// ─── Trigger message ─────────────────────────────────────────

// sandboxTrigger is the lightweight message received via queue.
// Contains only path references + metadata — no script content.
type sandboxTrigger struct {
	PlanID        string                  `json:"plan_id"`
	ScriptPath    string                  `json:"script_path"`
	Runtime       string                  `json:"runtime"`
	Timeout       string                  `json:"timeout"`
	Resources     *model.SandboxResources `json:"resources,omitempty"`
	NetworkPolicy *model.NetworkPolicy    `json:"network_policy,omitempty"`
	Workspace     string                  `json:"workspace,omitempty"`
	SpanID        string                  `json:"span_id"`
}

// ─── SandboxRunner ───────────────────────────────────────────

// SandboxRunner consumes and executes sandbox triggers.
type SandboxRunner struct {
	ID        string
	queue     queue.Queue
	policy    *model.SandboxPolicy
	image     string
	workspace string

	stopCh chan struct{}
}

// NewSandboxRunner creates a sandbox worker.
func NewSandboxRunner(id string, q queue.Queue, image, workspace string, policy *model.SandboxPolicy) *SandboxRunner {
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
		ID:        id,
		queue:     q,
		policy:    policy,
		image:     image,
		workspace: workspace,
		stopCh:    make(chan struct{}),
	}
}

func (w *SandboxRunner) GetID() string { return w.ID }

// Start begins the consume-execute loop.
func (w *SandboxRunner) Start(ctx context.Context) error {
	topic := "flowgent/sandbox/exec"
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.stopCh:
			return nil
		default:
		}

		msg, err := w.queue.Dequeue(ctx, topic)
		if err != nil || msg == nil {
			continue
		}

		var trigger sandboxTrigger
		if err := json.Unmarshal(msg.Payload, &trigger); err != nil {
			w.publishError(msg.ID, "invalid trigger: "+err.Error())
			_ = w.queue.Ack(context.Background(), msg.ID)
			continue
		}

		script, err := w.readScriptFromVolume(&trigger)
		if err != nil {
			w.publishError(msg.ID, "read script: "+err.Error())
			_ = w.queue.Ack(context.Background(), msg.ID)
			continue
		}

		os.WriteFile(filepath.Join(trigger.ScriptPath, "status"), []byte("RUNNING"), 0644)

		result := w.execute(ctx, &trigger, script)
		w.publishResult(msg.ID, &trigger, result)
		_ = w.queue.Ack(context.Background(), msg.ID)
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

func (w *SandboxRunner) readScriptFromVolume(trigger *sandboxTrigger) (string, error) {
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

func (w *SandboxRunner) publishResult(msgID string, trigger *sandboxTrigger, result *model.TaskResult) {
	payload, _ := json.Marshal(result)
	_ = w.queue.Push(context.Background(), &queue.Message{
		ID:      msgID,
		Topic:   "flowgent/sandbox/result/" + trigger.PlanID,
		Payload: payload,
	})
}

func (w *SandboxRunner) publishError(msgID, errStr string) {
	w.publishResult(msgID, &sandboxTrigger{PlanID: msgID}, &model.TaskResult{Error: errStr})
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

// Avoid crypto/rand import in sandbox package — use a simple time-based ID for worker-side.
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
