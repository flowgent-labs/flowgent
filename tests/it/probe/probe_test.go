package probe

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestMain runs before any test in this tiny package (which compiles instantly
// because it imports nothing from flowgent). It probes the network and core
// tools (docker/kubectl/helm), then the parent `go test ./...` continues to
// the heavy subpackages (apiserver, engine, etc.).
func TestMain(m *testing.M) {
	ts := time.Now().Format("15:04:05")

	// GFW detection runs first so devs see it while linking heavy packages.
	gfwVal, gfwDetail := gfwMode()

	ttyPrintf("\n")
	ttyPrintf("  ╔══════════════════════════════════════════════════════════════╗\n")
	ttyPrintf("  ║  IT Environment Probe  %s                              ║\n", ts)
	ttyPrintf("  ╚══════════════════════════════════════════════════════════════╝\n")
	ttyPrintf("  ┌─ Network ────────────────────────────────────────────────────┐\n")
	ttyPrintf("  │ %s %-12s %s\n", mark(gfwVal == "true"), "IN_CN_GFW", gfwDetail)

	internetTarget := "github.com:443"
	if gfwVal == "true" {
		internetTarget = "goproxy.cn:443"
	}
	netOK, _ := probeTCP(internetTarget)
	ttyPrintf("  │ %s %-12s %s reachable\n", mark(netOK), "internet", internetTarget)
	ttyPrintf("  ├── Tools ─────────────────────────────────────────────────────┤\n")

	r := probeCmd("/bin/docker", "ps")
	ttyPrintf("  │ %s %-12s %s\n", mark(r.ok), "docker", r.detail)

	r = probeCmd("kubectl", "get", "ns")
	ttyPrintf("  │ %s %-12s %s\n", mark(r.ok), "kubectl", r.detail)
	if !r.ok {
		if _, err := os.Stat("/etc/rancher/k3s/k3s.yaml"); err == nil {
			home, _ := os.UserHomeDir()
			ttyPrintf("  │   ⚠  /etc/rancher/k3s/k3s.yaml found but not accessible.\n")
			ttyPrintf("  │      Fix:  mkdir -p %s/.kube && sudo cp /etc/rancher/k3s/k3s.yaml %s/.kube/config\n", home, home)
		}
	}

	r = probeCmd("helm", "version")
	ttyPrintf("  │ %s %-12s %s\n", mark(r.ok), "helm", r.detail)

	ttyPrintf("  └──────────────────────────────────────────────────────────────┘\n\n")

	os.Exit(m.Run())
}

// ─── GFW detection ──────────────────────────────────────────────────────────

func gfwMode() (string, string) {
	if v, ok := os.LookupEnv("IN_CN_GFW"); ok && v != "" {
		return v, fmt.Sprintf("IN_CN_GFW=%s (explicit)", v)
	}
	country := detectCountry()
	if country == "CN" {
		setCNEnv()
		return "true", "auto-detected CN → GOPROXY=goproxy.cn"
	}
	if country != "??" {
		// Country identified and is NOT CN — no GFW.
		os.Setenv("IN_CN_GFW", "false")
		return "false", "auto-detected non-CN (" + country + ")"
	}
	conn, err := net.DialTimeout("tcp", "goproxy.cn:443", 3*time.Second)
	if err == nil {
		conn.Close()
		setCNEnv()
		return "true", "auto-detected CN (goproxy.cn reachable, ipinfo.io blocked)"
	}
	os.Setenv("IN_CN_GFW", "false")
	return "false", "auto-detected non-CN (neither reachable)"
}

func detectCountry() string {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://ipinfo.io/country")
	if err != nil {
		return "??"
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 128))
	return strings.TrimSpace(string(body))
}

func setCNEnv() {
	os.Setenv("IN_CN_GFW", "true")
	if v, ok := os.LookupEnv("GOPROXY"); !ok || v == "" {
		os.Setenv("GOPROXY", "https://goproxy.cn,direct")
	}
	if v, ok := os.LookupEnv("GONOSUMDB"); !ok || v == "" {
		os.Setenv("GONOSUMDB", "*")
	}
	if v, ok := os.LookupEnv("GONOSUMCHECK"); !ok || v == "" {
		os.Setenv("GONOSUMCHECK", "*")
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────

type probeResult struct {
	ok     bool
	detail string
}

func probeCmd(cmd string, args ...string) probeResult {
	_, err := exec.LookPath(cmd)
	if err != nil {
		return probeResult{false, "not found in PATH"}
	}
	out, err := exec.Command(cmd, args...).CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		if s == "" {
			return probeResult{false, "failed"}
		}
		line := strings.SplitN(s, "\n", 2)[0]
		if len(line) > 55 {
			line = line[:52] + "..."
		}
		return probeResult{false, line}
	}
	line := strings.SplitN(s, "\n", 2)[0]
	if len(line) > 55 {
		line = line[:52] + "..."
	}
	return probeResult{true, line}
}

func probeTCP(addr string) (bool, string) {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return false, "not reachable"
	}
	conn.Close()
	return true, "ok"
}

func mark(ok bool) string {
	if ok { return "✓" }
	return "✗"
}

// ttyPrintf — same trick as mcpfather.
func ttyPrintf(format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		tty.WriteString(line)
		tty.Close()
	} else {
		os.Stderr.WriteString(line)
	}
}
