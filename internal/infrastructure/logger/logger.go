package logger

import (
	"maps"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/infrastructure/logger/formatters"
)

// StructuredLogger wraps logrus to implement domain.Logger interface.
type StructuredLogger struct {
	logger *logrus.Logger
	fields logrus.Fields
}

// NewStructuredLogger creates a new structured logger with enhanced format support.
func NewStructuredLogger(level, format string) domain.Logger {
	logger := logrus.New()

	// Set log level
	switch level {
	case "debug":
		logger.SetLevel(logrus.DebugLevel)
	case "info":
		logger.SetLevel(logrus.InfoLevel)
	case "warn":
		logger.SetLevel(logrus.WarnLevel)
	case "error":
		logger.SetLevel(logrus.ErrorLevel)
	default:
		logger.SetLevel(logrus.InfoLevel)
	}

	// Set formatter based on format
	switch format {
	case "json":
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z",
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyTime:  "time",
				logrus.FieldKeyLevel: "level",
				logrus.FieldKeyMsg:   "msg",
			},
		})
	case "logfmt":
		logger.SetFormatter(formatters.NewLogFmtFormatter())
	case "pretty":
		logger.SetFormatter(formatters.NewPrettyFormatter())
	case "console":
		logger.SetFormatter(formatters.NewConsoleFormatter())
	default:
		// Default to JSON
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z",
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyTime:  "time",
				logrus.FieldKeyLevel: "level",
				logrus.FieldKeyMsg:   "msg",
			},
		})
	}

	return &StructuredLogger{
		logger: logger,
		fields: make(logrus.Fields),
	}
}

func (l *StructuredLogger) Debug(msg string, fields ...domain.Field) {
	l.logWithFields(logrus.DebugLevel, msg, fields...)
}

func (l *StructuredLogger) Info(msg string, fields ...domain.Field) {
	l.logWithFields(logrus.InfoLevel, msg, fields...)
}

func (l *StructuredLogger) Warn(msg string, fields ...domain.Field) {
	l.logWithFields(logrus.WarnLevel, msg, fields...)
}

func (l *StructuredLogger) Error(msg string, fields ...domain.Field) {
	l.logWithFields(logrus.ErrorLevel, msg, fields...)
}

func (l *StructuredLogger) With(fields ...domain.Field) domain.Logger {
	newFields := make(logrus.Fields)
	// Copy existing fields
	maps.Copy(newFields, l.fields)
	// Add new fields
	for _, field := range fields {
		newFields[field.Key] = field.Value
	}

	return &StructuredLogger{
		logger: l.logger,
		fields: newFields,
	}
}

func (l *StructuredLogger) logWithFields(level logrus.Level, msg string, fields ...domain.Field) {
	entry := l.logger.WithFields(l.fields)

	// Add fields from parameters with sanitization
	for _, field := range fields {
		entry = entry.WithField(field.Key, sanitizeLogValue(field.Value))
	}

	entry.Log(level, msg)
}

// sanitizeLogValue removes sensitive information from log values.
func sanitizeLogValue(value interface{}) interface{} {
	if str, ok := value.(string); ok {
		return sanitizeString(str)
	}
	return value
}

// sensitivePatterns contains regex patterns for sensitive data.
//
//nolint:gochecknoglobals // Regex patterns are immutable and safe as globals
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9\-_]+\.[a-zA-Z0-9\-_]+\.[a-zA-Z0-9\-_]+`), // JWT tokens
	regexp.MustCompile(`(?i)authorization:\s*bearer\s+[^\s]+`),                           // Auth headers
	regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|key)=[^\s&]+`),             // URL params
	regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|secret[_-]?key):\s*[^\s,}]+`),  // JSON/YAML values
}

// sanitizeString removes sensitive information from strings.
func sanitizeString(s string) string {
	result := s
	for _, pattern := range sensitivePatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			// Keep the field name but mask the value
			if idx := strings.Index(match, "="); idx != -1 {
				return match[:idx+1] + "[REDACTED]"
			}
			if idx := strings.Index(match, ":"); idx != -1 {
				return match[:idx+1] + " [REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	return result
}

// String field helpers for common patterns.
func String(key, value string) domain.Field {
	return domain.Field{Key: key, Value: value}
}

func Int(key string, value int) domain.Field {
	return domain.Field{Key: key, Value: value}
}

func Duration(key string, value any) domain.Field {
	return domain.Field{Key: key, Value: value}
}

func Error(err error) domain.Field {
	if err == nil {
		return domain.Field{Key: "error", Value: nil}
	}

	return domain.Field{Key: "error", Value: err.Error()}
}

func UserID(id string) domain.Field {
	return domain.Field{Key: "user_id", Value: id}
}

func RequestID(id string) domain.Field {
	return domain.Field{Key: "request_id", Value: id}
}

func Path(path string) domain.Field {
	return domain.Field{Key: "path", Value: path}
}

func Method(method string) domain.Field {
	return domain.Field{Key: "method", Value: method}
}

func StatusCode(code int) domain.Field {
	return domain.Field{Key: "status_code", Value: code}
}

// EnhancedStructuredLogger provides contextual logging capabilities.
type EnhancedStructuredLogger struct {
	domain.Logger
}

// NewEnhancedStructuredLogger creates a logger with contextual capabilities.
func NewEnhancedStructuredLogger(level, format string) *EnhancedStructuredLogger {
	baseLogger := NewStructuredLogger(level, format)
	return &EnhancedStructuredLogger{
		Logger: baseLogger,
	}
}

// WithComponent creates a logger with pre-configured component context.
func (e *EnhancedStructuredLogger) WithComponent(component string) domain.Logger {
	return e.Logger.With(domain.Field{Key: "component", Value: component})
}

// WithRequestID creates a logger with request ID context.
func (e *EnhancedStructuredLogger) WithRequestID(requestID string) domain.Logger {
	return e.Logger.With(domain.Field{Key: "request_id", Value: requestID})
}

// WithUser creates a logger with user context.
func (e *EnhancedStructuredLogger) WithUser(userID string) domain.Logger {
	return e.Logger.With(domain.Field{Key: "user_id", Value: userID})
}

// WithTenant creates a logger with tenant context.
func (e *EnhancedStructuredLogger) WithTenant(accountID, projectID string) domain.Logger {
	fields := []domain.Field{
		{Key: "tenant_account", Value: accountID},
	}

	if projectID != "" {
		fields = append(fields, domain.Field{Key: "tenant_project", Value: projectID})
	}

	return e.Logger.With(fields...)
}
