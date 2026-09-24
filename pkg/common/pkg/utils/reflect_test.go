package utils

import (
	"reflect"
	"testing"
	"time"
)

func TestStructFieldsPreservesTimestampInstantInUTC(t *testing.T) {
	local := time.Date(2026, time.September, 20, 17, 30, 0, 123, time.FixedZone("UTC+8", 8*60*60))
	type record struct {
		CreatedAt time.Time  `db:"created_at"`
		Finished  *time.Time `db:"finished_at"`
	}
	cols, args := StructFields(&record{CreatedAt: local, Finished: &local})
	if !reflect.DeepEqual(cols, []string{"created_at", "finished_at"}) {
		t.Fatalf("StructFields() columns = %#v", cols)
	}
	for index, argument := range args {
		got, ok := argument.(time.Time)
		if !ok {
			t.Fatalf("argument %d type = %T, want time.Time", index, argument)
		}
		if !got.Equal(local) || got.Location() != time.UTC {
			t.Fatalf("argument %d = %v (%v), want same instant in UTC", index, got, got.Location())
		}
	}
}

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
