package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/sandbox/src/seccomp"
)

// execute validates policy and dispatches to process or Docker executor.
func (w *SandboxRunner) execute(ctx context.Context, trigger *sandboxTrigger, script string) *model.TaskResult {
	runtime := trigger.Runtime
	if runtime == "" {
		runtime = "bash"
	}
	if !w.isRuntimeAllowed(runtime) {
		return &model.TaskResult{Error: fmt.Sprintf("runtime %q not allowed", runtime)}
	}
	if banned := w.checkBanned(script); banned != "" {
		return &model.TaskResult{Error: fmt.Sprintf("banned pattern: %q", banned)}
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

	// Resolve effective network policy for this execution.
	netPolicy := &w.policy.Network
	if trigger.NetworkPolicy != nil {
		netPolicy = trigger.NetworkPolicy
	}

	if w.image != "" {
		return w.executeInDocker(ctx, trigger.ScriptPath, script, runtime, netPolicy, trigger.Workspace, d)
	}
	return w.executeInProcess(ctx, trigger.ScriptPath, script, runtime, netPolicy, trigger.Workspace, d)
}

func (w *SandboxRunner) executeInProcess(ctx context.Context, scriptPath, script, runtime string, netPolicy *model.NetworkPolicy, workspace string, timeout time.Duration) *model.TaskResult {
	scriptFile := filepath.Join(scriptPath, "script."+extForRuntime(runtime))
	if err := os.WriteFile(scriptFile, []byte(script), 0700); err != nil {
		return &model.TaskResult{Error: "write script: " + err.Error()}
	}

	// Build seccomp filter from the effective network policy.
	filter, ferr := seccomp.BuildFilter(netPolicy)
	if ferr != nil {
		return &model.TaskResult{Error: "seccomp build: " + ferr.Error()}
	}

	// Get a filtered command (via re-exec if seccomp is needed).
	cmd, notifCh, cerr := filter.ScriptCmd(scriptPath, runtime, workspace)
	if cerr != nil {
		return &model.TaskResult{Error: "seccomp cmd: " + cerr.Error()}
	}

	// Apply context timeout. exec.CommandContext sets cmd.Cancel internally.
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

	if err := cmd.Start(); err != nil {
		return &model.TaskResult{Error: "start script: " + err.Error()}
	}

	// If the filter has a notifier, start it in a goroutine.
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
		result := &model.TaskResult{
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
	result := &model.TaskResult{Output: output}
	w.writeResultFile(scriptPath, result)
	os.WriteFile(filepath.Join(scriptPath, "status"), []byte("SUCCESS"), 0644)
	return result
}

func (w *SandboxRunner) executeInDocker(ctx context.Context, scriptPath, script, runtime string, netPolicy *model.NetworkPolicy, workspace string, timeout time.Duration) *model.TaskResult {
	scriptFile := filepath.Join(scriptPath, "script."+extForRuntime(runtime))
	if err := os.WriteFile(scriptFile, []byte(script), 0700); err != nil {
		return &model.TaskResult{Error: "write script: " + err.Error()}
	}

	containerName := fmt.Sprintf("flowgent-sandbox-%d", time.Now().UnixNano())
	memLimit := "256Mi"
	cpuLimit := "1.0"
	args := []string{"run", "--rm", "--name", containerName, "--memory=" + memLimit, "--cpus=" + cpuLimit}

	switch netPolicy.Mode {
	case "none":
		args = append(args, "--network=none")
	default:
		args = append(args, "--network=none") // TODO: allowlist/denylist via seccomp in container
	}

	args = append(args, "-v", scriptPath+":/sandbox:rw", "--workdir", "/sandbox")
	if workspace != "" {
		args = append(args, "-v", workspace+":/workspace:rw")
	}
	args = append(args, w.image, runtime, "/sandbox/script."+extForRuntime(runtime))

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		result := &model.TaskResult{
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
	result := &model.TaskResult{Output: output}
	w.writeResultFile(scriptPath, result)
	os.WriteFile(filepath.Join(scriptPath, "status"), []byte("SUCCESS"), 0644)
	return result
}

func (w *SandboxRunner) writeResultFile(scriptPath string, result *model.TaskResult) {
	data, _ := json.Marshal(result)
	os.WriteFile(filepath.Join(scriptPath, "result.json"), data, 0644)
}
