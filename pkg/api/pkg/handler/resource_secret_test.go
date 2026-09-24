package handler

import (
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func TestLlmProviderUsesWriteOnlyEnvironmentReference(t *testing.T) {
	provider := &entities.LlmProviderInfo{ApiKeyEnv: "DEEPSEEK_API_KEY"}
	if err := normalizeLlmSecret(provider); err != nil {
		t.Fatalf("normalize reference: %v", err)
	}
	if provider.ApiKey != "env://DEEPSEEK_API_KEY" {
		t.Fatalf("persisted API key = %q", provider.ApiKey)
	}
	public := publicLlmProvider(provider)
	if public.ApiKey != "" || public.ApiKeyEnv != "DEEPSEEK_API_KEY" || !public.KeyConfigured {
		t.Fatalf("unsafe public projection: %+v", public)
	}

	invalid := &entities.LlmProviderInfo{ApiKeyEnv: "not valid"}
	if err := normalizeLlmSecret(invalid); err == nil {
		t.Fatal("invalid environment name was accepted")
	}
}

func TestLlmProviderNormalizesPersistedTimeout(t *testing.T) {
	provider := &entities.LlmProviderInfo{Timeout: "90s"}
	provider.NormalizeAliases()
	if provider.TimeoutMs != 90_000 {
		t.Fatalf("timeout_ms = %d, want 90000", provider.TimeoutMs)
	}

	persisted := &entities.LlmProviderInfo{TimeoutMs: 45_000}
	persisted.NormalizeAliases()
	if persisted.Timeout != "45000ms" {
		t.Fatalf("timeout = %q, want 45000ms", persisted.Timeout)
	}
}

func TestMcpAPIUsesOnlySecretReferences(t *testing.T) {
	mcp := &entities.McpInfo{
		HeaderRefs: map[string]string{
			"Authorization": "Bearer ${GITHUB_TOKEN}",
			"X-Namespace":   "default",
		},
		EnvRefs: map[string]string{"GITHUB_TOKEN": "${GITHUB_TOKEN}"},
	}
	if err := normalizeMcpSecrets(mcp); err != nil {
		t.Fatalf("normalize MCP references: %v", err)
	}
	public := publicMcp(mcp)
	if public.Headers != nil || public.Env != nil {
		t.Fatalf("persisted fields leaked through API: %+v", public)
	}
	if public.HeaderRefs["Authorization"] != "Bearer ${GITHUB_TOKEN}" {
		t.Fatalf("reference projection missing: %+v", public.HeaderRefs)
	}
	if public.EnvRefs["GITHUB_TOKEN"] != "${GITHUB_TOKEN}" {
		t.Fatalf("environment reference projection missing: %+v", public.EnvRefs)
	}

	inline := &entities.McpInfo{HeaderRefs: map[string]string{"Authorization": "Bearer plaintext"}}
	if err := normalizeMcpSecrets(inline); err == nil {
		t.Fatal("inline authorization header was accepted")
	}
}
