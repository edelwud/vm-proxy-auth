package logger

import (
	"io"
	"log" //nolint:depguard // Required for hclog.Logger interface compatibility
	"strings"

	"github.com/hashicorp/go-hclog"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// HCLogAdapter adapts our domain.Logger to hclog.Logger interface.
// This enables integration with HashiCorp tools (like Raft) while maintaining
// consistent structured logging throughout the application.
type HCLogAdapter struct {
	logger domain.Logger
	level  hclog.Level
	name   string
}

// NewHCLogAdapter creates a new adapter for domain.Logger to hclog.Logger.
func NewHCLogAdapter(logger domain.Logger) hclog.Logger {
	return &HCLogAdapter{
		logger: logger.With(domain.Field{Key: domain.LogFieldComponent, Value: "hashicorp"}),
		level:  hclog.Info,
		name:   "hashicorp",
	}
}

// Log implements hclog.Logger with proper level mapping.
func (h *HCLogAdapter) Log(level hclog.Level, msg string, args ...interface{}) {
	// Skip if level is below our threshold
	if level < h.level {
		return
	}

	fields := h.convertArgsToFields(args)

	switch level {
	case hclog.Trace:
		h.logger.Debug(msg, fields...)
	case hclog.Debug:
		h.logger.Debug(msg, fields...)
	case hclog.Info:
		h.logger.Info(msg, fields...)
	case hclog.Warn:
		h.logger.Warn(msg, fields...)
	case hclog.Error:
		h.logger.Error(msg, fields...)
	case hclog.NoLevel:
		h.logger.Info(msg, fields...)
	case hclog.Off:
		// Do nothing when logging is turned off
		return
	default:
		h.logger.Info(msg, fields...)
	}
}

// Trace implements hclog.Logger.
func (h *HCLogAdapter) Trace(msg string, args ...interface{}) {
	h.Log(hclog.Trace, msg, args...)
}

// Debug implements hclog.Logger.
func (h *HCLogAdapter) Debug(msg string, args ...interface{}) {
	h.Log(hclog.Debug, msg, args...)
}

// Info implements hclog.Logger.
func (h *HCLogAdapter) Info(msg string, args ...interface{}) {
	h.Log(hclog.Info, msg, args...)
}

// Warn implements hclog.Logger.
func (h *HCLogAdapter) Warn(msg string, args ...interface{}) {
	h.Log(hclog.Warn, msg, args...)
}

// Error implements hclog.Logger.
func (h *HCLogAdapter) Error(msg string, args ...interface{}) {
	h.Log(hclog.Error, msg, args...)
}

// IsTrace implements hclog.Logger.
func (h *HCLogAdapter) IsTrace() bool {
	return h.level <= hclog.Trace
}

// IsDebug implements hclog.Logger.
func (h *HCLogAdapter) IsDebug() bool {
	return h.level <= hclog.Debug
}

// IsInfo implements hclog.Logger.
func (h *HCLogAdapter) IsInfo() bool {
	return h.level <= hclog.Info
}

// IsWarn implements hclog.Logger.
func (h *HCLogAdapter) IsWarn() bool {
	return h.level <= hclog.Warn
}

// IsError implements hclog.Logger.
func (h *HCLogAdapter) IsError() bool {
	return h.level <= hclog.Error
}

// ImpliedArgs implements hclog.Logger.
func (h *HCLogAdapter) ImpliedArgs() []interface{} {
	return nil
}

// With implements hclog.Logger creating a new logger with additional context.
func (h *HCLogAdapter) With(args ...interface{}) hclog.Logger {
	fields := h.convertArgsToFields(args)
	return &HCLogAdapter{
		logger: h.logger.With(fields...),
		level:  h.level,
		name:   h.name,
	}
}

// Name implements hclog.Logger.
func (h *HCLogAdapter) Name() string {
	return h.name
}

// Named implements hclog.Logger creating a named sub-logger.
func (h *HCLogAdapter) Named(name string) hclog.Logger {
	newName := h.name
	if name != "" {
		if h.name != "" {
			newName = h.name + "." + name
		} else {
			newName = name
		}
	}

	return &HCLogAdapter{
		logger: h.logger.With(domain.Field{Key: "subsystem", Value: name}),
		level:  h.level,
		name:   newName,
	}
}

// ResetNamed implements hclog.Logger creating a new logger with reset name.
func (h *HCLogAdapter) ResetNamed(name string) hclog.Logger {
	return &HCLogAdapter{
		logger: h.logger.With(domain.Field{Key: "component", Value: name}),
		level:  h.level,
		name:   name,
	}
}

// SetLevel implements hclog.Logger.
func (h *HCLogAdapter) SetLevel(level hclog.Level) {
	h.level = level
}

// GetLevel implements hclog.Logger.
func (h *HCLogAdapter) GetLevel() hclog.Level {
	return h.level
}

// StandardLogger implements hclog.Logger.
// Returns a standard library logger that writes to our structured logger.
func (h *HCLogAdapter) StandardLogger(_ *hclog.StandardLoggerOptions) *log.Logger {
	writer := &logWriter{logger: h.logger}
	return log.New(writer, "", 0)
}

// StandardWriter implements hclog.Logger.
// Returns a writer that writes to our structured logger.
func (h *HCLogAdapter) StandardWriter(_ *hclog.StandardLoggerOptions) io.Writer {
	return &logWriter{logger: h.logger}
}

// convertArgsToFields converts hclog key-value args to domain.Field slice.
func (h *HCLogAdapter) convertArgsToFields(args []interface{}) []domain.Field {
	fields := make([]domain.Field, 0, len(args)/domain.DefaultFieldsPerKeyValue)
	for i := 0; i < len(args)-1; i += domain.DefaultFieldsPerKeyValue {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		fields = append(fields, domain.Field{Key: key, Value: args[i+1]})
	}
	return fields
}

// logWriter implements io.Writer for StandardLogger/StandardWriter.
// It parses HashiCorp library log messages that contain level prefixes.
type logWriter struct {
	logger domain.Logger
}

// Write implements io.Writer with level prefix and component parsing.
func (w *logWriter) Write(p []byte) (int, error) {
	msg := string(p)
	if len(msg) > 0 && msg[len(msg)-1] == '\n' {
		msg = msg[:len(msg)-1]
	}

	// Parse HashiCorp library log prefixes and components
	level, component, cleanMsg := w.parseLogMessage(msg)

	// Create logger with component if found
	logger := w.logger
	if component != "" {
		logger = w.logger.With(domain.Field{Key: "hashicorp_component", Value: component})
	}

	switch level {
	case "TRACE", "DEBUG":
		logger.Debug(cleanMsg)
	case "INFO":
		logger.Info(cleanMsg)
	case "WARN":
		logger.Warn(cleanMsg)
	case "ERROR", "ERR":
		logger.Error(cleanMsg)
	default:
		logger.Info(msg) // fallback to original message
	}

	return len(p), nil
}

// parseLogMessage extracts log level and component from HashiCorp library messages.
// Handles formats like "[ERR] mdns: Failed to bind" or "[INFO] raft: entering leader state".
func (w *logWriter) parseLogMessage(msg string) (string, string, string) {
	msg = strings.TrimSpace(msg)

	var level, component, cleanMsg string

	// Check for level prefixes
	for _, prefix := range []string{"[TRACE]", "[DEBUG]", "[INFO]", "[WARN]", "[ERROR]", "[ERR]"} {
		if strings.HasPrefix(msg, prefix) {
			level = strings.Trim(prefix, "[]")
			cleanMsg = strings.TrimSpace(msg[len(prefix):])
			break
		}
	}

	// If no prefix found, treat as INFO level
	if level == "" {
		level = "INFO"
		cleanMsg = msg
	}

	// Extract component from message (format: "component: message")
	if colonIndex := strings.Index(cleanMsg, ": "); colonIndex > 0 {
		possibleComponent := cleanMsg[:colonIndex]
		// Check if it looks like a component name (simple word, no spaces)
		if !strings.Contains(possibleComponent, " ") && len(possibleComponent) < domain.DefaultComponentNameMaxLength {
			component = possibleComponent
			cleanMsg = strings.TrimSpace(cleanMsg[colonIndex+domain.DefaultColonSeparatorOffset:])
		}
	}

	return level, component, cleanMsg
}
