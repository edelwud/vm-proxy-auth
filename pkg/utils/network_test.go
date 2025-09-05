package netutils_test

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	netutils "github.com/edelwud/vm-proxy-auth/pkg/utils"
)

func TestGetExternalIP(t *testing.T) {
	t.Parallel()
	ip, err := netutils.GetExternalIP()
	require.NoError(t, err)
	require.NotEmpty(t, ip)

	// Verify it's a valid IP address
	parsedIP := net.ParseIP(ip)
	assert.NotNil(t, parsedIP, "Should return a valid IP address")
}

func TestConvertBindToAdvertiseAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		bindAddr    string
		expectError bool
		checkResult bool
	}{
		{
			name:        "specific_ip_unchanged",
			bindAddr:    "192.168.1.100:8080",
			expectError: false,
			checkResult: true,
		},
		{
			name:        "all_interfaces_converted",
			bindAddr:    "0.0.0.0:8080",
			expectError: false,
			checkResult: true,
		},
		{
			name:        "empty_host_converted",
			bindAddr:    ":8080",
			expectError: false,
			checkResult: true,
		},
		{
			name:        "invalid_address",
			bindAddr:    "invalid",
			expectError: true,
			checkResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := netutils.ConvertBindToAdvertiseAddress(tt.bindAddr)

			if tt.expectError {
				require.Error(t, err)
				assert.Empty(t, result)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, result)

			if tt.checkResult {
				// For specific IPs, result should be unchanged
				if tt.name == "specific_ip_unchanged" {
					assert.Equal(t, tt.bindAddr, result)
				} else {
					// For wildcard binds, should have real IP
					host, port, splitErr := net.SplitHostPort(result)
					require.NoError(t, splitErr)
					assert.NotEqual(t, "0.0.0.0", host)
					assert.NotEmpty(t, host)
					assert.Equal(t, "8080", port)
				}
			}
		})
	}
}
