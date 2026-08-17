package utils

import "testing"

func TestParseTimeAcceptsModerncSQLitePointerEncoding(t *testing.T) {
	t.Parallel()
	parsed, err := ParseTime("2026-08-14 04:00:00.25 +0000 UTC")
	if err != nil {
		t.Fatalf("ParseTime: %v", err)
	}
	if parsed.Year() != 2026 || parsed.Nanosecond() != 250_000_000 {
		t.Fatalf("parsed = %s", parsed)
	}
}
