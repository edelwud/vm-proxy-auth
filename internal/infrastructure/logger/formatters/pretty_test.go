//nolint:testpackage // Testing internal methods
package formatters

import (
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestPrettyFormatter_Format(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		entry    *logrus.Entry
		contains []string
		setup    func()
		cleanup  func()
	}{
		{
			name: "basic info entry",
			entry: &logrus.Entry{
				Message: "test message",
				Level:   logrus.InfoLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data:    logrus.Fields{},
			},
			contains: []string{
				"2023-01-01 12:00:00",
				"INFO ",
				"test message",
			},
		},
		{
			name: "error entry with fields",
			entry: &logrus.Entry{
				Message: "authentication failed",
				Level:   logrus.ErrorLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data: logrus.Fields{
					"user_id":   "123",
					"error":     "invalid token",
					"component": "auth",
				},
			},
			contains: []string{
				"2023-01-01 12:00:00",
				"ERROR",
				"authentication failed",
				"user_id=123",
				"error=\"invalid token\"",
				"component=auth",
			},
		},
		{
			name: "debug entry",
			entry: &logrus.Entry{
				Message: "parsing query",
				Level:   logrus.DebugLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data: logrus.Fields{
					"query":    "sum(cpu_usage)",
					"duration": "5ms",
				},
			},
			contains: []string{
				"2023-01-01 12:00:00",
				"DEBUG",
				"parsing query",
				"query=sum(cpu_usage)",
				"duration=5ms",
			},
		},
		{
			name: "warn entry",
			entry: &logrus.Entry{
				Message: "high memory usage",
				Level:   logrus.WarnLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data: logrus.Fields{
					"memory_percent": 85,
					"threshold":      80,
				},
			},
			contains: []string{
				"2023-01-01 12:00:00",
				"WARNING",
				"high memory usage",
				"memory_percent=85",
				"threshold=80",
			},
		},
		{
			name: "entry with quoted values",
			entry: &logrus.Entry{
				Message: "request processed",
				Level:   logrus.InfoLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data: logrus.Fields{
					"method":     "GET",
					"path":       "/api/v1/query",
					"user_agent": "Mozilla/5.0 (compatible)",
				},
			},
			contains: []string{
				"2023-01-01 12:00:00",
				"INFO ",
				"request processed",
				"method=GET",
				"path=/api/v1/query",
				"\"Mozilla/5.0 (compatible)\"",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.setup != nil {
				tt.setup()
			}
			if tt.cleanup != nil {
				defer tt.cleanup()
			}

			formatter := NewPrettyFormatter()
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

func TestPrettyFormatter_FormatLevel(t *testing.T) {
	t.Parallel()

	formatter := NewPrettyFormatter()

	tests := []struct {
		name     string
		level    logrus.Level
		expected string
	}{
		{
			name:     "info level",
			level:    logrus.InfoLevel,
			expected: "INFO ",
		},
		{
			name:     "error level",
			level:    logrus.ErrorLevel,
			expected: "ERROR",
		},
		{
			name:     "warn level",
			level:    logrus.WarnLevel,
			expected: "WARNING",
		},
		{
			name:     "debug level",
			level:    logrus.DebugLevel,
			expected: "DEBUG",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := formatter.formatLevel(tt.level)

			// Remove color codes for easier testing
			result = stripAnsiCodes(result)

			if result != tt.expected {
				t.Errorf("formatLevel() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestPrettyFormatter_PadMessage(t *testing.T) {
	t.Parallel()

	formatter := NewPrettyFormatter()

	tests := []struct {
		name     string
		message  string
		expected int // Expected minimum length
	}{
		{
			name:     "short message",
			message:  "test",
			expected: 35,
		},
		{
			name:     "long message",
			message:  "this is a very long message that exceeds the maximum width",
			expected: len("this is a very long message that exceeds the maximum width"),
		},
		{
			name:     "exact width message",
			message:  strings.Repeat("x", 35),
			expected: 35,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := formatter.padMessage(tt.message)
			if len(result) < tt.expected {
				t.Errorf("padMessage() length = %v, want at least %v", len(result), tt.expected)
			}

			// Ensure the original message is preserved
			if !strings.Contains(result, tt.message) {
				t.Errorf("padMessage() should contain original message %q, got %q", tt.message, result)
			}
		})
	}
}

func TestPrettyFormatter_FormatFieldValue(t *testing.T) {
	t.Parallel()

	formatter := NewPrettyFormatter()

	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{
			name:     "nil value",
			value:    nil,
			expected: "<nil>",
		},
		{
			name:     "simple string",
			value:    "hello",
			expected: "hello",
		},
		{
			name:     "string with spaces",
			value:    "hello world",
			expected: `"hello world"`,
		},
		{
			name:     "integer",
			value:    42,
			expected: "42",
		},
		{
			name:     "boolean",
			value:    true,
			expected: "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := formatter.formatFieldValue(tt.value)

			// Remove color codes for easier testing
			result = stripAnsiCodes(result)

			if result != tt.expected {
				t.Errorf("formatFieldValue() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestPrettyFormatter_ColorSupport(t *testing.T) {
	// Skip color detection tests in test environment
	// The color detection depends on os.Stdout being a terminal, which it isn't during tests
	t.Skip("Color detection tests skipped - requires terminal environment")
}

func TestPrettyFormatter_FormatFields(t *testing.T) {
	t.Parallel()
	formatter := NewPrettyFormatter()

	tests := []struct {
		name     string
		fields   logrus.Fields
		expected []string // Strings that should be present
	}{
		{
			name:     "empty fields",
			fields:   logrus.Fields{},
			expected: []string{},
		},
		{
			name: "important fields prioritized",
			fields: logrus.Fields{
				"user_id":    "123",
				"request_id": "req-456",
				"other":      "value",
				"component":  "auth",
			},
			expected: []string{
				"user_id=123",
				"request_id=req-456",
				"component=auth",
				"other=value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := formatter.formatFields(tt.fields)

			// Remove color codes for easier testing
			result = stripAnsiCodes(result)

			for _, expected := range tt.expected {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected formatFields() to contain %q, but got: %s", expected, result)
				}
			}
		})
	}
}

// stripAnsiCodes removes ANSI color codes from text for testing.
func stripAnsiCodes(text string) string {
	// Simple implementation to remove common ANSI escape sequences
	text = strings.ReplaceAll(text, ColorReset, "")
	text = strings.ReplaceAll(text, ColorRed, "")
	text = strings.ReplaceAll(text, ColorYellow, "")
	text = strings.ReplaceAll(text, ColorBlue, "")
	text = strings.ReplaceAll(text, ColorGray, "")
	text = strings.ReplaceAll(text, ColorBold, "")
	text = strings.ReplaceAll(text, ColorCyan, "")
	return text
}
