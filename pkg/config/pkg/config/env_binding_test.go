package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestKeyToEnvSegment locks the config-key → env-segment conversion. Canonical keys
// are snake_case; kebab-case and camelCase are also accepted (relaxed binding).
func TestKeyToEnvSegment(t *testing.T) {
	cases := map[string]string{
		"jm_image":               "JM_IMAGE",
		"agent_flow_id":          "AGENT_FLOW_ID",
		"api_server_url":         "API_SERVER_URL",
		"max_single_payment_usd": "MAX_SINGLE_PAYMENT_USD",
		"dsn":                    "DSN",
		"client_id":              "CLIENT_ID",
		"use_ssl":                "USE_SSL",
		"controller_label":       "CONTROLLER_LABEL",
		// non-canonical spellings still map to the same env segment
		"jm-image":    "JM_IMAGE",
		"agentFlowId": "AGENT_FLOW_ID",
		// alphanumeric abbreviations must NOT be split around digits
		// (a digit boundary is not a word boundary): "a2a" → "A2A", not "A_2_A".
		"a2a":  "A2A",
		"x402": "X402",
	}
	for in, want := range cases {
		if got := keyToEnvSegment(in); got != want {
			t.Errorf("keyToEnvSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRelaxedBinding proves Spring Boot-style relaxed binding: the canonical
// snake_case keys AND non-canonical kebab-case / camelCase spellings all bind to
// the same struct fields.
func TestRelaxedBinding(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "flowgent.yaml")
	// Deliberately mix all three styles: snake (canonical), kebab, camel.
	yaml := "" +
		"service_name: snake\n" + // snake_case (canonical)
		"server:\n" +
		"  context-path: /kebab\n" + // kebab-case
		"runtime:\n" +
		"  apiServerUrl: http://camel:9999\n" + // camelCase
		"  jm_image: snake-image\n" // snake_case
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ServiceName != "snake" {
		t.Errorf("service_name (snake) not bound: %q", cfg.ServiceName)
	}
	if cfg.Server.ContextPath != "/kebab" {
		t.Errorf("context-path (kebab) not bound: %q", cfg.Server.ContextPath)
	}
	if cfg.Runtime.APIServerURL != "http://camel:9999" {
		t.Errorf("apiServerUrl (camel) not bound: %q", cfg.Runtime.APIServerURL)
	}
	if cfg.Runtime.JMImage != "snake-image" {
		t.Errorf("jm_image (snake) not bound: %q", cfg.Runtime.JMImage)
	}
}

// TestEnvOverrideBinding guards the FLOWGENT__ env-override relaxed binding.
// It specifically covers multi-word fields, which a naive __→. split would fail
// to bind, using the canonical snake_case YAML keys:
//   - single-word nested   (storage.postgres.dsn)
//   - multi-word snake key  (messager.mqtt.client_id, runtime.agent_flow_id, ...)
func TestEnvOverrideBinding(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "flowgent.yaml")
	// Canonical snake_case keys (matching the struct tags), so FLOWGENT__ env
	// overrides target the exact same viper key and take precedence.
	yaml := "" +
		"service_name: probe\n" +
		"runtime:\n" +
		"  system_namespace: from-yaml\n" +
		"  k8s_namespace: from-yaml\n" +
		"  agent_flow_id: from-yaml\n" +
		"  jm_image: from-yaml\n" +
		"  controller_label: from-yaml\n" +
		"storage:\n" +
		"  postgres:\n" +
		"    dsn: from-yaml\n" +
		"  artifacts:\n" +
		"    provider: default\n" +
		"    s3:\n" +
		"      bucket: from-yaml\n" +
		"messager:\n" +
		"  mqtt:\n" +
		"    client_id: from-yaml\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("FLOWGENT__STORAGE__POSTGRES__DSN", "from-env")
	t.Setenv("FLOWGENT__STORAGE__ARTIFACTS__PROVIDER", "from-env")
	t.Setenv("FLOWGENT__STORAGE__ARTIFACTS__S3__BUCKET", "from-env")
	t.Setenv("FLOWGENT__RUNTIME__SYSTEM_NAMESPACE", "from-env")
	t.Setenv("FLOWGENT__RUNTIME__K8S_NAMESPACE", "from-env")
	t.Setenv("FLOWGENT__MESSAGER__MQTT__CLIENT_ID", "from-env")
	t.Setenv("FLOWGENT__RUNTIME__AGENT_FLOW_ID", "from-env")
	t.Setenv("FLOWGENT__RUNTIME__JM_IMAGE", "from-env")
	t.Setenv("FLOWGENT__RUNTIME__CONTROLLER_LABEL", "from-env")

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	checks := map[string]string{
		"storage.postgres.dsn":        cfg.Storage.Postgres.Dsn,
		"storage.artifacts.provider":  cfg.Storage.Artifacts.Provider,
		"storage.artifacts.s3.bucket": cfg.Storage.Artifacts.S3.Bucket,
		"runtime.system_namespace":    cfg.Runtime.SystemNamespace,
		"runtime.k8s_namespace":       cfg.Runtime.K8sNamespace,
		"messager.mqtt.client_id":     cfg.Messager.MQTT.ClientID,
		"runtime.agent_flow_id":       cfg.Runtime.AgentFlowID,
		"runtime.jm_image":            cfg.Runtime.JMImage,
		"runtime.controller_label":    cfg.Runtime.ControllerLabel,
	}
	for key, got := range checks {
		if got != "from-env" {
			t.Errorf("env override lost for %s: got %q, want from-env", key, got)
		}
	}
}

// TestEnvOverrideAlphanumericSegments guards against the digit-boundary regression
// where "a2a" / "x402" config keys were mis-split into "A_2_A" / "X_402" env
// segments, breaking FLOWGENT__A2A__* and FLOWGENT__WALLET__X402__* overrides.
func TestEnvOverrideAlphanumericSegments(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "flowgent.yaml")
	yaml := "" +
		"a2a:\n" +
		"  enabled: false\n" +
		"  host: from-yaml\n" +
		"wallet:\n" +
		"  enabled: false\n" +
		"  x402:\n" +
		"    timeout: from-yaml\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("FLOWGENT__A2A__HOST", "from-env")
	t.Setenv("FLOWGENT__WALLET__X402__TIMEOUT", "from-env")

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.A2A.Host != "from-env" {
		t.Errorf("FLOWGENT__A2A__HOST override lost: got %q", cfg.A2A.Host)
	}
	if cfg.Wallet == nil || cfg.Wallet.X402.Timeout != "from-env" {
		t.Errorf("FLOWGENT__WALLET__X402__TIMEOUT override lost: got %+v", cfg.Wallet)
	}
}

// TestEnvOverridePointerStructField guards env overrides reaching fields nested
// under a pointer struct (e.g. *WalletConfig), which reflection must dereference.
func TestEnvOverridePointerStructField(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "flowgent.yaml")
	yaml := "wallet:\n  enabled: false\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLOWGENT__WALLET__ENABLED", "true")

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Wallet == nil || !cfg.Wallet.Enabled {
		t.Errorf("FLOWGENT__WALLET__ENABLED override lost: got %+v", cfg.Wallet)
	}
}
