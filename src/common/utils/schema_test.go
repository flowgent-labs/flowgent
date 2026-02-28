package utils

import "testing"

func TestValidateJSONSchema_Object(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"decision": map[string]any{"type": "boolean"},
			"reason":   map[string]any{"type": "string"},
		},
		"required": []any{"decision", "reason"},
	}
	// Valid
	if err := ValidateJSONSchema(schema, map[string]any{
		"decision": true, "reason": "looks good",
	}); err != nil {
		t.Fatal(err)
	}
	// Missing required
	if err := ValidateJSONSchema(schema, map[string]any{"decision": true}); err == nil {
		t.Fatal("expected error for missing required")
	}
	// Wrong type
	if err := ValidateJSONSchema(schema, map[string]any{
		"decision": "yes", "reason": "ok",
	}); err == nil {
		t.Fatal("expected error for wrong type")
	}
	// Nil schema passes
	if err := ValidateJSONSchema(nil, "anything"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateJSONSchema_Array(t *testing.T) {
	schema := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": map[string]any{"type": "string"}},
			"required":   []any{"id"},
		},
	}
	// Valid
	if err := ValidateJSONSchema(schema, []any{
		map[string]any{"id": "ISS-001"},
		map[string]any{"id": "ISS-002"},
	}); err != nil {
		t.Fatal(err)
	}
	// Missing required in item
	if err := ValidateJSONSchema(schema, []any{
		map[string]any{"id": "ok"},
		map[string]any{"bad": "missing id"},
	}); err == nil {
		t.Fatal("expected error for missing id in item")
	}
	// Not array
	if err := ValidateJSONSchema(schema, "not array"); err == nil {
		t.Fatal("expected error for non-array")
	}
}

func TestValidateJSONSchema_Types(t *testing.T) {
	tests := []struct {
		schema map[string]any
		value  any
		valid  bool
	}{
		{map[string]any{"type": "string"}, "hello", true},
		{map[string]any{"type": "string"}, 123, false},
		{map[string]any{"type": "number"}, 3.14, true},
		{map[string]any{"type": "number"}, "hi", false},
		{map[string]any{"type": "boolean"}, true, true},
		{map[string]any{"type": "boolean"}, "true", false},
		{map[string]any{"type": "integer"}, 42, true},
		{map[string]any{"type": "integer"}, 3.14, false},
		{map[string]any{"type": "array"}, []any{1, 2}, true},
		{map[string]any{"type": "object"}, map[string]any{}, true},
	}
	for _, tt := range tests {
		err := ValidateJSONSchema(tt.schema, tt.value)
		if tt.valid && err != nil {
			t.Errorf("expected valid for %v: %v", tt.schema, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("expected error for %v with value %v", tt.schema, tt.value)
		}
	}
}

func TestValidateJSONSchema_Nested(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"patches": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file":  map[string]any{"type": "string"},
						"patch": map[string]any{"type": "string"},
					},
					"required": []any{"file", "patch"},
				},
			},
		},
	}
	// Valid nested
	if err := ValidateJSONSchema(schema, map[string]any{
		"patches": []any{
			map[string]any{"file": "a.go", "patch": "diff"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// Invalid nested (wrong type in array item)
	if err := ValidateJSONSchema(schema, map[string]any{
		"patches": []any{
			map[string]any{"file": 123, "patch": "diff"},
		},
	}); err == nil {
		t.Fatal("expected error for wrong type in nested")
	}
}

func TestValidateJSONSchema_RequiredOnly(t *testing.T) {
	// Schema without explicit type but with required
	schema := map[string]any{
		"required": []any{"name", "model"},
	}
	if err := ValidateJSONSchema(schema, map[string]any{"name": "test"}); err == nil {
		t.Fatal("expected error for missing required")
	}
	if err := ValidateJSONSchema(schema, map[string]any{"name": "test", "model": "gpt-4"}); err != nil {
		t.Fatal(err)
	}
}
