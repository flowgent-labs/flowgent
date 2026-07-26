package config

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// LoadCredentials reads credentials files in priority order (flow > namespace).
// Returns a merged map suitable for setting as environment variables.
//
// Paths resolved (CSI-mounted secrets from GCP Secret Manager or similar):
//
//	{basePath}/{namespace}/secret/.credentials          (namespace-level, lower priority)
//	{basePath}/{namespace}/{flow}/secret/.credentials   (flow-level, highest priority)
func LoadCredentials(basePath, namespace, flow string, flowCreds map[string]string) map[string]string {
	if basePath == "" {
		basePath = "/var/flowgent"
	}

	result := make(map[string]string)
	found := false

	// 1. Namespace-level credentials (lower priority)
	nsPath := filepath.Join(basePath, namespace, "secret", ".credentials")
	if m := readEnvFile(nsPath); len(m) > 0 {
		for k, v := range m {
			result[k] = v
		}
		found = true
		slog.Debug("credentials loaded namespace-level", "path", nsPath, "vars", len(m))
	}

	// 2. Flow-level credentials (highest priority — overrides namespace)
	if flow != "" {
		flowPath := filepath.Join(basePath, namespace, flow, "secret", ".credentials")
		if m := readEnvFile(flowPath); len(m) > 0 {
			for k, v := range m {
				result[k] = v
			}
			found = true
			slog.Debug("credentials loaded flow-level", "path", flowPath, "vars", len(m))
		}

		// 3. Inline flow credentials from FlowInfo (highest of all)
		for k, v := range flowCreds {
			result[k] = v
			found = true
		}
	}

	if !found {
		slog.Warn("no credentials found, components may fail external calls", "basePath", basePath)
	}

	return result
}

// readEnvFile reads a KEY=VALUE file (one per line, # comments and blank lines skipped).
func readEnvFile(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	m := make(map[string]string)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k != "" {
			m[k] = v
		}
	}
	_ = sc.Err()
	return m
}

// CredentialPathsOrDefault returns the credential paths config, filling defaults.
func CredentialPathsOrDefault(cfg *FlowgentConfig) CredentialPathsConfig {
	if cfg == nil {
		return CredentialPathsConfig{BasePath: "/var/flowgent"}
	}
	c := cfg.Runtime.CredentialPaths
	if c.BasePath == "" {
		c.BasePath = "/var/flowgent"
	}
	return c
}

// FormatCredentialPaths formats the expected credential file paths for display.
func FormatCredentialPaths(basePath, namespace, flow string) string {
	return fmt.Sprintf(
		"namespace: %s | flow: %s",
		filepath.Join(basePath, namespace, "secret", ".credentials"),
		filepath.Join(basePath, namespace, flow, "secret", ".credentials"),
	)
}
