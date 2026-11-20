// Package resourceid defines the stable, URL-safe identity contract shared by
// namespace and flow write paths.
package resourceid

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

const (
	MaxLength = 32
	Pattern   = `^[A-Za-z][A-Za-z0-9_-]{0,31}$`
)

var (
	namePattern        = regexp.MustCompile(Pattern)
	reservedNamespaces = map[string]struct{}{
		"api-keys": {}, "agents": {}, "dashboard": {}, "flows": {},
		"llms": {}, "mcps": {}, "memory": {}, "namespaces": {},
		"notifications": {}, "runs": {}, "settings": {}, "skills": {},
	}
)

// Validate verifies a canonical namespace or flow name. Names are preserved as
// entered, while uniqueness is evaluated case-insensitively by persistence.
func Validate(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("name must start with a letter, contain only letters, numbers, '_' or '-', and be at most %d characters", MaxLength)
	}
	return nil
}

// ValidateNamespace additionally excludes application root routes so the
// canonical /{namespace}/{flow} URL is always unambiguous.
func ValidateNamespace(name string) error {
	if err := Validate(name); err != nil {
		return err
	}
	if _, reserved := reservedNamespaces[strings.ToLower(name)]; reserved {
		return fmt.Errorf("namespace name %q is reserved", name)
	}
	return nil
}

func IsValid(name string) bool { return Validate(name) == nil }

func IsValidNamespace(name string) bool { return ValidateNamespace(name) == nil }

// KubernetesName maps logical identifiers to a DNS-label-safe physical name.
// Existing lowercase/hyphen names remain unchanged. Transformations that can
// collapse distinct logical names (notably '_' to '-') and truncation gain a
// stable hash suffix.
func KubernetesName(parts ...string) string {
	raw := strings.Join(parts, "-")
	lower := strings.ToLower(raw)
	var normalized strings.Builder
	previousHyphen := false
	needsHash := false
	for _, char := range lower {
		valid := char >= 'a' && char <= 'z' || char >= '0' && char <= '9'
		if valid {
			normalized.WriteRune(char)
			previousHyphen = false
			continue
		}
		if char != '-' {
			needsHash = true
		}
		if !previousHyphen {
			normalized.WriteByte('-')
			previousHyphen = true
		}
	}
	base := strings.Trim(normalized.String(), "-")
	if base == "" {
		base = "flowgent"
		needsHash = true
	}
	if len(base) > 63 {
		needsHash = true
	}
	if !needsHash {
		return base
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ToLower(raw))))[:10]
	maxBase := 63 - 1 - len(hash)
	if len(base) > maxBase {
		base = strings.TrimRight(base[:maxBase], "-")
	}
	return base + "-" + hash
}
