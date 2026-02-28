package utils

import (
	"testing"
)

func TestResolve_Simple(t *testing.T) {
	scope := map[string]map[string]any{
		"vars": {"key": "value"},
		"node": {"field": 42},
	}

	tests := []struct {
		input    any
		expected any
	}{
		{"hello", "hello"},
		{"${vars.key}", "value"},
		{123, 123},
		{true, true},
	}

	for _, tt := range tests {
		got := Resolve(tt.input, scope)
		if got != tt.expected {
			t.Errorf("Resolve(%v) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestResolve_IntToString(t *testing.T) {
	scope := map[string]map[string]any{
		"node": {"field": 42},
	}
	got := Resolve("${node.field}", scope)
	if got != "42" {
		t.Errorf("expected '42', got %v", got)
	}
}

func TestResolve_BoolToString(t *testing.T) {
	scope := map[string]map[string]any{
		"tribunal": {"decision": true},
	}
	got := Resolve("${tribunal.decision}", scope)
	if got != "true" {
		t.Errorf("expected 'true', got %v", got)
	}
}

func TestResolve_NoMatch(t *testing.T) {
	scope := map[string]map[string]any{
		"vars": {"key": "value"},
	}
	got := Resolve("${missing.field}", scope)
	if got != "${missing.field}" {
		t.Errorf("expected '${missing.field}', got %v", got)
	}
}

func TestResolve_NestedMap(t *testing.T) {
	scope := map[string]map[string]any{
		"node": {"items": []any{"a", "b"}},
	}
	got := Resolve("${node.items}", scope)
	if got != "[a b]" {
		t.Errorf("expected '[a b]', got %v", got)
	}
}

func TestEvalCondition_True(t *testing.T) {
	scope := map[string]map[string]any{
		"tribunal": {"decision": true},
	}
	if !EvalCondition("${tribunal.decision == true}", scope) {
		t.Error("expected true")
	}
}

func TestEvalCondition_False(t *testing.T) {
	scope := map[string]map[string]any{
		"tribunal": {"decision": false},
	}
	if EvalCondition("${tribunal.decision == true}", scope) {
		t.Error("expected false")
	}
}

func TestEvalCondition_BoolDirect(t *testing.T) {
	scope := map[string]map[string]any{
		"tribunal": {"decision": true},
	}
	if !EvalCondition("${tribunal.decision}", scope) {
		t.Error("expected true from direct bool access")
	}
}

func TestEvalCondition_Empty(t *testing.T) {
	if EvalCondition("", nil) {
		t.Error("empty expression should be false")
	}
}

func TestEvalCondition_PlainTrue(t *testing.T) {
	if !EvalCondition("true", nil) {
		t.Error("'true' should be true")
	}
}

func TestEvalCondition_PlainFalse(t *testing.T) {
	if EvalCondition("false", nil) {
		t.Error("'false' should be false")
	}
}

func TestTruncateJSON(t *testing.T) {
	tests := []struct {
		input  any
		maxLen int
	}{
		{map[string]any{"key": "value"}, 100},
		{map[string]any{"key": "value"}, 5},
		{nil, 100},
	}
	for _, tt := range tests {
		result := TruncateJSON(tt.input, tt.maxLen)
		if tt.input == nil && result != "null" {
			t.Errorf("nil -> %s", result)
		}
		if tt.maxLen < 10 && len(result) > tt.maxLen+20 {
			t.Errorf("truncated result too long: %d > %d", len(result), tt.maxLen+20)
		}
	}
}
