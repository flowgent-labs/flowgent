package util

import (
	"fmt"
	"regexp"
	"strings"
)

// Resolve resolves ${node.field} expressions against scope.
func Resolve(v any, scope map[string]map[string]any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	if !strings.Contains(s, "${") {
		return v
	}
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	result := re.ReplaceAllStringFunc(s, func(match string) string {
		path := match[2 : len(match)-1]
		parts := strings.SplitN(path, ".", 2)
		node, ok := scope[parts[0]]
		if !ok {
			return match
		}
		if len(parts) == 1 {
			return fmt.Sprintf("%v", node)
		}
		field := parts[1]
		if val, ok := node[field]; ok {
			return fmt.Sprintf("%v", val)
		}
		return match
	})
	return result
}

// EvalCondition evaluates a condition expression against the scope.
// Supported forms:
//   - ${tribunal.decision}         → resolves to bool directly
//   - ${tribunal.decision == true} → resolves and compares
//   - "true" / "false"         → plain bool literals
func EvalCondition(expr string, scope map[string]map[string]any) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	if expr == "true" {
		return true
	}
	if expr == "false" {
		return false
	}

	// Handle ${node.field == value} comparison
	if strings.HasPrefix(expr, "${") && strings.HasSuffix(expr, "}") && strings.Contains(expr, " ") {
		inner := expr[2 : len(expr)-1] // e.g. "vote.decision == true"
		// Split on comparison operator
		var path, op, rhs string
		if idx := strings.Index(inner, " == "); idx > 0 {
			path = strings.TrimSpace(inner[:idx])
			op = "=="
			rhs = strings.TrimSpace(inner[idx+4:])
		} else if idx := strings.Index(inner, " != "); idx > 0 {
			path = strings.TrimSpace(inner[:idx])
			op = "!="
			rhs = strings.TrimSpace(inner[idx+4:])
		} else {
			return false
		}

		// Resolve the left-hand side
		mockExpr := "${" + path + "}"
		resolved := Resolve(mockExpr, scope)
		lhs := fmt.Sprintf("%v", resolved)

		switch op {
		case "==":
			return lhs == rhs
		case "!=":
			return lhs != rhs
		}
		return false
	}

	// Handle simple ${node.field} (boolean lookup)
	if strings.HasPrefix(expr, "${") && strings.HasSuffix(expr, "}") {
		resolved := Resolve(expr, scope)
		if b, ok := resolved.(bool); ok {
			return b
		}
		if s, ok := resolved.(string); ok {
			return s == "true"
		}
		return false
	}

	// Fallback: check if the expression contains "true"
	return strings.EqualFold(expr, "true")
}
