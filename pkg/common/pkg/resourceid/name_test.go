package resourceid

import "testing"

func TestValidate(t *testing.T) {
	valid := []string{"a", "Team_1", "security-autonomy-fixer", "A123"}
	for _, name := range valid {
		if err := Validate(name); err != nil {
			t.Errorf("Validate(%q) error = %v", name, err)
		}
	}
	invalid := []string{"", "1team", "-team", "team.name", "team name", "abcdefghijklmnopqrstuvwxyzABCDEFG"}
	for _, name := range invalid {
		if err := Validate(name); err == nil {
			t.Errorf("Validate(%q) unexpectedly succeeded", name)
		}
	}
}

func TestValidateNamespaceRejectsRouteConflictsCaseInsensitively(t *testing.T) {
	for _, name := range []string{"flows", "Settings", "NAMESPACES"} {
		if err := ValidateNamespace(name); err == nil {
			t.Errorf("ValidateNamespace(%q) unexpectedly succeeded", name)
		}
	}
	if err := ValidateNamespace("Acme_Team"); err != nil {
		t.Fatalf("ValidateNamespace(Acme_Team) error = %v", err)
	}
}

func TestKubernetesName(t *testing.T) {
	if got := KubernetesName("flowgent", "default"); got != "flowgent-default" {
		t.Fatalf("existing DNS name changed: %q", got)
	}
	if got := KubernetesName("flowgent", "ACME"); got != "flowgent-acme" {
		t.Fatalf("uppercase mapping = %q", got)
	}
	underscore := KubernetesName("flowgent", "Acme_Team")
	hyphen := KubernetesName("flowgent", "Acme-Team")
	if underscore == hyphen {
		t.Fatalf("physical-name collision: %q", underscore)
	}
	if len(KubernetesName("flowgent-jobmanager", "namespace-with-32-characters-aa", "flow-with-32-characters-long-aaaa")) > 63 {
		t.Fatal("Kubernetes name exceeds 63 characters")
	}
}
