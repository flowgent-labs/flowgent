package config

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// LoadCredentials reads credentials files in priority order (flow > tenant).
// Returns a merged map suitable for setting as environment variables.
// Logs a warning if no credentials are found at any level.
//
// Paths resolved:
//
//	{basePath}/{tenant}/credentials          (tenant-level, lower priority)
//	{basePath}/{tenant}/{flow}/credentials    (flow-level, highest priority)
func LoadCredentials(basePath, tenant, flow string, flowCreds map[string]string) map[string]string {
	if basePath == "" {
		basePath = "/var/secret/flowgent"
	}

	result := make(map[string]string)
	found := false

	// 1. Tenant-level credentials (lower priority)
	tenantPath := filepath.Join(basePath, tenant, "credentials")
	if m := readEnvFile(tenantPath); len(m) > 0 {
		for k, v := range m {
			result[k] = v
		}
		found = true
		log.Printf("[credentials] loaded tenant-level: %s (%d vars)", tenantPath, len(m))
	}

	// 2. Flow-level credentials (highest priority — overrides tenant)
	if flow != "" {
		flowPath := filepath.Join(basePath, tenant, flow, "credentials")
		if m := readEnvFile(flowPath); len(m) > 0 {
			for k, v := range m {
				result[k] = v
			}
			found = true
			log.Printf("[credentials] loaded flow-level: %s (%d vars)", flowPath, len(m))
		}

		// 3. Inline flow credentials from AgentFlowSpec (highest of all)
		for k, v := range flowCreds {
			result[k] = v
			found = true
		}
	}

	if !found {
		log.Printf("[credentials] WARNING: no credentials found at %s/{tenant}/credentials or %s/{tenant}/{flow}/credentials — components may fail external calls", basePath, basePath)
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
		return CredentialPathsConfig{BasePath: "/var/secret/flowgent"}
	}
	c := cfg.CredentialPaths
	if c.BasePath == "" {
		c.BasePath = "/var/secret/flowgent"
	}
	return c
}

// FormatCredentialPaths formats the expected credential file paths for display.
func FormatCredentialPaths(basePath, tenant, flow string) string {
	return fmt.Sprintf(
		"tenant: %s | flow: %s",
		filepath.Join(basePath, tenant, "credentials"),
		filepath.Join(basePath, tenant, flow, "credentials"),
	)
}
