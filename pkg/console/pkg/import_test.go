package console

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/secretbox"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/google/uuid"
)

func TestImportCanonicalGraphAndDistinctSkillsByMetadataName(t *testing.T) {
	fc, err := NewFlowgentConsole(&config.FlowgentConfig{
		Storage: config.StorageConfig{Type: "SQLITE", SQLite: config.SQLiteConfig{Dir: t.TempDir()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fc.Close() })
	for _, resource := range []struct{ kind, name string }{
		{"AgentGraph", "repair"}, {"Skill", "sub-fix"}, {"Skill", "nexus-retrieval"},
	} {
		raw := []byte("consoleVersion: core.flowgent.io/v1\nkind: " + resource.kind +
			"\nmetadata: {name: " + resource.name + ", namespace: team}\ndata:\n  nodes: [{id: end, kind: noop}]\n  edges: []\n")
		parsed, ok := ParseResourceImport(raw, "yaml")
		if !ok || !fc.ImportResource(parsed, resource.name+".yaml") {
			t.Fatalf("import failed: %s %s", resource.kind, resource.name)
		}
		fc.SetNamespace("team")
		saved, err := fc.GetAgentGraph(resource.name)
		if err != nil || saved == nil || saved.Name != resource.name {
			t.Fatalf("resource not retrievable by name: %+v, %v", saved, err)
		}
		if _, err := uuid.Parse(saved.ID); err != nil {
			t.Fatalf("stable ID is not a UUID: %q", saved.ID)
		}
	}
	skills, err := fc.ListSkills()
	if err != nil || len(skills) != 2 {
		t.Fatalf("distinct imported skills = %d, %v", len(skills), err)
	}
}

func TestParseResourceImportAcceptsOnlyCanonicalEnvelope(t *testing.T) {
	canonical := []byte(`
consoleVersion: core.flowgent.io/v1
kind: AgentGraph
metadata:
  name: example
  namespace: default
data:
  id: example
  runtime_mode: application
  nodes:
    - id: end
      kind: noop
  edges: []
`)
	resource, ok := ParseResourceImport(canonical, "yaml")
	if !ok || resource.ConsoleVersion != ConsoleAPIVersion || resource.Metadata.Name != "example" {
		t.Fatalf("canonical resource was rejected: ok=%v resource=%+v", ok, resource)
	}

	invalid := map[string][]byte{
		"apiVersion alias": []byte(`apiVersion: core.flowgent.io/v1
kind: AgentGraph
metadata: {name: example}
data: {id: example}`),
		"spec payload": []byte(`consoleVersion: core.flowgent.io/v1
kind: AgentGraph
metadata: {name: example}
spec: {id: example}`),
		"unknown version": []byte(`consoleVersion: core.flowgent.io/v2
kind: AgentGraph
metadata: {name: example}
data: {id: example}`),
	}
	for name, source := range invalid {
		t.Run(name, func(t *testing.T) {
			if resource, ok := ParseResourceImport(source, "yaml"); ok || resource != nil {
				t.Fatalf("non-canonical resource accepted: %+v", resource)
			}
		})
	}
}

func TestParseSpecRejectsUnknownFields(t *testing.T) {
	var graph entities.AgentGraph
	err := parseSpec([]byte(`{"id":"example","nodes":[],"edges":[],"spec":{}}`), &graph)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field rejection, got %v", err)
	}
}

func TestPreserveImportIdentity(t *testing.T) {
	created := time.Date(2026, 9, 24, 1, 2, 3, 0, time.UTC)
	target := &entities.BaseEntity{ID: "generated", CreatedAt: time.Now(), RowVersion: 1}
	existing := &entities.BaseEntity{
		ID: "stable", CreatedAt: created, CreatedBy: "principal:creator", RowVersion: 7,
	}

	preserveImportIdentity(target, existing)

	if target.ID != existing.ID || !target.CreatedAt.Equal(created) ||
		target.CreatedBy != existing.CreatedBy || target.RowVersion != existing.RowVersion {
		t.Fatalf("stable import identity was not preserved: %+v", target)
	}
}

func TestConsoleProtectsNotificationSecrets(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	cipher, err := secretbox.NewAESGCMSecretCipher("test-v1", map[string]string{"test-v1": key})
	if err != nil {
		t.Fatal(err)
	}
	fc := &FlowgentConsole{ctx: context.Background(), secretCipher: cipher}
	channel := &entities.NotifyChannelInfo{
		BaseEntity: entities.BaseEntity{ID: "channel-id", Namespace: "tenant"},
		Name:       "alerts", ChannelType: entities.NotifWebhook,
		Config: map[string]any{"url": "https://hooks.example.test/secret", "method": "POST"},
	}

	if err := fc.protectChannelSecrets(channel); err != nil {
		t.Fatal(err)
	}
	if _, leaked := channel.Config["url"]; leaked {
		t.Fatal("plaintext webhook URL remained in persisted notification config")
	}
	redacted, err := channel.Redacted()
	if err != nil {
		t.Fatal(err)
	}
	if len(redacted.ConfiguredSecretFields) != 1 || redacted.ConfiguredSecretFields[0] != "url" {
		t.Fatalf("unexpected configured secret projection: %+v", redacted.ConfiguredSecretFields)
	}
	resolved, err := channel.ResolveSecrets(context.Background(), cipher)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Config["url"] != "https://hooks.example.test/secret" {
		t.Fatal("encrypted notification URL did not round-trip")
	}
}
