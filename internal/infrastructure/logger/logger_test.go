//nolint:testpackage // Testing internal methods
package logger

import (
	"bytes"
	"strings"
	"testing"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/sirupsen/logrus"
)

func TestNewStructuredLogger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		level        string
		format       string
		expectedType string
	}{
		{
			name:         "json format",
			level:        "info",
			format:       "json",
			expectedType: "*logrus.JSONFormatter",
		},
		{
			name:         "logfmt format",
			level:        "debug",
			format:       "logfmt",
			expectedType: "*formatters.LogFmtFormatter",
		},
		{
			name:         "pretty format",
			level:        "warn",
			format:       "pretty",
			expectedType: "*formatters.PrettyFormatter",
		},
		{
			name:         "console format",
			level:        "error",
			format:       "console",
			expectedType: "*formatters.ConsoleFormatter",
		},
		{
			name:         "default format (unknown)",
			level:        "info",
			format:       "unknown",
			expectedType: "*logrus.JSONFormatter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger := NewStructuredLogger(tt.level, tt.format)

			if logger == nil {
				t.Fatal("Expected logger to be created, got nil")
			}

			// Type assertion to check we got StructuredLogger
			structLogger, ok := logger.(*StructuredLogger)
			if !ok {
				t.Fatalf("Expected *StructuredLogger, got %T", logger)
			}

			// Check that the logger was properly initialized
			if structLogger.logger == nil {
				t.Error("Expected internal logrus logger to be initialized")
			}

			if structLogger.fields == nil {
				t.Error("Expected fields map to be initialized")
			}
		})
	}
}

func TestNewEnhancedStructuredLogger(t *testing.T) {
	t.Parallel()

	logger := NewEnhancedStructuredLogger("info", "json")

	if logger == nil {
		t.Fatal("Expected enhanced logger to be created, got nil")
	}

	if logger.Logger == nil {
		t.Error("Expected base logger to be initialized")
	}
}

func TestStructuredLogger_LogLevels(t *testing.T) {
	t.Parallel()

	// Capture log output
	var buf bytes.Buffer
	logger := logrus.New()
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetLevel(logrus.DebugLevel) // Set to debug to capture all log levels

	structLogger := &StructuredLogger{
		logger: logger,
		fields: make(logrus.Fields),
	}

	tests := []struct {
		name     string
		logFunc  func(string, ...domain.Field)
		message  string
		fields   []domain.Field
		expected string
	}{
		{
			name:     "debug level",
			logFunc:  structLogger.Debug,
			message:  "debug message",
			fields:   []domain.Field{{Key: "test", Value: "debug"}},
			expected: "debug message",
		},
		{
			name:     "info level",
			logFunc:  structLogger.Info,
			message:  "info message",
			fields:   []domain.Field{{Key: "test", Value: "info"}},
			expected: "info message",
		},
		{
			name:     "warn level",
			logFunc:  structLogger.Warn,
			message:  "warn message",
			fields:   []domain.Field{{Key: "test", Value: "warn"}},
			expected: "warn message",
		},
		{
			name:     "error level",
			logFunc:  structLogger.Error,
			message:  "error message",
			fields:   []domain.Field{{Key: "test", Value: "error"}},
			expected: "error message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			
			// Each subtest gets its own buffer and logger to avoid race conditions
			var testBuf bytes.Buffer
			testLogger := logrus.New()
			testLogger.SetOutput(&testBuf)
			testLogger.SetFormatter(&logrus.JSONFormatter{})
			testLogger.SetLevel(logrus.DebugLevel)
			
			testStructLogger := &StructuredLogger{
				logger: testLogger,
				fields: make(logrus.Fields),
			}
			
			// Execute the log function using the test-specific logger
			switch tt.name {
			case "debug level":
				testStructLogger.Debug(tt.message, tt.fields...)
			case "info level":
				testStructLogger.Info(tt.message, tt.fields...)
			case "warning level":
				testStructLogger.Warn(tt.message, tt.fields...)
			case "error level":
				testStructLogger.Error(tt.message, tt.fields...)
			default:
				tt.logFunc(tt.message, tt.fields...)
			}

			output := testBuf.String()
			if !strings.Contains(output, tt.expected) {
				t.Errorf("Expected output to contain %q, but got: %s", tt.expected, output)
			}

			// Verify field is included
			if len(tt.fields) > 0 {
				if !strings.Contains(output, tt.fields[0].Key) {
					t.Errorf("Expected output to contain field key %q, but got: %s", tt.fields[0].Key, output)
				}
			}
		})
	}
}

func TestStructuredLogger_With(t *testing.T) {
	t.Parallel()

	// Capture log output
	var buf bytes.Buffer
	logger := logrus.New()
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.JSONFormatter{})

	structLogger := &StructuredLogger{
		logger: logger,
		fields: make(logrus.Fields),
	}

	// Add contextual fields
	contextLogger := structLogger.With(
		domain.Field{Key: "component", Value: "test"},
		domain.Field{Key: "user_id", Value: "123"},
	)

	// Log a message
	contextLogger.Info("test message")

	output := buf.String()

	// Verify contextual fields are included
	if !strings.Contains(output, "component") {
		t.Errorf("Expected output to contain 'component' field, but got: %s", output)
	}

	if !strings.Contains(output, "user_id") {
		t.Errorf("Expected output to contain 'user_id' field, but got: %s", output)
	}

	if !strings.Contains(output, "test message") {
		t.Errorf("Expected output to contain message, but got: %s", output)
	}
}

func TestStructuredLogger_WithChaining(t *testing.T) {
	t.Parallel()

	// Capture log output
	var buf bytes.Buffer
	logger := logrus.New()
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.JSONFormatter{})

	structLogger := &StructuredLogger{
		logger: logger,
		fields: make(logrus.Fields),
	}

	// Chain With calls
	chainedLogger := structLogger.
		With(domain.Field{Key: "component", Value: "auth"}).
		With(domain.Field{Key: "user_id", Value: "456"}).
		With(domain.Field{Key: "request_id", Value: "req-789"})

	chainedLogger.Info("chained logging test")

	output := buf.String()

	// Verify all chained fields are included
	expectedFields := []string{"component", "user_id", "request_id"}
	for _, field := range expectedFields {
		if !strings.Contains(output, field) {
			t.Errorf("Expected output to contain %q field, but got: %s", field, output)
		}
	}
}

func TestFieldHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		helper   func() domain.Field
		expected domain.Field
	}{
		{
			name: "String helper",
			helper: func() domain.Field {
				return String("key", "value")
			},
			expected: domain.Field{Key: "key", Value: "value"},
		},
		{
			name: "Int helper",
			helper: func() domain.Field {
				return Int("count", 42)
			},
			expected: domain.Field{Key: "count", Value: 42},
		},
		{
			name: "Duration helper",
			helper: func() domain.Field {
				return Duration("elapsed", "1.5s")
			},
			expected: domain.Field{Key: "elapsed", Value: "1.5s"},
		},
		{
			name: "UserID helper",
			helper: func() domain.Field {
				return UserID("user123")
			},
			expected: domain.Field{Key: "user_id", Value: "user123"},
		},
		{
			name: "RequestID helper",
			helper: func() domain.Field {
				return RequestID("req456")
			},
			expected: domain.Field{Key: "request_id", Value: "req456"},
		},
		{
			name: "Path helper",
			helper: func() domain.Field {
				return Path("/api/v1/query")
			},
			expected: domain.Field{Key: "path", Value: "/api/v1/query"},
		},
		{
			name: "Method helper",
			helper: func() domain.Field {
				return Method("POST")
			},
			expected: domain.Field{Key: "method", Value: "POST"},
		},
		{
			name: "StatusCode helper",
			helper: func() domain.Field {
				return StatusCode(200)
			},
			expected: domain.Field{Key: "status_code", Value: 200},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tt.helper()
			if result.Key != tt.expected.Key {
				t.Errorf("Expected key %q, got %q", tt.expected.Key, result.Key)
			}
			if result.Value != tt.expected.Value {
				t.Errorf("Expected value %v, got %v", tt.expected.Value, result.Value)
			}
		})
	}
}

func TestErrorHelper(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected domain.Field
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: domain.Field{Key: "error", Value: nil},
		},
		{
			name:     "non-nil error",
			err:      domain.ErrUnauthorized,
			expected: domain.Field{Key: "error", Value: domain.ErrUnauthorized.Error()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := Error(tt.err)
			if result.Key != tt.expected.Key {
				t.Errorf("Expected key %q, got %q", tt.expected.Key, result.Key)
			}
			if result.Value != tt.expected.Value {
				t.Errorf("Expected value %v, got %v", tt.expected.Value, result.Value)
			}
		})
	}
}

func TestEnhancedStructuredLogger_ContextMethods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		expectedField string
		expectedValue string
	}{
		{
			name:          "WithComponent",
			expectedField: "component",
			expectedValue: "auth",
		},
		{
			name:          "WithRequestID",
			expectedField: "request_id",
			expectedValue: "req-123",
		},
		{
			name:          "WithUser",
			expectedField: "user_id",
			expectedValue: "user456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			
			// Each subtest gets its own buffer to avoid race conditions
			var testBuf bytes.Buffer
			testLogger := logrus.New()
			testLogger.SetOutput(&testBuf)
			testLogger.SetFormatter(&logrus.JSONFormatter{})
			
			testBaseLogger := &StructuredLogger{
				logger: testLogger,
				fields: make(logrus.Fields),
			}
			testEnhancedLogger := &EnhancedStructuredLogger{Logger: testBaseLogger}
			
			// Execute setup function with test-specific logger
			var contextualLogger domain.Logger
			switch tt.name {
			case "WithRequestID":
				contextualLogger = testEnhancedLogger.WithRequestID("req-123")
			case "WithUser":
				contextualLogger = testEnhancedLogger.WithUser("user456")
			case "WithComponent":
				contextualLogger = testEnhancedLogger.WithComponent("auth")
			}
			
			contextualLogger.Info("test message")

			output := testBuf.String()

			if !strings.Contains(output, tt.expectedField) {
				t.Errorf("Expected output to contain field %q, but got: %s", tt.expectedField, output)
			}

			if !strings.Contains(output, tt.expectedValue) {
				t.Errorf("Expected output to contain value %q, but got: %s", tt.expectedValue, output)
			}
		})
	}
}

//nolint:gocognit // Test function with comprehensive test cases
func TestEnhancedStructuredLogger_WithTenant(t *testing.T) {
	t.Parallel()

	// Capture log output
	var buf bytes.Buffer
	logger := logrus.New()
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.JSONFormatter{})

	tests := []struct {
		name           string
		accountID      string
		projectID      string
		expectsAccount bool
		expectsProject bool
	}{
		{
			name:           "with both account and project",
			accountID:      "acc123",
			projectID:      "proj456",
			expectsAccount: true,
			expectsProject: true,
		},
		{
			name:           "with account only",
			accountID:      "acc789",
			projectID:      "",
			expectsAccount: true,
			expectsProject: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			
			// Each subtest gets its own buffer to avoid race conditions
			var testBuf bytes.Buffer
			testLogger := logrus.New()
			testLogger.SetOutput(&testBuf)
			testLogger.SetFormatter(&logrus.JSONFormatter{})
			
			testBaseLogger := &StructuredLogger{
				logger: testLogger,
				fields: make(logrus.Fields),
			}
			testEnhancedLogger := &EnhancedStructuredLogger{Logger: testBaseLogger}
			
			tenantLogger := testEnhancedLogger.WithTenant(tt.accountID, tt.projectID)
			tenantLogger.Info("tenant test")

			output := testBuf.String()

			if tt.expectsAccount {
				if !strings.Contains(output, "tenant_account") {
					t.Errorf("Expected output to contain 'tenant_account' field, but got: %s", output)
				}
				if !strings.Contains(output, tt.accountID) {
					t.Errorf("Expected output to contain account ID %q, but got: %s", tt.accountID, output)
				}
			}

			if tt.expectsProject {
				if !strings.Contains(output, "tenant_project") {
					t.Errorf("Expected output to contain 'tenant_project' field, but got: %s", output)
				}
				if !strings.Contains(output, tt.projectID) {
					t.Errorf("Expected output to contain project ID %q, but got: %s", tt.projectID, output)
				}
			} else if tt.projectID == "" {
				// Should not contain project field when empty
				if strings.Contains(output, "tenant_project") {
					t.Errorf("Expected output to NOT contain 'tenant_project' field for empty project, but got: %s", output)
				}
			}
		})
	}
}
