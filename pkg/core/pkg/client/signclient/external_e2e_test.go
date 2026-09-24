//go:build x402

package signclient

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWalletdLocalContractE2E(t *testing.T) {
	walletd := os.Getenv("FLOWGENT_WALLETD_BIN")
	if walletd == "" {
		t.Skip("FLOWGENT_WALLETD_BIN is not set; external Wallet contract E2E is opt-in")
	}
	if info, err := os.Stat(walletd); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		t.Fatalf("FLOWGENT_WALLETD_BIN is not executable: %q", walletd)
	}

	root := t.TempDir()
	masterKey := filepath.Join(root, "master.key")
	store := filepath.Join(root, "store")
	socket := filepath.Join(root, "run", "wallet.sock")
	configPath := filepath.Join(root, "wallet.toml")
	runWalletd(t, walletd, "master-key", "generate", "--output", masterKey)
	config := fmt.Sprintf(`
[store]
directory = %q
master_key_file = %q

[security]
max_request_bytes = 16384
max_request_ttl_seconds = 30
clock_skew_seconds = 5
dedup_ttl_seconds = 300
max_dedup_entries = 10000

[[security.client_policies]]
client_id_prefix = "flowgent-"
wallet_ids = ["payer"]
purposes = ["x402.payment"]

[transports.local]
enabled = true
socket_path = %q

[transports.mqtt]
enabled = false
`, store, masterKey, socket)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write Wallet E2E config: %v", err)
	}

	generated := runWalletd(t, walletd, "--config", configPath, "key", "generate", "payer")
	fields := strings.Split(strings.TrimSpace(generated), "\t")
	if len(fields) != 3 || fields[0] != "payer" || fields[1] != "secp256k1" {
		t.Fatalf("unexpected walletd key output: %q", generated)
	}
	address := fields[2]

	var stderr bytes.Buffer
	daemon := exec.Command(walletd, "--config", configPath, "serve")
	daemon.Stderr = &stderr
	if err := daemon.Start(); err != nil {
		t.Fatalf("start walletd: %v", err)
	}
	t.Cleanup(func() {
		_ = daemon.Process.Kill()
		_ = daemon.Wait()
	})
	waitForSocket(t, socket, &stderr)

	transport, err := NewLocalTransport(socket, 3*time.Second)
	if err != nil {
		t.Fatalf("create Flowgent local Wallet transport: %v", err)
	}
	client, err := NewDigestClient(
		transport,
		"flowgent-go-e2e",
		"payer",
		address,
		3*time.Second,
	)
	if err != nil {
		t.Fatalf("create Flowgent Wallet digest client: %v", err)
	}
	digest := [32]byte{0x40, 0x2}
	signature, err := client.SignDigest(context.Background(), "x402.payment", digest[:])
	if err != nil {
		t.Fatalf("Flowgent-to-walletd signing failed: %v", err)
	}
	if len(signature) != 65 {
		t.Fatalf("unexpected signature length: %d", len(signature))
	}
}

func runWalletd(t *testing.T, walletd string, arguments ...string) string {
	t.Helper()
	output, err := exec.Command(walletd, arguments...).CombinedOutput()
	if err != nil {
		t.Fatalf("walletd %s failed: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func waitForSocket(t *testing.T, socket string, stderr *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socket); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("walletd did not create %s: %s", socket, stderr.String())
}
