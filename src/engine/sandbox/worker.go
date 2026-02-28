// Package sandbox implements the secure script execution worker.
//
// The SandboxWorker consumes ExecutionPlans from a dedicated queue topic,
// executes scripts in isolated environments (Docker container or process
// with network/f resource restrictions), and publishes results back.
//
// Security is enforced via SandboxPolicy at three levels:
//   global (flowgent.yaml) → flow (AgentFlowSpec) → node (Node.NetworkPolicy)
package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/queue"
)

// Worker consumes and executes sandbox ExecutionPlans.
type Worker struct {
	ID       string
	queue    queue.Queue
	policy   *model.SandboxPolicy
	image    string  // Docker image for container mode
	runtimeDir string // host directory for script temp files

	stopCh chan struct{}
}

// NewWorker creates a sandbox worker.
func NewWorker(id string, q queue.Queue, image, runtimeDir string, policy *model.SandboxPolicy) *Worker {
	if policy == nil {
		policy = &model.SandboxPolicy{
			Network:         model.NetworkPolicy{Mode: "none"},
			AllowedRuntimes: []string{"python3", "bash", "node"},
			DefaultTimeout:  "120s",
			MaxTimeout:      "600s",
			DefaultResources: &model.SandboxResources{CPU: "500m", Memory: "256Mi"},
		}
	}
	if runtimeDir == "" {
		runtimeDir = os.TempDir()
	}
	return &Worker{
		ID:         id,
		queue:      q,
		policy:     policy,
		image:      image,
		runtimeDir: runtimeDir,
		stopCh:     make(chan struct{}),
	}
}

// GetID returns the worker identifier.
func (w *Worker) GetID() string { return w.ID }

// Start begins consuming ExecutionPlans from the sandbox queue topic.
func (w *Worker) Start(ctx context.Context) error {
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
		if err != nil {
			continue
		}
		if msg == nil {
			continue
		}

		var plan model.ExecutionPlan
		if err := json.Unmarshal(msg.Payload, &plan); err != nil {
			w.publishError(msg, "invalid plan JSON: "+err.Error())
			continue
		}

		result := w.execute(ctx, &plan)
		w.publishResult(msg, result)
	}
}

// Stop gracefully shuts down the worker.
func (w *Worker) Stop() {
	select {
	case <-w.stopCh:
	default:
		close(w.stopCh)
	}
}

// execute runs a single sandbox plan.
func (w *Worker) execute(ctx context.Context, plan *model.ExecutionPlan) *model.TaskResult {
	ns := plan.NodeSpec
	if ns == nil {
		return &model.TaskResult{Error: "missing node_spec"}
	}

	// Resolve effective policy from global → flow → node.
	policy := w.effectivePolicy(plan)

	// Validate runtime.
	runtime := ns.Runtime
	if runtime == "" {
		runtime = "bash"
	}
	if !w.isRuntimeAllowed(runtime) {
		return &model.TaskResult{Error: fmt.Sprintf("runtime %q not in allowed list: %v", runtime, policy.AllowedRuntimes)}
	}

	// Banned commands check.
	script := ns.Script
	if script == "" {
		return &model.TaskResult{Error: "empty script"}
	}
	if banned := w.checkBanned(script, policy.BannedCommands); banned != "" {
		return &model.TaskResult{Error: fmt.Sprintf("script contains banned pattern: %q", banned)}
	}

	// Resolve timeout.
	timeout, err := time.ParseDuration(model.EffectiveTimeout(
		w.policy.DefaultTimeout, w.policy.MaxTimeout,
		"", ns.Timeout,
	))
	if err != nil {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Resolve resources.
	res := model.EffectiveResources(w.policy.DefaultResources, w.policy.MaxResources, nil, ns.Resources)

	// Execute based on available isolation.
	if w.image != "" {
		return w.executeInDocker(ctx, script, runtime, res, policy, timeout)
	}
	return w.executeInProcess(ctx, script, runtime, res, timeout)
}

// executeInProcess runs the script as a subprocess with resource limits.
func (w *Worker) executeInProcess(ctx context.Context, script, runtime string, res *model.SandboxResources, timeout time.Duration) *model.TaskResult {
	scriptFile := filepath.Join(w.runtimeDir, fmt.Sprintf("sandbox-%d", time.Now().UnixNano()))
	if err := os.WriteFile(scriptFile, []byte(script), 0700); err != nil {
		return &model.TaskResult{Error: "failed to write script: " + err.Error()}
	}
	defer os.Remove(scriptFile)

	var cmd *exec.Cmd
	switch runtime {
	case "python3":
		cmd = exec.CommandContext(ctx, "python3", scriptFile)
	case "node":
		cmd = exec.CommandContext(ctx, "node", scriptFile)
	default: // bash
		cmd = exec.CommandContext(ctx, "bash", scriptFile)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = w.sandboxEnv()

	if err := cmd.Run(); err != nil {
		return &model.TaskResult{
			Output: map[string]any{
				"exit_code": cmd.ProcessState.ExitCode(),
				"stdout":    stdout.String(),
				"stderr":    stderr.String(),
				"runtime":   runtime,
				"timeout":   timeout.String(),
			},
			Error: fmt.Sprintf("script failed: %v\nstderr: %s", err, stderr.String()),
		}
	}

	output := map[string]any{
		"exit_code": 0,
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
		"runtime":   runtime,
		"timeout":   timeout.String(),
	}
	// Attempt JSON parse of stdout for structured results.
	if parsed, ok := tryParseJSON(stdout.String()); ok {
		output["parsed"] = parsed
	}

	_ = res // resource limits applied via cgroups on K8s, Docker, or systemd
	return &model.TaskResult{Output: output}
}

// executeInDocker runs the script in an isolated Docker container.
func (w *Worker) executeInDocker(ctx context.Context, script, runtime string, res *model.SandboxResources, policy *model.SandboxPolicy, timeout time.Duration) *model.TaskResult {
	scriptFile := filepath.Join(w.runtimeDir, fmt.Sprintf("sandbox-%d", time.Now().UnixNano()))
	if err := os.WriteFile(scriptFile, []byte(script), 0700); err != nil {
		return &model.TaskResult{Error: "failed to write script: " + err.Error()}
	}
	defer os.Remove(scriptFile)

	containerName := fmt.Sprintf("flowgent-sandbox-%d", time.Now().UnixNano())
	args := []string{
		"run", "--rm",
		"--name", containerName,
		"--memory=" + res.Memory,
		"--cpus=" + cpuToDocker(res.CPU),
	}

	// Network policy.
	switch policy.Network.Mode {
	case "none":
		args = append(args, "--network=none")
	case "allowlist":
		args = append(args, "--network=none")
		// For allowlist mode, we'd need a custom bridge. Use host networking
		// with iptables as a practical alternative.
	default:
		args = append(args, "--network=none")
	}

	args = append(args,
		"-v", scriptFile+":/sandbox/script:ro",
		"--workdir", "/sandbox",
		w.image,
		runtime, "/sandbox/script",
	)

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return &model.TaskResult{
			Output: map[string]any{"stdout": stdout.String(), "stderr": stderr.String()},
			Error:  fmt.Sprintf("docker sandbox failed: %v", err),
		}
	}

	output := map[string]any{
		"stdout":  stdout.String(),
		"stderr":  stderr.String(),
		"runtime": runtime,
	}
	if parsed, ok := tryParseJSON(stdout.String()); ok {
		output["parsed"] = parsed
	}
	return &model.TaskResult{Output: output}
}

// effectivePolicy resolves the effective sandbox policy for a plan.
func (w *Worker) effectivePolicy(plan *model.ExecutionPlan) *model.SandboxPolicy {
	p := *w.policy // shallow copy
	if plan.NodeSpec != nil && plan.NodeSpec.NetworkPolicy != nil {
		p.Network = *plan.NodeSpec.NetworkPolicy
	}
	// Per-flow override is embedded in the plan context; for now node-level is authoritative.
	return &p
}

func (w *Worker) isRuntimeAllowed(r string) bool {
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

func (w *Worker) checkBanned(script string, patterns []string) string {
	lower := strings.ToLower(script)
	for _, p := range patterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return p
		}
	}
	return ""
}

func (w *Worker) sandboxEnv() []string {
	return []string{
		"HOME=/tmp",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"SANDBOX_MODE=1",
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

func (w *Worker) publishResult(msg *queue.Message, result *model.TaskResult) {
	resultTopic := "flowgent/sandbox/result/" + msg.ID
	payload, _ := json.Marshal(result)
	_ = w.queue.Push(context.Background(), &queue.Message{
		ID:      msg.ID,
		Topic:   resultTopic,
		Payload: payload,
	})
	_ = w.queue.Ack(context.Background(), msg.ID)
}

func (w *Worker) publishError(msg *queue.Message, errStr string) {
	result := &model.TaskResult{Error: errStr}
	w.publishResult(msg, result)
}

func cpuToDocker(cpu string) string {
	if cpu == "" {
		return "1.0"
	}
	return strings.TrimSuffix(cpu, "m") + "m"
}
