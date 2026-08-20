package console

import (
	"strings"
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func TestParseResourceImportAcceptsOnlyCanonicalEnvelope(t *testing.T) {
	canonical := []byte(`
consoleVersion: core.flowgent.io/v1
kind: Flow
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
kind: Flow
metadata: {name: example}
data: {id: example}`),
		"spec payload": []byte(`consoleVersion: core.flowgent.io/v1
kind: Flow
metadata: {name: example}
spec: {id: example}`),
		"unknown version": []byte(`consoleVersion: core.flowgent.io/v2
kind: Flow
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
	var flow entities.FlowInfo
	err := parseSpec([]byte(`{"id":"example","nodes":[],"edges":[],"spec":{}}`), &flow)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field rejection, got %v", err)
	}
}
