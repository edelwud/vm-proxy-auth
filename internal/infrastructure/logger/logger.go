package logger

import (
	"github.com/sirupsen/logrus"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// StructuredLogger wraps logrus to implement domain.Logger interface.
type StructuredLogger struct {
	logger *logrus.Logger
	fields logrus.Fields
}

// NewStructuredLogger creates a new structured logger.
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

	// Set format
	if format == "json" {
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyTime:  "timestamp",
				logrus.FieldKeyLevel: "level",
				logrus.FieldKeyMsg:   "message",
			},
		})
	} else {
		logger.SetFormatter(&logrus.TextFormatter{
			TimestampFormat: "2006-01-02 15:04:05",
			FullTimestamp:   true,
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
	for k, v := range l.fields {
		newFields[k] = v
	}
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

	// Add fields from parameters
	for _, field := range fields {
		entry = entry.WithField(field.Key, field.Value)
	}

	entry.Log(level, msg)
}

// Field helpers for common patterns.
func String(key, value string) domain.Field {
	return domain.Field{Key: key, Value: value}
}

func Int(key string, value int) domain.Field {
	return domain.Field{Key: key, Value: value}
}

func Duration(key string, value interface{}) domain.Field {
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
