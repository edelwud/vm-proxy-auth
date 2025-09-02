package logger_test

import (
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/infrastructure/logger"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestHCLogAdapter_LevelParsing(_ *testing.T) {
	mockLogger := testutils.NewMockLogger()
	adapter := logger.NewHCLogAdapter(mockLogger)

	// Test different log levels through the adapter
	adapter.Info("Test info message")
	adapter.Debug("Test debug message")
	adapter.Warn("Test warning message")
	adapter.Error("Test error message")
	adapter.Trace("Test trace message")
}

func TestHCLogAdapter_WithFields(_ *testing.T) {
	mockLogger := testutils.NewMockLogger()
	adapter := logger.NewHCLogAdapter(mockLogger)

	// Test structured logging with fields
	childAdapter := adapter.With("key1", "value1", "key2", 42)
	childAdapter.Info("Message with context")

	// Test named logger
	namedAdapter := adapter.Named("subsystem")
	namedAdapter.Info("Named logger message")
}

func TestHCLogAdapter_StandardLogger(t *testing.T) {
	mockLogger := testutils.NewMockLogger()
	adapter := logger.NewHCLogAdapter(mockLogger)

	// Test standard logger creation
	stdLogger := adapter.StandardLogger(nil)
	require.NotNil(t, stdLogger)

	// Test writing through standard logger (simulates HashiCorp library behavior)
	stdLogger.Print("[INFO] Standard logger info message")
	stdLogger.Print("[ERR] Standard logger error message")
	stdLogger.Print("[WARN] Standard logger warning message")
}

func TestLogWriter_ParseLogLevel(t *testing.T) {
	mockLogger := testutils.NewMockLogger()
	adapter := logger.NewHCLogAdapter(mockLogger)
	writer := adapter.StandardWriter(nil)

	// Test that we can write to the writer (tests the internal parsing)
	tests := []struct {
		name  string
		input string
	}{
		{"error prefix", "[ERR] mdns: Failed to bind to udp6 port"},
		{"info prefix", "[INFO] mdns: Service started successfully"},
		{"warn prefix", "[WARN] mdns: Connection timeout"},
		{"debug prefix", "[DEBUG] mdns: Processing query"},
		{"no prefix", "Plain message without prefix"},
		{"empty message", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := writer.Write([]byte(tt.input))
			assert.NoError(t, err)
		})
	}
}

func TestLogWriter_ParseLogMessage(t *testing.T) {
	mockLogger := testutils.NewMockLogger()
	adapter := logger.NewHCLogAdapter(mockLogger)
	writer := adapter.StandardWriter(nil)

	// Test that we can write complex messages to the writer
	tests := []struct {
		name  string
		input string
	}{
		{"error with mdns component", "[ERR] mdns: Failed to bind to udp6 port"},
		{"info with raft component", "[INFO] raft: entering leader state"},
		{"warn with consul component", "[WARN] consul: Connection lost"},
		{"message without component", "[INFO] Simple message without component"},
		{"message without prefix but with component", "vault: Token expired"},
		{"complex message with colon in content", "[ERR] mdns: Failed to connect to 192.168.1.1:5353"},
		{"invalid component format", "[INFO] too long component name here: message"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := writer.Write([]byte(tt.input))
			assert.NoError(t, err)
		})
	}
}

func TestHCLogAdapter_LevelThresholds(t *testing.T) {
	mockLogger := testutils.NewMockLogger()
	adapter := logger.NewHCLogAdapter(mockLogger)
	adapter.SetLevel(hclog.Warn) // Set threshold to WARN

	// Test level checking methods
	assert.False(t, adapter.IsTrace())
	assert.False(t, adapter.IsDebug())
	assert.False(t, adapter.IsInfo())
	assert.True(t, adapter.IsWarn())
	assert.True(t, adapter.IsError())

	// Test level setting
	adapter.SetLevel(hclog.Debug)
	assert.True(t, adapter.IsDebug())
	assert.Equal(t, hclog.Debug, adapter.GetLevel())
}
