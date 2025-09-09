package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config"
)

func TestLoadConfig_Success(t *testing.T) {
	t.Parallel()

	// Create a temporary config file
	configContent := `
metadata:
  nodeId: "test-node"
  role: "gateway"
  environment: "test"
  region: "local"

logging:
  level: "info"
  format: "json"

metrics:
  enabled: true
  path: "/metrics"

server:
  address: "0.0.0.0:8080"
  timeouts:
    read: "30s"
    write: "30s"
    idle: "60s"

proxy:
  upstreams:
    - url: "http://localhost:8428"
      weight: 1
  routing:
    strategy: "round-robin"
    healthCheck:
      interval: "30s"
      timeout: "10s"
      endpoint: "/health"
      healthyThreshold: 2
      unhealthyThreshold: 3
  reliability:
    timeout: "30s"
    retries: 3
    backoff: "100ms"
    queue:
      enabled: false
      maxSize: 1000
      timeout: "5s"

auth:
  jwt:
    algorithm: "HS256"
    secret: "test-secret-key-32-chars-long"
    cache:
      tokenTtl: "15m"
      jwksTtl: "1h"
    validation:
      requiredClaims: ["sub", "iat", "exp"]
      skipExpiration: false
    claims:
      userIdClaim: "sub"
      emailClaim: "email"
      groupsClaim: "groups"

tenants:
  filter:
    strategy: "orConditions"
    labels:
      account: "vm_account_id"
      project: "vm_project_id"
      useProjectId: false
  mappings:
    - groups: ["admin"]
      vmTenants:
        - account: "0"
          project: ""
      permissions: ["read", "write"]

storage:
  type: "local"
`

	tmpFile := createTempConfigFile(t, configContent)
	defer os.Remove(tmpFile)

	cfg, err := config.LoadConfig(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Validate loaded configuration
	assert.Equal(t, "test-node", cfg.Metadata.NodeID)
	assert.Equal(t, "gateway", cfg.Metadata.Role)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)
	assert.True(t, cfg.Metrics.Enabled)
	assert.Equal(t, "/metrics", cfg.Metrics.Path)
	assert.Equal(t, "0.0.0.0:8080", cfg.Server.Address)
	assert.Len(t, cfg.Proxy.Upstreams, 1)
	assert.Equal(t, "http://localhost:8428", cfg.Proxy.Upstreams[0].URL)
	assert.Equal(t, "HS256", cfg.Auth.JWT.Algorithm)
	assert.Equal(t, "test-secret-key-32-chars-long", cfg.Auth.JWT.Secret)
	assert.Equal(t, "orConditions", cfg.Tenants.Filter.Strategy)
	assert.Len(t, cfg.Tenants.Mappings, 1)
	assert.Equal(t, "local", cfg.Storage.Type)
}

func TestLoadConfig_WithEnvironmentVariables(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv()

	// Set environment variables
	envVars := map[string]string{
		"VM_PROXY_AUTH_METADATA_NODE_ID":                "env-node",
		"VM_PROXY_AUTH_LOGGING_LEVEL":                   "debug",
		"VM_PROXY_AUTH_SERVER_ADDRESS":                  "127.0.0.1:9090",
		"VM_PROXY_AUTH_AUTH_JWT_ALGORITHM":              "RS256",
		"VM_PROXY_AUTH_AUTH_JWT_JWKS_URL":               "https://auth.example.com/.well-known/jwks.json",
		"VM_PROXY_AUTH_STORAGE_TYPE":                    "redis",
		"VM_PROXY_AUTH_STORAGE_REDIS_ADDRESS":           "redis-server:6379",
		"VM_PROXY_AUTH_STORAGE_REDIS_DATABASE":          "5",
		"VM_PROXY_AUTH_CLUSTER_MEMBERLIST_BIND_ADDRESS": "0.0.0.0:7946",
	}

	// Set environment variables
	for key, value := range envVars {
		t.Setenv(key, value)
	}

	// Create a minimal config file
	configContent := `
metadata:
  role: "gateway"
  environment: "test"

proxy:
  upstreams:
    - url: "http://localhost:8428"
      weight: 1
  routing:
    strategy: "round-robin"
    healthCheck:
      interval: "30s"
      timeout: "10s"
      endpoint: "/health"
      healthyThreshold: 2
      unhealthyThreshold: 3
  reliability:
    timeout: "30s"
    retries: 3
    backoff: "100ms"
    queue:
      enabled: false

auth:
  jwt:
    cache:
      tokenTtl: "15m"
      jwksTtl: "1h"
    validation:
      requiredClaims: ["sub"]
    claims:
      userIdClaim: "sub"
      emailClaim: "email"
      groupsClaim: "groups"

tenants:
  filter:
    strategy: "orConditions"
    labels:
      account: "vm_account_id"
      project: "vm_project_id"
  mappings:
    - groups: ["admin"]
      vmTenants:
        - account: "0"
      permissions: ["read", "write"]

storage:
  redis:
    pool:
      size: 10
    timeouts:
      connect: "5s"
      read: "3s"
      write: "3s"
    retry:
      maxAttempts: 3
      backoff:
        min: "100ms"
        max: "1s"
`

	tmpFile := createTempConfigFile(t, configContent)
	defer os.Remove(tmpFile)

	cfg, err := config.LoadConfig(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify environment variables override config file values
	assert.Equal(t, "env-node", cfg.Metadata.NodeID)
	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, "127.0.0.1:9090", cfg.Server.Address)
	assert.Equal(t, "RS256", cfg.Auth.JWT.Algorithm)
	assert.Equal(t, "https://auth.example.com/.well-known/jwks.json", cfg.Auth.JWT.JwksURL)
	assert.Equal(t, "redis", cfg.Storage.Type)
	assert.Equal(t, "redis-server:6379", cfg.Storage.Redis.Address)
	assert.Equal(t, 5, cfg.Storage.Redis.Database)
}

func TestLoadConfig_Defaults(t *testing.T) {
	t.Parallel()

	// Create a minimal config file with only required fields
	configContent := `
metadata:
  nodeId: "minimal-node"
  role: "gateway"

proxy:
  upstreams:
    - url: "http://localhost:8428"

auth:
  jwt:
    algorithm: "HS256"
    secret: "minimal-secret-key-for-testing"

tenants:
  mappings:
    - groups: ["admin"]
      vmTenants:
        - account: "0"
      permissions: ["read", "write"]
`

	tmpFile := createTempConfigFile(t, configContent)
	defer os.Remove(tmpFile)

	cfg, err := config.LoadConfig(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify defaults are applied
	assert.Equal(t, "info", cfg.Logging.Level)                   // Default log level
	assert.Equal(t, "json", cfg.Logging.Format)                  // Default log format
	assert.True(t, cfg.Metrics.Enabled)                          // Default metrics enabled
	assert.Equal(t, "/metrics", cfg.Metrics.Path)                // Default metrics path
	assert.Equal(t, "0.0.0.0:8080", cfg.Server.Address)          // Default server address
	assert.Equal(t, "round-robin", cfg.Proxy.Routing.Strategy)   // Default proxy strategy
	assert.Equal(t, "local", cfg.Storage.Type)                   // Default storage type
	assert.Equal(t, "orConditions", cfg.Tenants.Filter.Strategy) // Default filter strategy
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := config.LoadConfig("non-existent-file.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	t.Parallel()

	invalidContent := `
metadata:
  nodeId: "test"
  invalid yaml content [ unclosed bracket
proxy:
  upstreams:
`

	tmpFile := createTempConfigFile(t, invalidContent)
	defer os.Remove(tmpFile)

	_, err := config.LoadConfig(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoadConfig_ValidationFailure(t *testing.T) {
	t.Parallel()

	// Create config with validation errors
	configContent := `
metadata:
  nodeId: ""  # Invalid: empty node ID
  role: "gateway"

proxy:
  upstreams: []  # Invalid: no upstreams

auth:
  jwt:
    algorithm: "INVALID"  # Invalid algorithm
    secret: "short"       # Too short for HS256

tenants:
  mappings: []  # Invalid: no mappings
`

	tmpFile := createTempConfigFile(t, configContent)
	defer os.Remove(tmpFile)

	_, err := config.LoadConfig(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config validation failed")
}

func TestLoadConfig_EmptyFile(t *testing.T) {
	t.Parallel()

	tmpFile := createTempConfigFile(t, "")
	defer os.Remove(tmpFile)

	_, err := config.LoadConfig(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config validation failed")
}

func TestLoadConfig_RaftStorageWithCluster(t *testing.T) {
	t.Parallel()

	configContent := `
metadata:
  nodeId: "raft-node"
  role: "gateway"
  environment: "test"

proxy:
  upstreams:
    - url: "http://localhost:8428"

auth:
  jwt:
    algorithm: "HS256"
    secret: "test-secret-key-32-chars-long"

tenants:
  mappings:
    - groups: ["admin"]
      vmTenants:
        - account: "0"
      permissions: ["read", "write"]

storage:
  type: "raft"
  raft:
    bindAddress: "0.0.0.0:9000"
    dataDir: "/tmp/raft-test"
    bootstrapExpected: 1

cluster:
  memberlist:
    bindAddress: "0.0.0.0:7946"
    gossip:
      interval: "200ms"
      nodes: 3
    probe:
      interval: "1s"
      timeout: "500ms"
  discovery:
    enabled: false
`

	tmpFile := createTempConfigFile(t, configContent)
	defer os.Remove(tmpFile)

	cfg, err := config.LoadConfig(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify Raft configuration
	assert.Equal(t, "raft", cfg.Storage.Type)
	assert.Equal(t, "0.0.0.0:9000", cfg.Storage.Raft.BindAddress)
	assert.Equal(t, "/tmp/raft-test", cfg.Storage.Raft.DataDir)
	assert.Equal(t, 1, cfg.Storage.Raft.BootstrapExpected)

	// Verify cluster is enabled
	assert.True(t, cfg.IsClusterEnabled())
	assert.Equal(t, "0.0.0.0:7946", cfg.Cluster.Memberlist.BindAddress)
	assert.False(t, cfg.Cluster.Discovery.Enabled)
}

func TestLoadConfig_NoConfigFile(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv()

	// Test loading without a config file (should use defaults + env vars)
	t.Setenv("VM_PROXY_AUTH_METADATA_NODE_ID", "env-only-node")
	t.Setenv("VM_PROXY_AUTH_METADATA_ROLE", "gateway")

	// This should fail because we still need minimal required config
	_, err := config.LoadConfig("")
	require.Error(t, err)
}

func TestEnvironmentVariableBinding(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv()

	tests := []struct {
		name     string
		envVar   string
		envValue string
		checkFn  func(*config.Config) bool
	}{
		{
			name:     "node ID override",
			envVar:   "VM_PROXY_AUTH_METADATA_NODE_ID",
			envValue: "env-node-id",
			checkFn:  func(c *config.Config) bool { return c.Metadata.NodeID == "env-node-id" },
		},
		{
			name:     "log level override",
			envVar:   "VM_PROXY_AUTH_LOGGING_LEVEL",
			envValue: "debug",
			checkFn:  func(c *config.Config) bool { return c.Logging.Level == "debug" },
		},
		{
			name:     "server address override",
			envVar:   "VM_PROXY_AUTH_SERVER_ADDRESS",
			envValue: "10.0.0.1:9999",
			checkFn:  func(c *config.Config) bool { return c.Server.Address == "10.0.0.1:9999" },
		},
		{
			name:     "metrics enabled override",
			envVar:   "VM_PROXY_AUTH_METRICS_ENABLED",
			envValue: "false",
			checkFn:  func(c *config.Config) bool { return !c.Metrics.Enabled },
		},
		{
			name:     "proxy queue enabled override",
			envVar:   "VM_PROXY_AUTH_PROXY_RELIABILITY_QUEUE_ENABLED",
			envValue: "false",
			checkFn:  func(c *config.Config) bool { return !c.Proxy.Reliability.Queue.Enabled },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Cannot use t.Parallel() with t.Setenv()

			// Set the environment variable
			t.Setenv(tt.envVar, tt.envValue)

			// Create a minimal valid config
			configContent := `
metadata:
  nodeId: "config-node"
  role: "gateway"

proxy:
  upstreams:
    - url: "http://localhost:8428"

auth:
  jwt:
    algorithm: "HS256"
    secret: "test-secret-key-32-chars-long"

tenants:
  mappings:
    - groups: ["admin"]
      vmTenants:
        - account: "0"
      permissions: ["read", "write"]
`

			tmpFile := createTempConfigFile(t, configContent)
			defer os.Remove(tmpFile)

			cfg, err := config.LoadConfig(tmpFile)
			require.NoError(t, err)
			require.NotNil(t, cfg)

			// Check that the environment variable override worked
			assert.True(t, tt.checkFn(cfg), "Environment variable %s should override config", tt.envVar)
		})
	}
}

// Helper function to create temporary config files.
func createTempConfigFile(t *testing.T, content string) string {
	t.Helper()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")

	err := os.WriteFile(tmpFile, []byte(content), 0o644)
	require.NoError(t, err)

	return tmpFile
}
