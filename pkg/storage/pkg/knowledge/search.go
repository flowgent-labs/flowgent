package knowledge

import (
	"strings"
	"unicode"
)

func searchTerms(query string) []string {
	seen := make(map[string]struct{})
	terms := make([]string, 0, 8)
	for _, part := range strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		term := strings.ToLower(strings.TrimSpace(part))
		if len(term) < 2 {
			continue
		}
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
		if len(terms) >= 16 {
			break
		}
	}
	if len(terms) == 0 && strings.TrimSpace(query) != "" {
		terms = append(terms, strings.ToLower(strings.TrimSpace(query)))
	}
	return terms
}
