// Package secretref validates and resolves references to secrets injected into
// a process environment. Persisted resource definitions contain references,
// never the referenced plaintext values.
package secretref

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

const Prefix = "env://"

var (
	envNamePattern  = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	templatePattern = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)
)

// EnvName extracts an environment variable name from env://NAME or ${NAME}.
func EnvName(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, Prefix) {
		name := strings.TrimPrefix(value, Prefix)
		return name, envNamePattern.MatchString(name)
	}
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		name := value[2 : len(value)-1]
		return name, envNamePattern.MatchString(name)
	}
	return "", false
}

// Normalize converts a supported environment reference to its persisted form.
func Normalize(value string) (string, error) {
	name, ok := EnvName(value)
	if !ok {
		return "", fmt.Errorf("secret must be an environment reference such as ${API_TOKEN}")
	}
	return Prefix + name, nil
}

// TemplateIsReference reports whether a string contains at least one valid
// ${NAME} reference and contains no malformed dollar expansion.
func TemplateIsReference(value string) bool {
	matches := templatePattern.FindAllStringIndex(value, -1)
	if len(matches) == 0 {
		return false
	}
	remaining := templatePattern.ReplaceAllString(value, "")
	return !strings.Contains(remaining, "${")
}

// IsSensitiveHeader identifies HTTP header fields whose values commonly carry
// credentials and therefore must be persisted as environment references.
func IsSensitiveHeader(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "authorization" || name == "proxy-authorization" || name == "cookie" ||
		strings.Contains(name, "api-key") || strings.Contains(name, "apikey") ||
		strings.Contains(name, "token") || strings.Contains(name, "secret")
}

// Expand resolves every ${NAME} reference and fails closed when an injected
// value is missing. It deliberately avoids os.ExpandEnv's silent empty value.
func Expand(value string) (string, error) {
	var missing string
	resolved := templatePattern.ReplaceAllStringFunc(value, func(match string) string {
		name := match[2 : len(match)-1]
		secret, ok := os.LookupEnv(name)
		if !ok || secret == "" {
			missing = name
			return ""
		}
		return secret
	})
	if missing != "" {
		return "", fmt.Errorf("secret environment variable %s is not configured", missing)
	}
	if strings.Contains(resolved, "${") {
		return "", fmt.Errorf("invalid secret environment reference")
	}
	return resolved, nil
}

// Resolve resolves an exact env://NAME or ${NAME} reference.
func Resolve(value string) (string, error) {
	name, ok := EnvName(value)
	if !ok {
		return "", fmt.Errorf("invalid secret environment reference")
	}
	secret, ok := os.LookupEnv(name)
	if !ok || secret == "" {
		return "", fmt.Errorf("secret environment variable %s is not configured", name)
	}
	return secret, nil
}
