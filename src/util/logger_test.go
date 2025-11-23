package util

import (
	"testing"
)

func TestNewLogger_JSON(t *testing.T) {
	l := NewLogger("JSON", "DEBUG")
	if l == nil {
		t.Fatal("logger should not be nil")
	}
	l.Debug("test", "key", "value")
	l.Info("test", "key", "value")
	l.Warn("test", "key", "value")
	l.Error("test", "key", "value")
}

func TestNewLogger_Text(t *testing.T) {
	l := NewLogger("HUMAN", "INFO")
	if l == nil {
		t.Fatal("logger should not be nil")
	}
	l.Info("hello")
}

func TestNewLogger_DefaultLevel(t *testing.T) {
	l := NewLogger("JSON", "")
	if l == nil {
		t.Fatal("logger should not be nil")
	}
}

func TestTruncateJSON_Short(t *testing.T) {
	s := TruncateJSON(map[string]string{"a": "b"}, 100)
	if s != `{"a":"b"}` {
		t.Errorf("unexpected: %s", s)
	}
}

func TestTruncateJSON_Long(t *testing.T) {
	s := TruncateJSON(map[string]string{"a": "very-long-value-that-exceeds-the-limit"}, 10)
	if len(s) > 30 {
		t.Errorf("expected truncated, got %d chars: %s", len(s), s)
	}
}
