package testutils

import (
	"sync"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// MockLogger provides a test implementation of the domain.Logger interface.
type MockLogger struct {
	entries []LogEntry
	mu      sync.Mutex
}

// LogEntry represents a single log entry.
type LogEntry struct {
	Level   string
	Message string
	Fields  map[string]interface{}
}

// NewMockLogger creates a new mock logger.
func NewMockLogger() *MockLogger {
	return &MockLogger{}
}

// Debug logs a debug message.
func (ml *MockLogger) Debug(msg string, fields ...domain.Field) {
	ml.log("DEBUG", msg, fields)
}

// Info logs an info message.
func (ml *MockLogger) Info(msg string, fields ...domain.Field) {
	ml.log("INFO", msg, fields)
}

// Warn logs a warning message.
func (ml *MockLogger) Warn(msg string, fields ...domain.Field) {
	ml.log("WARN", msg, fields)
}

// Error logs an error message.
func (ml *MockLogger) Error(msg string, fields ...domain.Field) {
	ml.log("ERROR", msg, fields)
}

// With creates a new logger with additional fields.
func (ml *MockLogger) With(fields ...domain.Field) domain.Logger {
	newLogger := &MockLogger{}

	// Copy existing entries
	ml.mu.Lock()
	newLogger.entries = make([]LogEntry, len(ml.entries))
	copy(newLogger.entries, ml.entries)
	ml.mu.Unlock()

	return newLogger
}

// log records a log entry with the given level, message, and fields.
func (ml *MockLogger) log(level, message string, fields []domain.Field) {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	fieldMap := make(map[string]interface{})
	for _, field := range fields {
		fieldMap[field.Key] = field.Value
	}

	ml.entries = append(ml.entries, LogEntry{
		Level:   level,
		Message: message,
		Fields:  fieldMap,
	})
}

// GetEntries returns all logged entries.
func (ml *MockLogger) GetEntries() []LogEntry {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	entries := make([]LogEntry, len(ml.entries))
	copy(entries, ml.entries)
	return entries
}

// HasEntry checks if an entry with the given level and message exists.
func (ml *MockLogger) HasEntry(level, message string) bool {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	for _, entry := range ml.entries {
		if entry.Level == level && entry.Message == message {
			return true
		}
	}
	return false
}

// HasEntryWithField checks if an entry exists with the given level, message, and field.
func (ml *MockLogger) HasEntryWithField(level, message, fieldKey string, fieldValue interface{}) bool {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	for _, entry := range ml.entries {
		if entry.Level == level && entry.Message == message {
			if value, exists := entry.Fields[fieldKey]; exists && value == fieldValue {
				return true
			}
		}
	}
	return false
}

// Clear removes all logged entries.
func (ml *MockLogger) Clear() {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	ml.entries = nil
}

// EntryCount returns the number of logged entries.
func (ml *MockLogger) EntryCount() int {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	return len(ml.entries)
}

// GetEntriesByLevel returns all entries for a specific level.
func (ml *MockLogger) GetEntriesByLevel(level string) []LogEntry {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	var result []LogEntry
	for _, entry := range ml.entries {
		if entry.Level == level {
			result = append(result, entry)
		}
	}
	return result
}
