package server_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config/modules/server"
)

func TestServerConfig_Validate_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config server.Config
	}{
		{
			name: "valid minimal config",
			config: server.Config{
				Address: "0.0.0.0:8080",
				Timeouts: server.Timeouts{
					Read:  30 * time.Second,
					Write: 30 * time.Second,
					Idle:  60 * time.Second,
				},
			},
		},
		{
			name: "valid with IPv6",
			config: server.Config{
				Address: "[::1]:8080",
				Timeouts: server.Timeouts{
					Read:  10 * time.Second,
					Write: 15 * time.Second,
					Idle:  30 * time.Second,
				},
			},
		},
		{
			name: "valid with hostname",
			config: server.Config{
				Address: "localhost:9000",
				Timeouts: server.Timeouts{
					Read:  5 * time.Second,
					Write: 5 * time.Second,
					Idle:  10 * time.Second,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestServerConfig_Validate_Failure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      server.Config
		expectedErr string
	}{
		{
			name: "missing address",
			config: server.Config{
				Address: "",
				Timeouts: server.Timeouts{
					Read:  30 * time.Second,
					Write: 30 * time.Second,
					Idle:  60 * time.Second,
				},
			},
			expectedErr: "server address is required",
		},
		{
			name: "invalid address format - missing port",
			config: server.Config{
				Address: "localhost",
				Timeouts: server.Timeouts{
					Read:  30 * time.Second,
					Write: 30 * time.Second,
					Idle:  60 * time.Second,
				},
			},
			expectedErr: "invalid server address format",
		},
		{
			name: "invalid address format - malformed",
			config: server.Config{
				Address: "not-an-address",
				Timeouts: server.Timeouts{
					Read:  30 * time.Second,
					Write: 30 * time.Second,
					Idle:  60 * time.Second,
				},
			},
			expectedErr: "invalid server address format",
		},
		{
			name: "zero read timeout",
			config: server.Config{
				Address: "0.0.0.0:8080",
				Timeouts: server.Timeouts{
					Read:  0,
					Write: 30 * time.Second,
					Idle:  60 * time.Second,
				},
			},
			expectedErr: "server read timeout must be positive",
		},
		{
			name: "negative write timeout",
			config: server.Config{
				Address: "0.0.0.0:8080",
				Timeouts: server.Timeouts{
					Read:  30 * time.Second,
					Write: -10 * time.Second,
					Idle:  60 * time.Second,
				},
			},
			expectedErr: "server write timeout must be positive",
		},
		{
			name: "zero idle timeout",
			config: server.Config{
				Address: "0.0.0.0:8080",
				Timeouts: server.Timeouts{
					Read:  30 * time.Second,
					Write: 30 * time.Second,
					Idle:  0,
				},
			},
			expectedErr: "server idle timeout must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestServerConfig_Defaults(t *testing.T) {
	t.Parallel()

	defaults := server.GetDefaults()

	assert.Equal(t, "0.0.0.0:8080", defaults.Address)
	assert.Equal(t, 30*time.Second, defaults.Timeouts.Read)
	assert.Equal(t, 30*time.Second, defaults.Timeouts.Write)
	assert.Equal(t, 60*time.Second, defaults.Timeouts.Idle)
}
