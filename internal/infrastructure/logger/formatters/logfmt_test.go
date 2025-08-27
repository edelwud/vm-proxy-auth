//nolint:testpackage // Testing internal methods
package formatters

import (
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

//nolint:gocognit // Test function with comprehensive test cases
func TestLogFmtFormatter_Format(t *testing.T) {
	tests := []struct {
		name        string
		entry       *logrus.Entry
		contains    []string // Expected key=value pairs to be present
		notContains []string
	}{
		{
			name: "basic log entry",
			entry: &logrus.Entry{
				Message: "test message",
				Level:   logrus.InfoLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data:    logrus.Fields{},
			},
			contains: []string{
				"level=info",
				"time=2023-01-01T12:00:00.000Z",
				"msg=\"test message\"",
			},
		},
		{
			name: "entry with data fields",
			entry: &logrus.Entry{
				Message: "user authenticated",
				Level:   logrus.InfoLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data: logrus.Fields{
					"user_id":    "123",
					"component":  "auth",
					"request_id": "req-456",
				},
			},
			contains: []string{
				"level=info",
				"msg=\"user authenticated\"",
				"component=auth",
				"request_id=req-456",
				"user_id=123",
			},
		},
		{
			name: "entry with special characters",
			entry: &logrus.Entry{
				Message: "test with spaces and = signs",
				Level:   logrus.WarnLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data: logrus.Fields{
					"query":       "sum(cpu_usage{cluster=\"prod\"})",
					"empty_field": "",
					"space_value": "value with spaces",
				},
			},
			contains: []string{
				"level=warn",
				"msg=\"test with spaces and = signs\"",
				"query=\"sum(cpu_usage{cluster=\\\"prod\\\"})\"",
				"empty_field=\"\"",
				"space_value=\"value with spaces\"",
			},
		},
		{
			name: "debug level entry",
			entry: &logrus.Entry{
				Message: "debug info",
				Level:   logrus.DebugLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data:    logrus.Fields{"trace": "enabled"},
			},
			contains: []string{
				"level=debug",
				"msg=\"debug info\"",
				"trace=enabled",
			},
		},
		{
			name: "error level entry",
			entry: &logrus.Entry{
				Message: "error occurred",
				Level:   logrus.ErrorLevel,
				Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Data: logrus.Fields{
					"error":       "connection timeout",
					"retry_count": 3,
				},
			},
			contains: []string{
				"level=error",
				"msg=\"error occurred\"",
				"error=\"connection timeout\"",
				"retry_count=3",
			},
		},
	}

	formatter := NewLogFmtFormatter()

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

			// Check that unwanted strings are not present
			for _, notExpected := range tt.notContains {
				if strings.Contains(output, notExpected) {
					t.Errorf("Expected output to NOT contain %q, but got: %s", notExpected, output)
				}
			}

			// Verify output ends with newline
			if !strings.HasSuffix(output, "\n") {
				t.Errorf("Expected output to end with newline, but got: %s", output)
			}
		})
	}
}

func TestLogFmtFormatter_FormatValue(t *testing.T) {
	formatter := NewLogFmtFormatter()

	tests := []struct {
		name     string
		value    interface{}
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
			name:     "string with equals",
			value:    "key=value",
			expected: `"key=value"`,
		},
		{
			name:     "string with quotes",
			value:    `say "hello"`,
			expected: `"say \"hello\""`,
		},
		{
			name:     "empty string",
			value:    "",
			expected: `""`,
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
			result := formatter.formatValue(tt.value)
			if result != tt.expected {
				t.Errorf("formatValue() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestLogFmtFormatter_NeedsQuoting(t *testing.T) {
	formatter := NewLogFmtFormatter()

	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		{
			name:     "simple string",
			value:    "hello",
			expected: false,
		},
		{
			name:     "empty string",
			value:    "",
			expected: true,
		},
		{
			name:     "string with space",
			value:    "hello world",
			expected: true,
		},
		{
			name:     "string with equals",
			value:    "key=value",
			expected: true,
		},
		{
			name:     "string with quote",
			value:    `say "hello"`,
			expected: true,
		},
		{
			name:     "string with tab",
			value:    "hello\tworld",
			expected: true,
		},
		{
			name:     "string with newline",
			value:    "hello\nworld",
			expected: true,
		},
		{
			name:     "string with carriage return",
			value:    "hello\rworld",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.needsQuoting(tt.value)
			if result != tt.expected {
				t.Errorf("needsQuoting() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestLogFmtFormatter_FieldOrdering(t *testing.T) {
	formatter := NewLogFmtFormatter()

	// Create entry with fields in alphabetical order to test sorting
	entry := &logrus.Entry{
		Message: "test ordering",
		Level:   logrus.InfoLevel,
		Time:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		Data: logrus.Fields{
			"zebra": "last",
			"alpha": "first",
			"beta":  "second",
			"gamma": "third",
		},
	}

	result, err := formatter.Format(entry)
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}

	output := string(result)

	// Find positions of each field in the output
	alphaPos := strings.Index(output, "alpha=first")
	betaPos := strings.Index(output, "beta=second")
	gammaPos := strings.Index(output, "gamma=third")
	zebraPos := strings.Index(output, "zebra=last")

	// Verify alphabetical ordering
	if alphaPos == -1 || betaPos == -1 || gammaPos == -1 || zebraPos == -1 {
		t.Fatalf("Not all fields found in output: %s", output)
	}

	if alphaPos >= betaPos || betaPos >= gammaPos || gammaPos >= zebraPos {
		t.Errorf("Fields are not in alphabetical order in output: %s", output)
	}
}
