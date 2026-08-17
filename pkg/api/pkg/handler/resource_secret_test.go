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

	inline := &entities.LlmProviderInfo{ApiKey: "plaintext-secret"}
	if err := normalizeLlmSecret(inline); err == nil {
		t.Fatal("inline API key was accepted")
	}
}

func TestMcpSensitiveHeadersUseReferencesAndAreRedacted(t *testing.T) {
	mcp := &entities.McpInfo{
		Headers: map[string]string{
			"Authorization": "Bearer ${GITHUB_TOKEN}",
			"X-Namespace":   "default",
		},
		Env: map[string]string{"GITHUB_TOKEN": "${GITHUB_TOKEN}"},
	}
	if err := normalizeMcpSecrets(mcp, nil); err != nil {
		t.Fatalf("normalize MCP references: %v", err)
	}
	public := publicMcp(mcp)
	if public.Headers["Authorization"] != redactedSecret {
		t.Fatalf("authorization was not redacted: %+v", public.Headers)
	}
	if public.HeaderRefs["Authorization"] != "Bearer ${GITHUB_TOKEN}" {
		t.Fatalf("reference projection missing: %+v", public.HeaderRefs)
	}
	if public.Env["GITHUB_TOKEN"] != redactedSecret {
		t.Fatalf("environment value was not redacted: %+v", public.Env)
	}

	inline := &entities.McpInfo{Headers: map[string]string{"Authorization": "Bearer plaintext"}}
	if err := normalizeMcpSecrets(inline, nil); err == nil {
		t.Fatal("inline authorization header was accepted")
	}
}
