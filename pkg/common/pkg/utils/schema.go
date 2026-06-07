package utils

import "fmt"

// ValidateJSONSchema performs lightweight validation of a JSON value against a
// JSON Schema (draft-04/draft-07 subset). Supports: type, properties, required,
// items (array type check).
//
// Returns nil if valid, or an error describing the first violation found.
func ValidateJSONSchema(schema map[string]any, value any) error {
	if schema == nil {
		return nil
	}
	return validateNode(schema, value, "$")
}

func validateNode(schema map[string]any, value any, path string) error {
	if t, ok := schema["type"].(string); ok {
		if err := checkType(t, value, path); err != nil {
			return err
		}
	}

	switch schema["type"] {
	case "object":
		return validateObject(schema, value, path)
	case "array":
		return validateArray(schema, value, path)
	}

	// Handle required even without explicit type
	if required, ok := schema["required"].([]any); ok {
		m, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected object for required check", path)
		}
		for _, r := range required {
			key, ok := r.(string)
			if !ok {
				continue
			}
			if _, exists := m[key]; !exists {
				return fmt.Errorf("%s.%s: required field missing", path, key)
			}
		}
	}

	return nil
}

func checkType(expected string, value any, path string) error {
	if value == nil && expected != "null" {
		return fmt.Errorf("%s: expected %s, got null", path, expected)
	}
	switch expected {
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("%s: expected object, got %T", path, value)
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("%s: expected array, got %T", path, value)
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s: expected string, got %T", path, value)
		}
	case "number":
		switch value.(type) {
		case float64, int, int64, float32:
		default:
			return fmt.Errorf("%s: expected number, got %T", path, value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: expected boolean, got %T", path, value)
		}
	case "integer":
		switch value.(type) {
		case int, int64, float64:
			if f, ok := value.(float64); ok && f != float64(int64(f)) {
				return fmt.Errorf("%s: expected integer, got float", path)
			}
		default:
			return fmt.Errorf("%s: expected integer, got %T", path, value)
		}
	}
	return nil
}

func validateObject(schema map[string]any, value any, path string) error {
	m, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: expected object", path)
	}

	if props, ok := schema["properties"].(map[string]any); ok {
		for key, raw := range props {
			propSchema, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if child, exists := m[key]; exists {
				if err := validateNode(propSchema, child, path+"."+key); err != nil {
					return err
				}
			}
		}
	}

	if required, ok := schema["required"].([]any); ok {
		for _, r := range required {
			key, ok := r.(string)
			if !ok {
				continue
			}
			if _, exists := m[key]; !exists {
				return fmt.Errorf("%s.%s: required field missing", path, key)
			}
		}
	}

	return nil
}

func validateArray(schema map[string]any, value any, path string) error {
	arr, ok := value.([]any)
	if !ok {
		return fmt.Errorf("%s: expected array", path)
	}
	if itemSchema, ok := schema["items"].(map[string]any); ok {
		for i, item := range arr {
			if err := validateNode(itemSchema, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}
