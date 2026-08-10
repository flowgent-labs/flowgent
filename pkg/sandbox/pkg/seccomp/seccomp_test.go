//go:build linux

package seccomp

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	model "github.com/flowgent-labs/flowgent/model/pkg"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "sandbox-child" {
		if err := ChildMain(); err != nil {
			os.Stderr.WriteString(err.Error() + "\n")
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSplitPolicyEntryAcceptsHostAndURL(t *testing.T) {
	tests := []struct {
		entry string
		host  string
		port  string
	}{
		{entry: "github.com", host: "github.com", port: "443"},
		{entry: "github.com:443", host: "github.com", port: "443"},
		{entry: "http://sonarqube.local:9000", host: "sonarqube.local", port: "9000"},
		{entry: "https://api.github.com/repos", host: "api.github.com", port: "443"},
	}
	for _, tt := range tests {
		host, port, err := splitPolicyEntry(tt.entry)
		if err != nil {
			t.Fatalf("splitPolicyEntry(%q) unexpected error: %v", tt.entry, err)
		}
		if host != tt.host || port != tt.port {
			t.Fatalf("splitPolicyEntry(%q)=%s,%s want %s,%s", tt.entry, host, port, tt.host, tt.port)
		}
	}
}

func TestScriptCmdRunsFromScriptPath(t *testing.T) {
	scriptPath := t.TempDir()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(scriptPath, "input.json"), []byte(`{"ok":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptPath, "script.sh"), []byte("cat input.json"), 0700); err != nil {
		t.Fatal(err)
	}

	cmd, _, err := (&Filter{mode: "none"}).ScriptCmd(scriptPath, "bash", workspace)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("script failed: %v", err)
	}
	if string(out) != `{"ok":true}` {
		t.Fatalf("unexpected output %q", string(out))
	}
}

func TestScriptCmdAllowlistCreatesNotifier(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			io.Copy(io.Discard, conn)
			conn.Close()
		}
	}()

	scriptPath := t.TempDir()
	hostPort := ln.Addr().String()
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		t.Fatal(err)
	}
	script := "echo ok >/dev/tcp/" + host + "/" + port + "\n"
	if err := os.WriteFile(filepath.Join(scriptPath, "script.sh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}

	filter, err := BuildFilter(&model.NetworkPolicy{Mode: "allowlist", Allowed: []string{hostPort}})
	if err != nil {
		t.Fatal(err)
	}
	cmd, notifCh, err := filter.ScriptCmd(scriptPath, "bash", scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if notifCh == nil {
		t.Fatal("expected notifier channel for allowlist filter")
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for _, f := range cmd.ExtraFiles {
		if f != nil {
			_ = f.Close()
		}
	}

	select {
	case notifier := <-notifCh:
		if notifier == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("notifier handshake returned nil; stderr=%s", stderr.String())
		}
		go notifier.Start()
		defer notifier.Close()
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("timed out waiting for notifier")
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("script failed: %v stderr=%s", err, stderr.String())
	}
}
