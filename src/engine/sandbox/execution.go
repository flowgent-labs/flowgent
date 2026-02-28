package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/flowgent-labs/flowgent/src/model"
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

	res := trigger.Resources
	if res == nil {
		res = w.policy.DefaultResources
	}

	policy := w.policy
	if trigger.NetworkPolicy != nil {
		policy = &model.SandboxPolicy{
			Network:          *trigger.NetworkPolicy,
			AllowedRuntimes:  w.policy.AllowedRuntimes,
			BannedCommands:   w.policy.BannedCommands,
			DefaultTimeout:   w.policy.DefaultTimeout,
			MaxTimeout:       w.policy.MaxTimeout,
			DefaultResources: w.policy.DefaultResources,
			MaxResources:     w.policy.MaxResources,
		}
	}

	if w.image != "" {
		return w.executeInDocker(ctx, trigger.ScriptPath, script, runtime, res, policy, trigger.Workspace, d)
	}
	return w.executeInProcess(ctx, trigger.ScriptPath, script, runtime, res, trigger.Workspace, d)
}

func (w *SandboxRunner) executeInProcess(ctx context.Context, scriptPath, script, runtime string, res *model.SandboxResources, workspace string, timeout time.Duration) *model.TaskResult {
	scriptFile := filepath.Join(scriptPath, "script."+extForRuntime(runtime))
	if err := os.WriteFile(scriptFile, []byte(script), 0700); err != nil {
		return &model.TaskResult{Error: "write script: " + err.Error()}
	}

	var cmd *exec.Cmd
	switch runtime {
	case "python3":
		cmd = exec.CommandContext(ctx, "python3", scriptFile)
	case "node":
		cmd = exec.CommandContext(ctx, "node", scriptFile)
	default:
		cmd = exec.CommandContext(ctx, "bash", scriptFile)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = []string{"HOME=/tmp", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "SANDBOX_MODE=1"}
	_ = workspace // reserved for future workspace-aware execution

	if err := cmd.Run(); err != nil {
		result := &model.TaskResult{
			Output: map[string]any{
				"exit_code": cmd.ProcessState.ExitCode(),
				"stdout":    stdout.String(), "stderr": stderr.String(),
				"runtime":   runtime, "timeout": timeout.String(),
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

func (w *SandboxRunner) executeInDocker(ctx context.Context, scriptPath, script, runtime string, res *model.SandboxResources, policy *model.SandboxPolicy, workspace string, timeout time.Duration) *model.TaskResult {
	scriptFile := filepath.Join(scriptPath, "script."+extForRuntime(runtime))
	if err := os.WriteFile(scriptFile, []byte(script), 0700); err != nil {
		return &model.TaskResult{Error: "write script: " + err.Error()}
	}

	containerName := fmt.Sprintf("flowgent-sandbox-%d", time.Now().UnixNano())
	args := []string{"run", "--rm", "--name", containerName, "--memory=" + res.Memory, "--cpus=" + cpuToDocker(res.CPU)}
	switch policy.Network.Mode {
	case "none":
		args = append(args, "--network=none")
	default:
		args = append(args, "--network=none")
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
