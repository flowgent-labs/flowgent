package secretref

import "testing"

func TestNormalizeAndResolve(t *testing.T) {
	t.Setenv("FLOWGENT_TEST_TOKEN", "top-secret")

	for _, value := range []string{"${FLOWGENT_TEST_TOKEN}", "env://FLOWGENT_TEST_TOKEN"} {
		normalized, err := Normalize(value)
		if err != nil || normalized != "env://FLOWGENT_TEST_TOKEN" {
			t.Fatalf("Normalize(%q) = %q, %v", value, normalized, err)
		}
		resolved, err := Resolve(value)
		if err != nil || resolved != "top-secret" {
			t.Fatalf("Resolve(%q) = %q, %v", value, resolved, err)
		}
	}
}

func TestRejectsInlineAndMissingSecrets(t *testing.T) {
	if _, err := Normalize("plaintext"); err == nil {
		t.Fatal("inline secret was accepted")
	}
	if _, err := Resolve("${FLOWGENT_MISSING_TOKEN}"); err == nil {
		t.Fatal("missing environment secret was accepted")
	}
}

func TestExpandTemplate(t *testing.T) {
	t.Setenv("FLOWGENT_TEST_TOKEN", "token")
	if !TemplateIsReference("Bearer ${FLOWGENT_TEST_TOKEN}") {
		t.Fatal("valid template was rejected")
	}
	got, err := Expand("Bearer ${FLOWGENT_TEST_TOKEN}")
	if err != nil || got != "Bearer token" {
		t.Fatalf("Expand() = %q, %v", got, err)
	}
	if TemplateIsReference("Bearer plaintext") {
		t.Fatal("inline credential was treated as a reference")
	}
}
