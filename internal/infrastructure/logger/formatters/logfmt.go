package formatters

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/sirupsen/logrus"
)

// LogFmtFormatter formats logs in logfmt format (key=value pairs).
type LogFmtFormatter struct {
	TimestampFormat string
}

// NewLogFmtFormatter creates a new LogFmt formatter.
func NewLogFmtFormatter() *LogFmtFormatter {
	return &LogFmtFormatter{
		TimestampFormat: "2006-01-02T15:04:05.000Z",
	}
}

// Format formats the log entry in logfmt format.
func (f *LogFmtFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	var b bytes.Buffer

	// Add standard fields
	f.appendKeyValue(&b, "level", entry.Level.String())
	f.appendKeyValue(&b, "time", entry.Time.UTC().Format(f.TimestampFormat))
	f.appendKeyValue(&b, "msg", entry.Message)

	// Add data fields in sorted order for consistency
	keys := make([]string, 0, len(entry.Data))
	for k := range entry.Data {
		keys = append(keys, k)
	}

	// Sort keys for consistent output
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	for _, key := range keys {
		f.appendKeyValue(&b, key, entry.Data[key])
	}

	b.WriteByte('\n')
	return b.Bytes(), nil
}

// appendKeyValue appends a key=value pair to the buffer.
func (f *LogFmtFormatter) appendKeyValue(b *bytes.Buffer, key string, value interface{}) {
	if b.Len() > 0 {
		b.WriteByte(' ')
	}

	b.WriteString(key)
	b.WriteByte('=')

	valueStr := f.formatValue(value)
	b.WriteString(valueStr)
}

// formatValue formats a value for logfmt output, quoting if necessary.
func (f *LogFmtFormatter) formatValue(value interface{}) string {
	if value == nil {
		return "<nil>"
	}

	str := fmt.Sprintf("%v", value)

	// Quote strings that need quoting
	if f.needsQuoting(str) {
		return strconv.Quote(str)
	}

	return str
}

// needsQuoting determines if a string needs to be quoted.
func (f *LogFmtFormatter) needsQuoting(s string) bool {
	if s == "" {
		return true
	}

	for _, r := range s {
		if r == ' ' || r == '=' || r == '"' || r == '\t' || r == '\n' || r == '\r' {
			return true
		}
	}

	return false
}
