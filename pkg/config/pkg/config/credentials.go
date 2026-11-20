package config

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// LoadCredentials reads credentials files in priority order (graph > namespace).
// Returns a merged map suitable for setting as environment variables.
//
// Paths resolved (CSI-mounted secrets from GCP Secret Manager or similar):
//
//	{basePath}/{namespace}/secret/.credentials          (namespace-level, lower priority)
//	{basePath}/{namespace}/{graph}/secret/.credentials   (graph-level, highest priority)
func LoadCredentials(basePath, namespace, graph string) map[string]string {
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

	// 2. AgentGraph-level credentials (highest priority — overrides namespace)
	if graph != "" {
		graphPath := filepath.Join(basePath, namespace, graph, "secret", ".credentials")
		if m := readEnvFile(graphPath); len(m) > 0 {
			for k, v := range m {
				result[k] = v
			}
			found = true
			slog.Debug("credentials loaded graph-level", "path", graphPath, "vars", len(m))
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
func FormatCredentialPaths(basePath, namespace, graph string) string {
	return fmt.Sprintf(
		"namespace: %s | graph: %s",
		filepath.Join(basePath, namespace, "secret", ".credentials"),
		filepath.Join(basePath, namespace, graph, "secret", ".credentials"),
	)
}
