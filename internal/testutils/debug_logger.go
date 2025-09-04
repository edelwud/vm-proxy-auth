package testutils

import (
	"fmt"
	"testing"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// DebugLogger is a logger that outputs to testing.T for debugging.
type DebugLogger struct {
	t      *testing.T
	prefix string
}

// NewDebugLogger creates a new debug logger.
func NewDebugLogger(t *testing.T) *DebugLogger {
	return &DebugLogger{t: t}
}

// Debug logs a debug message.
func (dl *DebugLogger) Debug(msg string, fields ...domain.Field) {
	dl.t.Logf("[DEBUG]%s %s %v", dl.prefix, msg, dl.fieldsToMap(fields))
}

// Info logs an info message.
func (dl *DebugLogger) Info(msg string, fields ...domain.Field) {
	dl.t.Logf("[INFO]%s %s %v", dl.prefix, msg, dl.fieldsToMap(fields))
}

// Warn logs a warning message.
func (dl *DebugLogger) Warn(msg string, fields ...domain.Field) {
	dl.t.Logf("[WARN]%s %s %v", dl.prefix, msg, dl.fieldsToMap(fields))
}

// Error logs an error message.
func (dl *DebugLogger) Error(msg string, fields ...domain.Field) {
	dl.t.Logf("[ERROR]%s %s %v", dl.prefix, msg, dl.fieldsToMap(fields))
}

// With creates a new logger with additional fields.
func (dl *DebugLogger) With(fields ...domain.Field) domain.Logger {
	prefix := dl.prefix
	for _, f := range fields {
		prefix += fmt.Sprintf(" [%s=%v]", f.Key, f.Value)
	}
	return &DebugLogger{t: dl.t, prefix: prefix}
}

func (dl *DebugLogger) fieldsToMap(fields []domain.Field) map[string]any {
	m := make(map[string]any)
	for _, f := range fields {
		m[f.Key] = f.Value
	}
	return m
}
