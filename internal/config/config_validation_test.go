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
		"../../examples/config.example.yaml",
		"../../examples/config.rs256.example.yaml",
		"../../examples/config.hs256.example.yaml",
		"../../examples/config.vm-multitenancy.yaml",
		"../../examples/config.test.yaml",
		"../../examples/config.viper.example.yaml",
	}

	for _, configFile := range configFiles {
		t.Run(filepath.Base(configFile), func(t *testing.T) {
			viperConfig, err := config.LoadViperConfig(configFile)
			require.NoError(t, err, "Config %s should be valid", configFile)

			// Basic validation checks
			assert.NotEmpty(t, viperConfig.Upstream.URL, "Upstream URL should not be empty")

			// JWT configuration should be valid
			switch viperConfig.Auth.JWT.Algorithm {
			case "RS256":
				assert.NotEmpty(t, viperConfig.Auth.JWT.JwksURL, "JWKS URL required for RS256")
			case "HS256":
				assert.NotEmpty(t, viperConfig.Auth.JWT.Secret, "Secret required for HS256")
			}

			// Tenant filter strategy should be valid
			assert.Contains(t, []string{"orConditions", "regex"}, viperConfig.TenantFilter.Strategy)

			// Logging configuration should be valid
			assert.Contains(t, []string{"debug", "info", "warn", "error", "fatal"}, viperConfig.Logging.Level)
			assert.Contains(t, []string{"json", "text"}, viperConfig.Logging.Format)
		})
	}
}

func TestViperConfig_StructureConsistency(t *testing.T) {
	// Test that all configs have consistent structure
	configFile := "../../examples/config.example.yaml"
	viperConfig, err := config.LoadViperConfig(configFile)
	require.NoError(t, err)

	// Test nested structures exist
	assert.NotNil(t, viperConfig.Server.Timeouts)
	assert.NotNil(t, viperConfig.Upstream.Retry)
	assert.NotNil(t, viperConfig.Auth.JWT)
	assert.NotNil(t, viperConfig.Auth.JWT.Validation)
	assert.NotNil(t, viperConfig.Auth.JWT.Claims)
	assert.NotNil(t, viperConfig.TenantFilter.Labels)

	// Test that durations are positive
	assert.Positive(t, viperConfig.Server.Timeouts.ReadTimeout)
	assert.Positive(t, viperConfig.Auth.JWT.TokenTTL)
	assert.Positive(t, viperConfig.Auth.JWT.CacheTTL)
}
