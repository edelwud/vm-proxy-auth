//nolint:testpackage // Testing internal methods
package formatters

import (
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestConsoleFormatter_Format(t *testing.T) {
	tests := []struct {
		name     string
		entry    *logrus.Entry
		contains []string
	}{
		{
			name: "basic info entry",
			entry: &logrus.Entry{
				Message: "server started",
				Level:   logrus.InfoLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data:    logrus.Fields{},
			},
			contains: []string{
				"[INFO] server started",
			},
		},
		{
			name: "error entry with important fields",
			entry: &logrus.Entry{
				Message: "authentication failed",
				Level:   logrus.ErrorLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data: logrus.Fields{
					"user_id":    "123",
					"error":      "invalid token",
					"component":  "auth",
					"extra_data": "should not appear", // Not in important fields list
				},
			},
			contains: []string{
				"[ERROR] authentication failed - user_id: 123, error: invalid token, component: auth",
			},
		},
		{
			name: "warn entry with HTTP fields",
			entry: &logrus.Entry{
				Message: "slow request",
				Level:   logrus.WarnLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data: logrus.Fields{
					"method":   "POST",
					"path":     "/api/v1/query",
					"duration": "2.5s",
					"status":   "200",
					"user_id":  "456",
				},
			},
			contains: []string{
				"[WARNING] slow request - user_id: 456, status: 200, method: POST, path: /api/v1/query, duration: 2.5s",
			},
		},
		{
			name: "debug entry",
			entry: &logrus.Entry{
				Message: "parsing query",
				Level:   logrus.DebugLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data: logrus.Fields{
					"request_id": "req-789",
					"tenant_id":  "tenant-abc",
				},
			},
			contains: []string{
				"[DEBUG] parsing query",
				"request_id: req-789",
				"tenant_id: tenant-abc",
			},
		},
		{
			name: "entry with no important fields",
			entry: &logrus.Entry{
				Message: "background task completed",
				Level:   logrus.InfoLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data: logrus.Fields{
					"task_id":     "task-123",
					"internal_id": "int-456",
				},
			},
			contains: []string{
				"[INFO] background task completed",
			},
		},
		{
			name: "entry with nil and empty values",
			entry: &logrus.Entry{
				Message: "processing request",
				Level:   logrus.InfoLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data: logrus.Fields{
					"user_id":   "",      // Empty string should not appear
					"error":     nil,     // Nil should not appear
					"method":    "GET",   // Should appear
					"component": "proxy", // Should appear
				},
			},
			contains: []string{
				"[INFO] processing request - method: GET, component: proxy",
			},
		},
	}

	formatter := NewConsoleFormatter()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := formatter.Format(tt.entry)
			if err != nil {
				t.Fatalf("Format() error = %v", err)
			}

			output := string(result)

			// Check that all expected strings are present
			for _, expected := range tt.contains {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, but got: %s", expected, output)
				}
			}

			// Verify output ends with newline
			if !strings.HasSuffix(output, "\n") {
				t.Errorf("Expected output to end with newline, but got: %s", output)
			}
		})
	}
}

func TestConsoleFormatter_ExtractImportantFields(t *testing.T) {
	formatter := NewConsoleFormatter()

	tests := []struct {
		name     string
		fields   logrus.Fields
		expected string
	}{
		{
			name:     "empty fields",
			fields:   logrus.Fields{},
			expected: "",
		},
		{
			name: "only important fields",
			fields: logrus.Fields{
				"user_id": "123",
				"method":  "GET",
				"path":    "/api/v1/query",
			},
			expected: "- user_id: 123, method: GET, path: /api/v1/query",
		},
		{
			name: "mixed important and unimportant fields",
			fields: logrus.Fields{
				"user_id":      "123",
				"internal_ref": "should-not-appear",
				"error":        "connection failed",
				"debug_info":   "also-should-not-appear",
				"status":       "500",
			},
			expected: "- user_id: 123, error: connection failed, status: 500",
		},
		{
			name: "fields with nil and empty values",
			fields: logrus.Fields{
				"user_id":    "",        // Should not appear
				"error":      nil,       // Should not appear
				"method":     "POST",    // Should appear
				"component":  "auth",    // Should appear
				"request_id": "req-123", // Should appear
			},
			expected: "- method: POST, component: auth, request_id: req-123",
		},
		{
			name: "all important field types",
			fields: logrus.Fields{
				"user_id":    "user123",
				"user":       "john",
				"error":      "timeout",
				"status":     "error",
				"method":     "POST",
				"path":       "/api/query",
				"duration":   "1.5s",
				"tenant_id":  "tenant456",
				"component":  "gateway",
				"request_id": "req789",
			},
			expected: "- user_id: user123, user: john, error: timeout, status: error, method: POST, path: /api/query, duration: 1.5s, tenant_id: tenant456, component: gateway, request_id: req789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.extractImportantFields(tt.fields)

			if result != tt.expected {
				t.Errorf("extractImportantFields() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestConsoleFormatter_LevelFormatting(t *testing.T) {
	formatter := NewConsoleFormatter()

	tests := []struct {
		name     string
		level    logrus.Level
		expected string
	}{
		{
			name:     "info level",
			level:    logrus.InfoLevel,
			expected: "[INFO]",
		},
		{
			name:     "error level",
			level:    logrus.ErrorLevel,
			expected: "[ERROR]",
		},
		{
			name:     "warn level",
			level:    logrus.WarnLevel,
			expected: "[WARNING]",
		},
		{
			name:     "debug level",
			level:    logrus.DebugLevel,
			expected: "[DEBUG]",
		},
		{
			name:     "trace level",
			level:    logrus.TraceLevel,
			expected: "[TRACE]",
		},
		{
			name:     "fatal level",
			level:    logrus.FatalLevel,
			expected: "[FATAL]",
		},
		{
			name:     "panic level",
			level:    logrus.PanicLevel,
			expected: "[PANIC]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &logrus.Entry{
				Message: "test message",
				Level:   tt.level,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data:    logrus.Fields{},
			}

			result, err := formatter.Format(entry)
			if err != nil {
				t.Fatalf("Format() error = %v", err)
			}

			output := string(result)

			if !strings.HasPrefix(output, tt.expected) {
				t.Errorf("Expected output to start with %q, but got: %s", tt.expected, output)
			}
		})
	}
}

func TestConsoleFormatter_MessagePreservation(t *testing.T) {
	formatter := NewConsoleFormatter()

	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "simple message",
			message: "hello world",
		},
		{
			name:    "message with special characters",
			message: "query: sum(cpu_usage{cluster=\"prod\"})",
		},
		{
			name:    "empty message",
			message: "",
		},
		{
			name:    "message with newlines",
			message: "line1\nline2\nline3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &logrus.Entry{
				Message: tt.message,
				Level:   logrus.InfoLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data:    logrus.Fields{},
			}

			result, err := formatter.Format(entry)
			if err != nil {
				t.Fatalf("Format() error = %v", err)
			}

			output := string(result)

			// The message should appear exactly as provided after the level prefix
			if !strings.Contains(output, tt.message) {
				t.Errorf("Expected output to contain message %q, but got: %s", tt.message, output)
			}
		})
	}
}
