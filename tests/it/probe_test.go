package it

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestMain is the integration-test entry point. It probes the local environment
// before any tests run so developers see immediately what is available and what
// will be skipped or fail.
func TestMain(m *testing.M) {
	probeEnvironment()
	os.Exit(m.Run())
}

// ttyPrintf writes directly to /dev/tty to bypass go test's stderr capture pipe.
// Falls back to os.Stderr when /dev/tty is unavailable (CI environments).
func ttyPrintf(format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		tty.WriteString(line)
		tty.Close()
	} else {
		os.Stderr.WriteString(line)
	}
}

// probeEnvironment checks every external dependency the IT suite may need and
// prints a summary table directly to the terminal via /dev/tty.
func probeEnvironment() {
	ts := time.Now().Format("15:04:05")
	ttyPrintf("\n")
	ttyPrintf("  ╔══════════════════════════════════════════════════════════════╗\n")
	ttyPrintf("  ║  IT Environment Probe  %s                              ║\n", ts)
	ttyPrintf("  ╚══════════════════════════════════════════════════════════════╝\n")
	ttyPrintf("  ┌─ Connectivity ───────────────────────────────────────────────┐\n")

	// docker ps — can we talk to the daemon?
	dockerOK, dockerDetail := probeLive("docker ps", []string{"/bin/docker", "ps"})
	ttyPrintf("  │ %s %-12s %s\n", mark(dockerOK), "docker", dockerDetail)
	if !dockerOK {
		ttyPrintf("  │   ⚠  docker ps failed — tests that need PostgreSQL/OIDC will skip.\n")
	}

	// kubectl get ns — can we reach a k8s cluster via ~/.kube/config?
	kubectlOK, kubectlDetail := probeLive("kubectl get ns", []string{"kubectl", "get", "ns"})
	ttyPrintf("  │ %s %-12s %s\n", mark(kubectlOK), "kubectl", kubectlDetail)
	if !kubectlOK {
		if _, err := os.Stat("/etc/rancher/k3s/k3s.yaml"); err == nil {
			home, _ := os.UserHomeDir()
			target := home + "/.kube/config"
			ttyPrintf("  │   ⚠  /etc/rancher/k3s/k3s.yaml found but not accessible.\n")
			ttyPrintf("  │      Fix:  mkdir -p %s/.kube && sudo cp /etc/rancher/k3s/k3s.yaml %s && chmod 600 %s\n", home, target, target)
		}
	}

	// helm version — is helm present?
	helmOK, helmDetail := probeLive("helm version", []string{"helm", "version"})
	ttyPrintf("  │ %s %-12s %s\n", mark(helmOK), "helm", helmDetail)

	ttyPrintf("  └──────────────────────────────────────────────────────────────┘\n\n")
}

// probeLive runs a command and returns whether it succeeded plus a one-line
// detail string (first line of stdout, or first line of stderr on failure).
func probeLive(label string, cmdArgs []string) (bool, string) {
	_, err := exec.LookPath(cmdArgs[0])
	if err != nil {
		return false, "not found in PATH"
	}
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	out, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(out))
	if err != nil {
		if outStr == "" {
			return false, "failed"
		}
		line := strings.SplitN(outStr, "\n", 2)[0]
		if len(line) > 55 {
			line = line[:52] + "..."
		}
		return false, line
	}
	// Show first line of output
	line := strings.SplitN(outStr, "\n", 2)[0]
	if len(line) > 55 {
		line = line[:52] + "..."
	}
	return true, line
}

func mark(ok bool) string {
	if ok { return "✓" }
	return "✗"
}
