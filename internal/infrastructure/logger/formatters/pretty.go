package formatters

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

// ANSI color codes.
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorGray   = "\033[90m"
	ColorBold   = "\033[1m"
	ColorCyan   = "\033[36m"
)

// PrettyFormatter formats logs in a human-readable format with colors.
type PrettyFormatter struct {
	TimestampFormat string
	UseColors       bool
}

// NewPrettyFormatter creates a new Pretty formatter.
func NewPrettyFormatter() *PrettyFormatter {
	return &PrettyFormatter{
		TimestampFormat: "2006-01-02 15:04:05",
		UseColors:       supportsColor(),
	}
}

// Format formats the log entry in pretty format.
func (f *PrettyFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	var b bytes.Buffer

	// Format timestamp
	timestamp := entry.Time.Format(f.TimestampFormat)
	b.WriteString(f.colorize(ColorGray, timestamp))
	b.WriteByte(' ')

	// Format level with color and padding
	levelStr := f.formatLevel(entry.Level)
	b.WriteString(levelStr)
	b.WriteByte(' ')

	// Format message with proper padding
	message := f.padMessage(entry.Message)
	b.WriteString(f.colorize(ColorBold, message))

	// Add fields if present
	if len(entry.Data) > 0 {
		fieldsStr := f.formatFields(entry.Data)
		b.WriteString(fieldsStr)
	}

	b.WriteByte('\n')
	return b.Bytes(), nil
}

// formatLevel formats the log level with color and padding.
func (f *PrettyFormatter) formatLevel(level logrus.Level) string {
	levelStr := strings.ToUpper(level.String())
	levelStr = fmt.Sprintf("%-5s", levelStr) // Pad to 5 chars

	if !f.UseColors {
		return levelStr
	}

	switch level {
	case logrus.ErrorLevel:
		return f.colorize(ColorRed, levelStr)
	case logrus.WarnLevel:
		return f.colorize(ColorYellow, levelStr)
	case logrus.InfoLevel:
		return f.colorize(ColorBlue, levelStr)
	case logrus.DebugLevel:
		return f.colorize(ColorGray, levelStr)
	case logrus.PanicLevel:
		return f.colorize(ColorRed, levelStr)
	case logrus.FatalLevel:
		return f.colorize(ColorRed, levelStr)
	case logrus.TraceLevel:
		return f.colorize(ColorGray, levelStr)
	default:
		return levelStr
	}
}

// padMessage pads the message for consistent formatting.
func (f *PrettyFormatter) padMessage(message string) string {
	const maxWidth = 35
	if len(message) > maxWidth {
		return message
	}
	return fmt.Sprintf("%-*s", maxWidth, message)
}

// formatFields formats the data fields in a readable way.
func (f *PrettyFormatter) formatFields(fields logrus.Fields) string {
	if len(fields) == 0 {
		return ""
	}

	var parts []string

	// Extract important fields first
	importantFields := []string{"user_id", "request_id", "component", "error", "status", "duration"}

	// Add important fields first
	for _, key := range importantFields {
		if value, exists := fields[key]; exists {
			parts = append(parts, fmt.Sprintf("%s=%s",
				f.colorize(ColorCyan, key),
				f.formatFieldValue(value)))
			delete(fields, key)
		}
	}

	// Add remaining fields
	for key, value := range fields {
		parts = append(parts, fmt.Sprintf("%s=%s",
			f.colorize(ColorCyan, key),
			f.formatFieldValue(value)))
	}

	if len(parts) > 0 {
		return " " + strings.Join(parts, " ")
	}
	return ""
}

// formatFieldValue formats a field value for display.
func (f *PrettyFormatter) formatFieldValue(value interface{}) string {
	if value == nil {
		return f.colorize(ColorGray, "<nil>")
	}

	str := fmt.Sprintf("%v", value)

	// Add quotes for strings with spaces
	if strings.Contains(str, " ") {
		return fmt.Sprintf(`"%s"`, str)
	}

	return str
}

// colorize applies color to text if colors are enabled.
func (f *PrettyFormatter) colorize(color, text string) string {
	if !f.UseColors {
		return text
	}
	return color + text + ColorReset
}

// supportsColor detects if the terminal supports colors.
func supportsColor() bool {
	// Check if output is a terminal
	if fileInfo, err := os.Stdout.Stat(); err != nil || (fileInfo.Mode()&os.ModeCharDevice) == 0 {
		return false
	}

	// Check environment variables
	term := os.Getenv("TERM")
	colorTerm := os.Getenv("COLORTERM")

	return term != "dumb" && (colorTerm != "" ||
		strings.Contains(term, "color") ||
		strings.Contains(term, "256") ||
		term == "xterm" ||
		term == "screen")
}
