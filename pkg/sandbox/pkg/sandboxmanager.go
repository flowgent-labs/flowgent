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
	slots       int
	namespaceID string
	flowID      string

	stopCh chan struct{}
}

type sandboxJob struct {
	topic   string
	payload []byte
}

// SandboxSlotWorker is the smallest sandbox compute unit inside one sandbox pod.
type SandboxSlotWorker struct {
	ID      string
	manager *FlowgentSandboxManager
	jobs    <-chan sandboxJob
}

func (sw *SandboxSlotWorker) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-sw.jobs:
			sw.manager.handleTrigger(ctx, sw.ID, job.topic, job.payload)
		}
	}
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
		slots:       1,
		stopCh:      make(chan struct{}),
	}
}

func (w *FlowgentSandboxManager) GetID() string { return w.ID }

func (w *FlowgentSandboxManager) SetDistributed(v bool) { w.distributed = v }

func (w *FlowgentSandboxManager) SetSlots(slots int) {
	if slots <= 0 {
		slots = 1
	}
	w.slots = slots
}

func (w *FlowgentSandboxManager) SetScope(namespaceID, flowID string) {
	w.namespaceID = namespaceID
	w.flowID = flowID
}

// Start subscribes to sandbox triggers and blocks until ctx is done.
func (w *FlowgentSandboxManager) Start(ctx context.Context) error {
	subTopic := w.subscriptionTopic()
	slotCount := w.slots
	if slotCount <= 0 {
		slotCount = 1
	}
	slog.Info("sandbox manager subscribing",
		"id", w.ID, "topic", subTopic, "distributed", w.distributed,
		"slots", slotCount, "namespace", w.namespaceID, "flow", w.flowID)

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan sandboxJob, slotCount)
	for i := 0; i < slotCount; i++ {
		worker := &SandboxSlotWorker{
			ID:      fmt.Sprintf("%s-slot-%d", w.ID, i),
			manager: w,
			jobs:    jobs,
		}
		go worker.Start(workerCtx)
	}

	if err := w.queue.Subscribe(ctx, subTopic, func(topic string, payload []byte) {
		select {
		case jobs <- sandboxJob{topic: topic, payload: payload}:
		case <-ctx.Done():
		}
	}); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.stopCh:
		return nil
	}
}

func (w *FlowgentSandboxManager) subscriptionTopic() string {
	filter := messager.TopicPrefix + "/+/flows/+/runs/+/sandbox/trigger"
	group := "sandbox-pool"
	if w.namespaceID != "" && w.flowID != "" {
		filter = fmt.Sprintf("%s/%s/flows/%s/runs/+/sandbox/trigger", messager.TopicPrefix, w.namespaceID, w.flowID)
		group = "sandbox-pool-" + sanitizeShareGroup(w.namespaceID) + "-" + sanitizeShareGroup(w.flowID)
	}
	if !w.distributed {
		return filter
	}
	return "$share/" + group + "/" + filter
}

func sanitizeShareGroup(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

func (w *FlowgentSandboxManager) handleTrigger(ctx context.Context, workerID, topic string, payload []byte) {
	var trigger model.SandboxTrigger
	if err := json.Unmarshal(payload, &trigger); err != nil {
		slog.Error("invalid sandbox trigger", "worker", workerID, "error", err)
		return
	}
	slog.Info("sandbox trigger received", "worker", workerID, "topic", topic, "plan_id", trigger.PlanID, "runtime", trigger.Runtime)

	script, err := w.readScriptFromVolume(&trigger)
	if err != nil {
		if err := w.publishError(ctx, &trigger, "read script: "+err.Error()); err != nil {
			slog.Error("sandbox result publish failed", "worker", workerID, "plan_id", trigger.PlanID, "error", err)
		}
		return
	}

	os.WriteFile(filepath.Join(trigger.ScriptPath, "status"), []byte("RUNNING"), 0644)

	result := w.execute(ctx, &trigger, script)
	if err := w.publishResult(ctx, &trigger, result); err != nil {
		slog.Error("sandbox result publish failed", "worker", workerID, "plan_id", trigger.PlanID, "error", err)
		return
	}
	slog.Info("sandbox result published", "worker", workerID, "plan_id", trigger.PlanID, "has_error", result != nil && result.Error != "")
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
	baseEnv := cmd.Env
	if len(baseEnv) == 0 {
		baseEnv = os.Environ()
	}
	cmd.Env = append(baseEnv,
		"HOME=/tmp",
		"SANDBOX_MODE=1",
	)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	if err := cmd.Start(); err != nil {
		return &entities.TaskResult{Error: "start script: " + err.Error()}
	}
	for _, f := range cmd.ExtraFiles {
		if f != nil {
			_ = f.Close()
		}
	}

	var notifier *seccomp.Notifier
	if notifCh != nil {
		select {
		case notifier = <-notifCh:
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return &entities.TaskResult{Error: "seccomp notifier handshake: " + ctx.Err().Error()}
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return &entities.TaskResult{Error: "seccomp notifier handshake timed out"}
		}
		if notifier != nil {
			go notifier.Start()
		} else {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return &entities.TaskResult{Error: "seccomp notifier handshake failed"}
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
	attachParsedOutput(output, stdout.String())
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
	attachParsedOutput(output, stdout.String())
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

func (w *FlowgentSandboxManager) publishResult(ctx context.Context, trigger *model.SandboxTrigger, result *entities.TaskResult) error {
	payload, _ := json.Marshal(result)
	namespaceID := trigger.Namespace
	if namespaceID == "" {
		namespaceID = "default"
	}
	resultTopic := messager.SandboxResultTopic(namespaceID, trigger.FlowID, trigger.RunID)

	msg := &messager.InterMessage{ID: trigger.PlanID, Payload: payload}
	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		if err := w.queue.Publish(ctx, resultTopic, msg); err == nil {
			return nil
		} else {
			lastErr = err
			slog.Warn("sandbox result publish retry",
				"id", w.ID, "plan_id", trigger.PlanID, "topic", resultTopic,
				"attempt", attempt, "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	return lastErr
}

func (w *FlowgentSandboxManager) publishError(ctx context.Context, trigger *model.SandboxTrigger, errStr string) error {
	return w.publishResult(ctx, trigger, &entities.TaskResult{Error: errStr})
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
	if s == "" {
		return nil, false
	}
	if v, ok := parseJSONObject(s); ok {
		return v, true
	}
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if v, ok := parseJSONObject(line); ok {
			return v, true
		}
	}
	return nil, false
}

func parseJSONObject(s string) (map[string]any, bool) {
	if !strings.HasPrefix(s, "{") {
		return nil, false
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, false
	}
	return v, true
}

func attachParsedOutput(output map[string]any, stdout string) {
	parsed, ok := tryParseJSON(stdout)
	if !ok {
		return
	}
	output["parsed"] = parsed
	for k, v := range parsed {
		if _, exists := output[k]; !exists {
			output[k] = v
		}
	}
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
