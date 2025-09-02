package config_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config"
)

func TestAllViperConfigs_Validation(t *testing.T) {
	configFiles := []string{
		"../../examples/config.basic.yaml",
		"../../examples/config.production.yaml",
		"../../examples/config.kubernetes.yaml",
		"../../examples/config.development.yaml",
		"../../examples/config.high-availability.yaml",
	}

	for _, configFile := range configFiles {
		t.Run(filepath.Base(configFile), func(t *testing.T) {
			viperConfig, err := config.LoadViperConfig(configFile)
			require.NoError(t, err, "Config %s should be valid", configFile)

			// Basic validation checks
			assert.NotEmpty(t, viperConfig.Backends, "At least one backend should be configured")

			// JWT configuration should be valid
			switch viperConfig.Auth.JWT.Algorithm {
			case "RS256":
				assert.NotEmpty(t, viperConfig.Auth.JWT.JwksURL, "JWKS URL required for RS256")
			case "HS256":
				assert.NotEmpty(t, viperConfig.Auth.JWT.Secret, "Secret required for HS256")
			}

			// Tenant filter strategy should be valid
			assert.Contains(t, []string{"orConditions", "andConditions"}, viperConfig.TenantFilter.Strategy)

			// Logging configuration should be valid
			assert.Contains(t, []string{"debug", "info", "warn", "error", "fatal"}, viperConfig.Logging.Level)
			assert.Contains(t, []string{"json", "text", "logfmt", "pretty", "console"}, viperConfig.Logging.Format)
		})
	}
}

func TestViperConfig_StructureConsistency(t *testing.T) {
	// Test that all configs have consistent structure
	configFile := "../../examples/config.basic.yaml"
	viperConfig, err := config.LoadViperConfig(configFile)
	require.NoError(t, err)

	// Test nested structures exist
	require.NotNil(t, viperConfig.Server.Timeouts)
	require.NotNil(t, viperConfig.Auth.JWT)
	require.NotNil(t, viperConfig.Auth.JWT.Validation)
	require.NotNil(t, viperConfig.Auth.JWT.Claims)
	require.NotNil(t, viperConfig.TenantFilter.Labels)

	// Test that durations are positive
	assert.Positive(t, viperConfig.Server.Timeouts.ReadTimeout)
	assert.Positive(t, viperConfig.Auth.JWT.TokenTTL)
	assert.Positive(t, viperConfig.Auth.JWT.CacheTTL)
}
