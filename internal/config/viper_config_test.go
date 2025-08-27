package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config"
)

func TestLoadViperConfig_DefaultValues(t *testing.T) {
	// Create empty config file to test defaults
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "empty.yaml")

	// Write minimal config with only required fields
	content := `
upstream:
  url: "https://test.example.com"
auth:
  jwt:
    jwksUrl: "https://auth.example.com/.well-known/jwks.json"
`
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))

	viperConfig, err := config.LoadViperConfig(configPath)
	require.NoError(t, err)

	// Test server defaults
	assert.Equal(t, "0.0.0.0:8080", viperConfig.Server.Address)
	assert.Equal(t, 30*time.Second, viperConfig.Server.Timeouts.ReadTimeout)
	assert.Equal(t, 30*time.Second, viperConfig.Server.Timeouts.WriteTimeout)
	assert.Equal(t, 60*time.Second, viperConfig.Server.Timeouts.IdleTimeout)

	// Test upstream defaults
	assert.Equal(t, "https://test.example.com", viperConfig.Upstream.URL)
	assert.Equal(t, 30*time.Second, viperConfig.Upstream.Timeout)
	assert.Equal(t, 3, viperConfig.Upstream.Retry.MaxRetries)
	assert.Equal(t, 1*time.Second, viperConfig.Upstream.Retry.RetryDelay)

	// Test auth defaults
	assert.Equal(t, "RS256", viperConfig.Auth.JWT.Algorithm)
	assert.Equal(t, "https://auth.example.com/.well-known/jwks.json", viperConfig.Auth.JWT.JwksURL)
	assert.False(t, viperConfig.Auth.JWT.Validation.ValidateAudience)
	assert.False(t, viperConfig.Auth.JWT.Validation.ValidateIssuer)
	assert.Equal(t, "groups", viperConfig.Auth.JWT.Claims.UserGroupsClaim)
	assert.Equal(t, 1*time.Hour, viperConfig.Auth.JWT.TokenTTL)
	assert.Equal(t, 5*time.Minute, viperConfig.Auth.JWT.CacheTTL)

	// Test tenant filter defaults
	assert.Equal(t, "orConditions", viperConfig.TenantFilter.Strategy)
	assert.Equal(t, "vm_account_id", viperConfig.TenantFilter.Labels.AccountLabel)
	assert.Equal(t, "vm_project_id", viperConfig.TenantFilter.Labels.ProjectLabel)
	assert.True(t, viperConfig.TenantFilter.Labels.UseProjectID)

	// Test metrics defaults
	assert.True(t, viperConfig.Metrics.Enabled)
	assert.Equal(t, "/metrics", viperConfig.Metrics.Path)

	// Test logging defaults
	assert.Equal(t, "info", viperConfig.Logging.Level)
	assert.Equal(t, "json", viperConfig.Logging.Format)
}

func TestLoadViperConfig_EnvironmentOverrides(t *testing.T) {
	// Set environment variables
	envVars := map[string]string{
		"VM_PROXY_AUTH_SERVER_ADDRESS":        "127.0.0.1:9090",
		"VM_PROXY_AUTH_UPSTREAM_URL":          "https://upstream.test.com",
		"VM_PROXY_AUTH_AUTH_JWT_ALGORITHM":    "HS256",
		"VM_PROXY_AUTH_AUTH_JWT_SECRET":       "test-secret-key",
		"VM_PROXY_AUTH_TENANTFILTER_STRATEGY": "regex",
		"VM_PROXY_AUTH_LOGGING_LEVEL":         "debug",
	}

	for key, value := range envVars {
		t.Setenv(key, value)
	}

	// Don't use config file, just use environment variables
	viperConfig, err := config.LoadViperConfig("")
	require.NoError(t, err)

	// Test environment overrides
	assert.Equal(t, "127.0.0.1:9090", viperConfig.Server.Address)
	assert.Equal(t, "https://upstream.test.com", viperConfig.Upstream.URL)
	assert.Equal(t, "HS256", viperConfig.Auth.JWT.Algorithm)
	assert.Equal(t, "test-secret-key", viperConfig.Auth.JWT.Secret)
	assert.Equal(t, "regex", viperConfig.TenantFilter.Strategy)
	assert.Equal(t, "debug", viperConfig.Logging.Level)
}

func TestLoadViperConfig_FullConfiguration(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "full.yaml")

	content := `
server:
  address: "0.0.0.0:8080"
  timeouts:
    readTimeout: "45s"
    writeTimeout: "45s"
    idleTimeout: "90s"

upstream:
  url: "https://vmselect.example.com"
  timeout: "60s"
  retry:
    maxRetries: 5
    retryDelay: "2s"

auth:
  jwt:
    algorithm: "RS256"
    jwksUrl: "https://keycloak.example.com/.well-known/jwks.json"
    validation:
      validateAudience: true
      validateIssuer: true
      requiredIssuer: "https://keycloak.example.com/realms/staff"
      requiredAudience: ["vm-proxy-auth"]
    claims:
      userGroupsClaim: "realm_access.roles"
    tokenTtl: "2h"
    cacheTtl: "10m"

tenantMapping:
  - groups: ["developers"]
    vmTenants:
      - accountId: "1000"
        projectId: "10"
    readOnly: false
  - groups: ["viewers"]
    vmTenants:
      - accountId: "2000"
    readOnly: true

tenantFilter:
  strategy: "orConditions"
  labels:
    accountLabel: "tenant_id"
    projectLabel: "project_id"
    useProjectId: false

metrics:
  enabled: false
  path: "/custom/metrics"

logging:
  level: "debug"
  format: "text"
`
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))

	viperConfig, err := config.LoadViperConfig(configPath)
	require.NoError(t, err)

	// Test server configuration
	assert.Equal(t, "0.0.0.0:8080", viperConfig.Server.Address)
	assert.Equal(t, 45*time.Second, viperConfig.Server.Timeouts.ReadTimeout)
	assert.Equal(t, 45*time.Second, viperConfig.Server.Timeouts.WriteTimeout)
	assert.Equal(t, 90*time.Second, viperConfig.Server.Timeouts.IdleTimeout)

	// Test upstream configuration
	assert.Equal(t, "https://vmselect.example.com", viperConfig.Upstream.URL)
	assert.Equal(t, 60*time.Second, viperConfig.Upstream.Timeout)
	assert.Equal(t, 5, viperConfig.Upstream.Retry.MaxRetries)
	assert.Equal(t, 2*time.Second, viperConfig.Upstream.Retry.RetryDelay)

	// Test auth configuration
	assert.Equal(t, "RS256", viperConfig.Auth.JWT.Algorithm)
	assert.Equal(t, "https://keycloak.example.com/.well-known/jwks.json", viperConfig.Auth.JWT.JwksURL)
	assert.True(t, viperConfig.Auth.JWT.Validation.ValidateAudience)
	assert.True(t, viperConfig.Auth.JWT.Validation.ValidateIssuer)
	assert.Equal(t, "https://keycloak.example.com/realms/staff", viperConfig.Auth.JWT.Validation.RequiredIssuer)
	assert.Equal(t, []string{"vm-proxy-auth"}, viperConfig.Auth.JWT.Validation.RequiredAudience)
	assert.Equal(t, "realm_access.roles", viperConfig.Auth.JWT.Claims.UserGroupsClaim)
	assert.Equal(t, 2*time.Hour, viperConfig.Auth.JWT.TokenTTL)
	assert.Equal(t, 10*time.Minute, viperConfig.Auth.JWT.CacheTTL)

	// Test tenant mapping
	require.Len(t, viperConfig.TenantMapping, 2)

	firstMapping := viperConfig.TenantMapping[0]
	assert.Equal(t, []string{"developers"}, firstMapping.Groups)
	assert.False(t, firstMapping.ReadOnly)
	require.Len(t, firstMapping.VMTenants, 1)
	assert.Equal(t, "1000", firstMapping.VMTenants[0].AccountID)
	assert.Equal(t, "10", firstMapping.VMTenants[0].ProjectID)

	secondMapping := viperConfig.TenantMapping[1]
	assert.Equal(t, []string{"viewers"}, secondMapping.Groups)
	assert.True(t, secondMapping.ReadOnly)
	require.Len(t, secondMapping.VMTenants, 1)
	assert.Equal(t, "2000", secondMapping.VMTenants[0].AccountID)
	assert.Empty(t, secondMapping.VMTenants[0].ProjectID)

	// Test tenant filter configuration
	assert.Equal(t, "orConditions", viperConfig.TenantFilter.Strategy)
	assert.Equal(t, "tenant_id", viperConfig.TenantFilter.Labels.AccountLabel)
	assert.Equal(t, "project_id", viperConfig.TenantFilter.Labels.ProjectLabel)
	assert.False(t, viperConfig.TenantFilter.Labels.UseProjectID)

	// Test metrics configuration
	assert.False(t, viperConfig.Metrics.Enabled)
	assert.Equal(t, "/custom/metrics", viperConfig.Metrics.Path)

	// Test logging configuration
	assert.Equal(t, "debug", viperConfig.Logging.Level)
	assert.Equal(t, "text", viperConfig.Logging.Format)
}

func TestLoadViperConfig_ValidationErrors(t *testing.T) {
	tests := []struct {
		name          string
		config        string
		expectedError string
	}{
		{
			name: "missing upstream URL",
			config: `
auth:
  jwt:
    jwksUrl: "https://auth.example.com/.well-known/jwks.json"
`,
			expectedError: "upstream.url is required",
		},
		{
			name: "missing auth config",
			config: `
upstream:
  url: "https://test.example.com"
`,
			expectedError: "either auth.jwks_url or auth.jwt_secret must be provided",
		},
		{
			name: "invalid JWT algorithm",
			config: `
upstream:
  url: "https://test.example.com"
auth:
  jwt:
    algorithm: "INVALID"
    jwksUrl: "https://auth.example.com/.well-known/jwks.json"
`,
			expectedError: "unsupported JWT algorithm: INVALID",
		},
		{
			name: "RS256 without JWKS URL",
			config: `
upstream:
  url: "https://test.example.com"
auth:
  jwt:
    algorithm: "RS256"
    secret: "some-secret"
`,
			expectedError: "RS256 algorithm requires jwksUrl",
		},
		{
			name: "HS256 without secret",
			config: `
upstream:
  url: "https://test.example.com"
auth:
  jwt:
    algorithm: "HS256"
    jwksUrl: "https://auth.example.com/.well-known/jwks.json"
`,
			expectedError: "HS256 algorithm requires secret",
		},
		{
			name: "invalid tenant filter strategy",
			config: `
upstream:
  url: "https://test.example.com"
auth:
  jwt:
    jwksUrl: "https://auth.example.com/.well-known/jwks.json"
tenantFilter:
  strategy: "invalid"
`,
			expectedError: "unsupported tenant filter strategy: invalid",
		},
		{
			name: "invalid logging level",
			config: `
upstream:
  url: "https://test.example.com"
auth:
  jwt:
    jwksUrl: "https://auth.example.com/.well-known/jwks.json"
logging:
  level: "invalid"
`,
			expectedError: "unsupported logging level: invalid",
		},
		{
			name: "invalid logging format",
			config: `
upstream:
  url: "https://test.example.com"
auth:
  jwt:
    jwksUrl: "https://auth.example.com/.well-known/jwks.json"
logging:
  format: "invalid"
`,
			expectedError: "unsupported logging format: invalid (supported: json, text, logfmt, pretty, console)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			configPath := filepath.Join(tempDir, "test.yaml")
			require.NoError(t, os.WriteFile(configPath, []byte(tt.config), 0644))

			_, err := config.LoadViperConfig(configPath)
			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), strings.ToLower(tt.expectedError))
		})
	}
}

func TestLoadViperConfig_NoConfigFile(t *testing.T) {
	// Test with non-existent config file but with environment variables
	t.Setenv("VM_PROXY_AUTH_UPSTREAM_URL", "https://test.example.com")
	t.Setenv("VM_PROXY_AUTH_AUTH_JWT_JWKSURL", "https://auth.example.com/.well-known/jwks.json")

	viperConfig, err := config.LoadViperConfig("")
	require.NoError(t, err)

	// Should use environment variables
	assert.Equal(t, "https://test.example.com", viperConfig.Upstream.URL)
	assert.Equal(t, "https://auth.example.com/.well-known/jwks.json", viperConfig.Auth.JWT.JwksURL)

	// Should use defaults for other values
	assert.Equal(t, "0.0.0.0:8080", viperConfig.Server.Address)
	assert.Equal(t, "orConditions", viperConfig.TenantFilter.Strategy)
}

func TestViperConfig_Creation(t *testing.T) {
	viperConfig := &config.ViperConfig{}

	// Test that ViperConfig can be created
	assert.NotNil(t, viperConfig)
}
