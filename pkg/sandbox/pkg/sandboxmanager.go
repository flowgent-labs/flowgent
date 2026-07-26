// Package sandbox implements the secure script execution worker.
//
// FlowgentSandboxManager consumes trigger messages via Subscribe, reads scripts from
// a workspace volume, executes them in isolated environments, writes results back,
// and publishes completion via Publish.
package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/sandbox/pkg/seccomp"
)

// FlowgentSandboxManager consumes and executes sandbox triggers.
type FlowgentSandboxManager struct {
	ID          string
	queue       messager.IMessager
	policy      *model.SandboxPolicy
	image       string
	workspace   string
	distributed bool

	stopCh chan struct{}
}

// NewFlowgentSandboxManager creates a sandbox worker.
func NewFlowgentSandboxManager(id string, q messager.IMessager, image, workspace string, policy *model.SandboxPolicy) *FlowgentSandboxManager {
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
	return &FlowgentSandboxManager{
		ID:          id,
		queue:       q,
		policy:      policy,
		image:       image,
		workspace:   workspace,
		distributed: false,
		stopCh:      make(chan struct{}),
	}
}

func (w *FlowgentSandboxManager) GetID() string { return w.ID }

func (w *FlowgentSandboxManager) SetDistributed(v bool) { w.distributed = v }

// Start subscribes to sandbox triggers and blocks until ctx is done.
func (w *FlowgentSandboxManager) Start(ctx context.Context) error {
	subTopic := "$share/sandbox-pool/" + messager.TopicPrefix + "/+/flows/+/runs/+/sandbox/trigger"
	if !w.distributed {
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

func (w *FlowgentSandboxManager) Stop() {
	select {
	case <-w.stopCh:
	default:
		close(w.stopCh)
	}
}

// ─── Execution ──────────────────────────────────────────────

func (w *FlowgentSandboxManager) execute(ctx context.Context, trigger *model.SandboxTrigger, script string) *entities.TaskResult {
	runtime := trigger.Runtime
	if runtime == "" {
		runtime = "bash"
	}
	if !w.isRuntimeAllowed(runtime) {
		return &entities.TaskResult{Error: fmt.Sprintf("runtime %q not allowed", runtime)}
	}
	if banned := w.checkBanned(script); banned != "" {
		return &entities.TaskResult{Error: fmt.Sprintf("banned pattern: %q", banned)}
	}

	timeout := trigger.Timeout
	if timeout == "" {
		timeout = w.policy.DefaultTimeout
	}
	d, err := time.ParseDuration(timeout)
	if err != nil {
		d = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()

	netPolicy := &w.policy.Network
	if trigger.NetworkPolicy != nil {
		netPolicy = trigger.NetworkPolicy
	}

	if w.image != "" {
		return w.executeInDocker(ctx, trigger.ScriptPath, script, runtime, netPolicy, trigger.Workspace, trigger.Env, d)
	}
	return w.executeInProcess(ctx, trigger.ScriptPath, script, runtime, netPolicy, trigger.Workspace, trigger.Env, d)
}

func (w *FlowgentSandboxManager) executeInProcess(ctx context.Context, scriptPath, script, runtime string, netPolicy *model.NetworkPolicy, workspace string, env map[string]string, timeout time.Duration) *entities.TaskResult {
	scriptFile := filepath.Join(scriptPath, "script."+extForRuntime(runtime))
	if err := os.WriteFile(scriptFile, []byte(script), 0700); err != nil {
		return &entities.TaskResult{Error: "write script: " + err.Error()}
	}

	filter, ferr := seccomp.BuildFilter(netPolicy)
	if ferr != nil {
		return &entities.TaskResult{Error: "seccomp build: " + ferr.Error()}
	}

	cmd, notifCh, cerr := filter.ScriptCmd(scriptPath, runtime, workspace)
	if cerr != nil {
		return &entities.TaskResult{Error: "seccomp cmd: " + cerr.Error()}
	}

	if ctx != nil {
		wrapped := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
		wrapped.Dir = cmd.Dir
		wrapped.Env = cmd.Env
		wrapped.Stdout = cmd.Stdout
		wrapped.Stderr = cmd.Stderr
		wrapped.ExtraFiles = cmd.ExtraFiles
		cmd = wrapped
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(),
		"HOME=/tmp",
		"SANDBOX_MODE=1",
	)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	if err := cmd.Start(); err != nil {
		return &entities.TaskResult{Error: "start script: " + err.Error()}
	}

	var notifier *seccomp.Notifier
	if notifCh != nil {
		notifier = <-notifCh
		if notifier != nil {
			go notifier.Start()
		}
	}

	err := cmd.Wait()
	if notifier != nil {
		notifier.Close()
	}

	if err != nil {
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		result := &entities.TaskResult{
			Output: map[string]any{
				"exit_code": exitCode,
				"stdout":    stdout.String(), "stderr": stderr.String(),
				"runtime": runtime, "timeout": timeout.String(),
			},
			Error: fmt.Sprintf("script failed: %v", err),
		}
		w.writeResultFile(scriptPath, result)
		os.WriteFile(filepath.Join(scriptPath, "status"), []byte("FAILED"), 0644)
		return result
	}

	output := map[string]any{"exit_code": 0, "stdout": stdout.String(), "stderr": stderr.String(), "runtime": runtime}
	if parsed, ok := tryParseJSON(stdout.String()); ok {
		output["parsed"] = parsed
	}
	result := &entities.TaskResult{Output: output}
	w.writeResultFile(scriptPath, result)
	os.WriteFile(filepath.Join(scriptPath, "status"), []byte("SUCCESS"), 0644)
	return result
}

func (w *FlowgentSandboxManager) executeInDocker(ctx context.Context, scriptPath, script, runtime string, netPolicy *model.NetworkPolicy, workspace string, env map[string]string, timeout time.Duration) *entities.TaskResult {
	scriptFile := filepath.Join(scriptPath, "script."+extForRuntime(runtime))
	if err := os.WriteFile(scriptFile, []byte(script), 0700); err != nil {
		return &entities.TaskResult{Error: "write script: " + err.Error()}
	}

	containerName := fmt.Sprintf("flowgent-sandbox-%d", time.Now().UnixNano())
	memLimit := "256Mi"
	cpuLimit := "1.0"
	args := []string{"run", "--rm", "--name", containerName, "--memory=" + memLimit, "--cpus=" + cpuLimit}

	switch netPolicy.Mode {
	case "none":
		args = append(args, "--network=none")
	default:
		args = append(args, "--network=none")
	}

	args = append(args, "-v", scriptPath+":/sandbox:rw", "--workdir", "/sandbox")
	if workspace != "" {
		args = append(args, "-v", workspace+":/workspace:rw")
	}
	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}

	args = append(args, w.image, runtime, "/sandbox/script."+extForRuntime(runtime))

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		result := &entities.TaskResult{
			Output: map[string]any{"stdout": stdout.String(), "stderr": stderr.String()},
			Error:  fmt.Sprintf("docker sandbox failed: %v", err),
		}
		w.writeResultFile(scriptPath, result)
		os.WriteFile(filepath.Join(scriptPath, "status"), []byte("FAILED"), 0644)
		return result
	}

	output := map[string]any{"stdout": stdout.String(), "stderr": stderr.String(), "runtime": runtime}
	if parsed, ok := tryParseJSON(stdout.String()); ok {
		output["parsed"] = parsed
	}
	result := &entities.TaskResult{Output: output}
	w.writeResultFile(scriptPath, result)
	os.WriteFile(filepath.Join(scriptPath, "status"), []byte("SUCCESS"), 0644)
	return result
}

// ─── Helpers ─────────────────────────────────────────────────

func (w *FlowgentSandboxManager) readScriptFromVolume(trigger *model.SandboxTrigger) (string, error) {
	data, err := os.ReadFile(filepath.Join(trigger.ScriptPath, "script."+extForRuntime(trigger.Runtime)))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (w *FlowgentSandboxManager) isRuntimeAllowed(r string) bool {
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

func (w *FlowgentSandboxManager) checkBanned(script string) string {
	lower := strings.ToLower(script)
	for _, p := range w.policy.BannedCommands {
		if strings.Contains(lower, strings.ToLower(p)) {
			return p
		}
	}
	return ""
}

func (w *FlowgentSandboxManager) publishResult(trigger *model.SandboxTrigger, result *entities.TaskResult) {
	payload, _ := json.Marshal(result)
	namespaceID := trigger.Namespace
	if namespaceID == "" {
		namespaceID = "default"
	}
	resultTopic := messager.SandboxResultTopic(namespaceID, trigger.FlowID, trigger.RunID)

	_ = w.queue.Publish(context.Background(), resultTopic, &messager.InterMessage{
		ID:      trigger.PlanID,
		Payload: payload,
	})
}

func (w *FlowgentSandboxManager) publishError(trigger *model.SandboxTrigger, errStr string) {
	w.publishResult(trigger, &entities.TaskResult{Error: errStr})
}

func (w *FlowgentSandboxManager) writeResultFile(scriptPath string, result *entities.TaskResult) {
	data, _ := json.Marshal(result)
	os.WriteFile(filepath.Join(scriptPath, "result.json"), data, 0644)
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
