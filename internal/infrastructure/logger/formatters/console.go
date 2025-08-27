package formatters

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
)

// ConsoleFormatter formats logs in a simple console format.
type ConsoleFormatter struct{}

// NewConsoleFormatter creates a new Console formatter.
func NewConsoleFormatter() *ConsoleFormatter {
	return &ConsoleFormatter{}
}

// Format formats the log entry in simple console format.
func (f *ConsoleFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	var b bytes.Buffer

	// Format: [LEVEL] message - key: value, key: value
	levelStr := fmt.Sprintf("[%s]", strings.ToUpper(entry.Level.String()))
	b.WriteString(levelStr)
	b.WriteByte(' ')
	b.WriteString(entry.Message)

	// Add important fields inline
	importantFields := f.extractImportantFields(entry.Data)
	if importantFields != "" {
		b.WriteByte(' ')
		b.WriteString(importantFields)
	}

	b.WriteByte('\n')
	return b.Bytes(), nil
}

// extractImportantFields extracts only the most important fields for console display.
func (f *ConsoleFormatter) extractImportantFields(fields logrus.Fields) string {
	var parts []string

	// Important fields that should be shown in console mode
	importantKeys := []string{
		"user_id", "user", "error", "status", "method", "path",
		"duration", "tenant_id", "component", "request_id",
	}

	for _, key := range importantKeys {
		if value, exists := fields[key]; exists && value != nil {
			valueStr := fmt.Sprintf("%v", value)
			if valueStr != "" {
				parts = append(parts, fmt.Sprintf("%s: %s", key, valueStr))
			}
		}
	}

	if len(parts) > 0 {
		return "- " + strings.Join(parts, ", ")
	}

	return ""
}
