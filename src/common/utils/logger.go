package utils

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
)

type Logger struct {
	*slog.Logger
}

func NewLogger(mode, level string) *Logger {
	var handler slog.Handler
	opts := &slog.HandlerOptions{}
	switch level {
	case "DEBUG":
		opts.Level = slog.LevelDebug
	case "INFO":
		opts.Level = slog.LevelInfo
	case "WARN":
		opts.Level = slog.LevelWarn
	case "ERROR":
		opts.Level = slog.LevelError
	default:
		opts.Level = slog.LevelInfo
	}
	if mode == "JSON" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	return &Logger{slog.New(handler)}
}

func (l *Logger) Debug(msg string, args ...any) { l.LogAttrs(nil, slog.LevelDebug, msg, toAttrs(args)...) }
func (l *Logger) Info(msg string, args ...any)  { l.LogAttrs(nil, slog.LevelInfo, msg, toAttrs(args)...) }
func (l *Logger) Warn(msg string, args ...any)  { l.LogAttrs(nil, slog.LevelWarn, msg, toAttrs(args)...) }
func (l *Logger) Error(msg string, args ...any) { l.LogAttrs(nil, slog.LevelError, msg, toAttrs(args)...) }

func toAttrs(args []any) []slog.Attr {
	var attrs []slog.Attr
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			attrs = append(attrs, slog.Any(fmt.Sprint(args[i]), args[i+1]))
		}
	}
	return attrs
}

// TruncateJSON serializes and truncates a value for tracing/logging.
func TruncateJSON(v any, maxLen int) string {
	b, _ := json.Marshal(v)
	if len(b) > maxLen {
		return string(b[:maxLen]) + "...(truncated)"
	}
	return string(b)
}
