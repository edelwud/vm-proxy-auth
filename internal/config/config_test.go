package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/auth"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/cluster"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/logging"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/metadata"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/metrics"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/proxy"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/server"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/storage"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/tenant"
)

func TestConfig_Validate_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config config.Config
	}{
		{
			name: "valid minimal config",
			config: config.Config{
				Metadata: metadata.Config{
					NodeID:      "vm-proxy-auth-1",
					Role:        "gateway",
					Environment: "development",
					Region:      "local",
				},
				Logging: logging.Config{
					Level:  "info",
					Format: "json",
				},
				Metrics: metrics.Config{
					Enabled: true,
					Path:    "/metrics",
				},
				Server: server.Config{
					Address: "0.0.0.0:8080",
					Timeouts: server.Timeouts{
						Read:  30 * time.Second,
						Write: 30 * time.Second,
						Idle:  60 * time.Second,
					},
				},
				Proxy: proxy.Config{
					Upstreams: []proxy.UpstreamConfig{
						{URL: "http://localhost:8428", Weight: 1},
					},
					Routing: proxy.RoutingConfig{
						Strategy: "round-robin",
						HealthCheck: proxy.HealthCheckConfig{
							Interval:           30 * time.Second,
							Timeout:            10 * time.Second,
							Endpoint:           "/health",
							HealthyThreshold:   2,
							UnhealthyThreshold: 3,
						},
					},
					Reliability: proxy.ReliabilityConfig{
						Timeout: 30 * time.Second,
						Retries: 3,
						Backoff: 100 * time.Millisecond,
						Queue: proxy.QueueConfig{
							Enabled: false,
							MaxSize: 1000,
							Timeout: 5 * time.Second,
						},
					},
				},
				Auth: auth.Config{
					JWT: auth.JWTConfig{
						Algorithm: "HS256",
						Secret:    "test-secret-key-32-chars-long",
						Cache: auth.CacheConfig{
							TokenTTL: 15 * time.Minute,
							JwksTTL:  1 * time.Hour,
						},
						Validation: auth.ValidationConfig{
							Audience: false,
							Issuer:   false,
						},
						Claims: auth.ClaimsConfig{
							UserGroups: "groups",
						},
					},
				},
				Tenants: tenant.Config{
					Filter: tenant.FilterConfig{
						Strategy: "orConditions",
						Labels: tenant.LabelsConfig{
							Account:      "vm_account_id",
							Project:      "vm_project_id",
							UseProjectID: false,
						},
					},
					Mappings: []tenant.MappingConfig{
						{
							Groups: []string{"admin"},
							VMTenants: []tenant.VMTenantConfig{
								{Account: "0", Project: ""},
							},
							Permissions: []string{"read", "write"},
						},
					},
				},
				Storage: storage.Config{
					Type: "local",
				},
				Cluster: cluster.Config{}, // Empty cluster config for local storage
			},
		},
		{
			name: "valid redis config",
			config: config.Config{
				Metadata: metadata.Config{
					NodeID:      "vm-proxy-auth-1",
					Role:        "gateway",
					Environment: "production",
					Region:      "us-east-1",
				},
				Logging: logging.Config{
					Level:  "warn",
					Format: "json",
				},
				Metrics: metrics.Config{
					Enabled: true,
					Path:    "/metrics",
				},
				Server: server.Config{
					Address: "0.0.0.0:8080",
					Timeouts: server.Timeouts{
						Read:  30 * time.Second,
						Write: 30 * time.Second,
						Idle:  60 * time.Second,
					},
				},
				Proxy: proxy.Config{
					Upstreams: []proxy.UpstreamConfig{
						{URL: "http://vm-1:8428", Weight: 3},
						{URL: "http://vm-2:8428", Weight: 2},
					},
					Routing: proxy.RoutingConfig{
						Strategy: "weighted-round-robin",
						HealthCheck: proxy.HealthCheckConfig{
							Interval:           15 * time.Second,
							Timeout:            5 * time.Second,
							Endpoint:           "/health",
							HealthyThreshold:   1,
							UnhealthyThreshold: 2,
						},
					},
					Reliability: proxy.ReliabilityConfig{
						Timeout: 45 * time.Second,
						Retries: 5,
						Backoff: 200 * time.Millisecond,
						Queue: proxy.QueueConfig{
							Enabled: true,
							MaxSize: 5000,
							Timeout: 30 * time.Second,
						},
					},
				},
				Auth: auth.Config{
					JWT: auth.JWTConfig{
						Algorithm: "RS256",
						JwksURL:   "https://auth.example.com/.well-known/jwks.json",
						Cache: auth.CacheConfig{
							TokenTTL: 10 * time.Minute,
							JwksTTL:  2 * time.Hour,
						},
						Validation: auth.ValidationConfig{
							Audience:         true,
							Issuer:           false,
							RequiredAudience: []string{"api.example.com"},
						},
						Claims: auth.ClaimsConfig{
							UserGroups: "groups",
						},
					},
				},
				Tenants: tenant.Config{
					Filter: tenant.FilterConfig{
						Strategy: "orConditions",
						Labels: tenant.LabelsConfig{
							Account:      "account_id",
							Project:      "project_id",
							UseProjectID: true,
						},
					},
					Mappings: []tenant.MappingConfig{
						{
							Groups: []string{"admin", "sre"},
							VMTenants: []tenant.VMTenantConfig{
								{Account: "0", Project: "default"},
								{Account: "1", Project: "monitoring"},
							},
							Permissions: []string{"read", "write"},
						},
						{
							Groups: []string{"developer"},
							VMTenants: []tenant.VMTenantConfig{
								{Account: "2", Project: "dev"},
							},
							Permissions: []string{"read"},
						},
					},
				},
				Storage: storage.Config{
					Type: "redis",
					Redis: storage.RedisConfig{
						Address:   "redis:6379",
						Password:  "redis-password",
						Database:  1,
						KeyPrefix: "vm-proxy:",
						Pool: storage.RedisPoolConfig{
							Size:    20,
							MinIdle: 10,
						},
						Timeouts: storage.RedisTimeoutsConfig{
							Connect: 10 * time.Second,
							Read:    5 * time.Second,
							Write:   5 * time.Second,
						},
						Retry: storage.RedisRetryConfig{
							MaxAttempts: 5,
							Backoff: storage.BackoffConfig{
								Min: 50 * time.Millisecond,
								Max: 2 * time.Second,
							},
						},
					},
				},
				Cluster: cluster.Config{}, // Empty cluster config for Redis storage
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

func TestConfig_Validate_Failure(t *testing.T) {
	t.Parallel()

	// Helper function to create minimal valid config
	minimalConfig := func() config.Config {
		return config.Config{
			Metadata: metadata.Config{
				NodeID: "test-node",
				Role:   "gateway",
			},
			Server: server.Config{
				Address: "0.0.0.0:8080",
				Timeouts: server.Timeouts{
					Read:  30 * time.Second,
					Write: 30 * time.Second,
					Idle:  60 * time.Second,
				},
			},
			Auth: auth.Config{
				JWT: auth.JWTConfig{
					Algorithm: "HS256",
					Secret:    "test-secret-key-32-chars-minimum",
					Cache: auth.CacheConfig{
						TokenTTL: 15 * time.Minute,
						JwksTTL:  1 * time.Hour,
					},
				},
			},
			Proxy: proxy.Config{
				Upstreams: []proxy.UpstreamConfig{
					{URL: "http://localhost:8428", Weight: 1},
				},
				Routing: proxy.RoutingConfig{
					Strategy: "round-robin",
					HealthCheck: proxy.HealthCheckConfig{
						Interval:           30 * time.Second,
						Timeout:            10 * time.Second,
						Endpoint:           "/health",
						HealthyThreshold:   2,
						UnhealthyThreshold: 3,
					},
				},
				Reliability: proxy.ReliabilityConfig{
					Timeout: 30 * time.Second,
					Retries: 3,
					Backoff: 100 * time.Millisecond,
				},
			},
			Tenants: tenant.Config{
				Filter: tenant.FilterConfig{
					Strategy: "orConditions",
					Labels: tenant.LabelsConfig{
						Account: "vm_account_id",
						Project: "vm_project_id",
					},
				},
				Mappings: []tenant.MappingConfig{
					{
						Groups:      []string{"admin"},
						VMTenants:   []tenant.VMTenantConfig{{Account: "0"}},
						Permissions: []string{"read", "write"},
					},
				},
			},
			Storage: storage.Config{
				Type: "local",
			},
		}
	}

	tests := []struct {
		name        string
		modifyFunc  func(*config.Config)
		expectedErr string
	}{
		{
			name: "missing server address",
			modifyFunc: func(c *config.Config) {
				c.Server.Address = ""
			},
			expectedErr: "server address is required",
		},
		{
			name: "invalid routing strategy",
			modifyFunc: func(c *config.Config) {
				c.Proxy.Routing.Strategy = "invalid-strategy"
			},
			expectedErr: "invalid routing strategy",
		},
		{
			name: "missing JWT secret for HS256",
			modifyFunc: func(c *config.Config) {
				c.Auth.JWT.Algorithm = "HS256"
				c.Auth.JWT.Secret = ""
				c.Auth.JWT.JwksURL = "https://example.com/jwks" // Set JWKS URL so first validation passes
			},
			expectedErr: "HS256 algorithm requires secret",
		},
		{
			name: "missing JWKS URL for RS256",
			modifyFunc: func(c *config.Config) {
				c.Auth.JWT.Algorithm = "RS256"
				c.Auth.JWT.Secret = "temp-secret-for-validation" // Provide secret so first validation passes
				c.Auth.JWT.JwksURL = ""
			},
			expectedErr: "RS256 algorithm requires jwksUrl",
		},
		{
			name: "no upstreams",
			modifyFunc: func(c *config.Config) {
				c.Proxy.Upstreams = []proxy.UpstreamConfig{}
			},
			expectedErr: "at least one upstream is required",
		},
		{
			name: "no tenant mappings",
			modifyFunc: func(c *config.Config) {
				c.Tenants.Mappings = []tenant.MappingConfig{}
			},
			expectedErr: "at least one tenant mapping is required",
		},
		{
			name: "invalid storage type",
			modifyFunc: func(c *config.Config) {
				c.Storage.Type = "invalid-storage"
			},
			expectedErr: "invalid storage type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := minimalConfig()
			tt.modifyFunc(&cfg)
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestConfig_IsClusterEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		storageType string
		expected    bool
	}{
		{"local storage", "local", false},
		{"redis storage", "redis", false},
		{"raft storage", "raft", true},
		{"empty storage type", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Config{
				Storage: storage.Config{
					Type: tt.storageType,
				},
			}
			assert.Equal(t, tt.expected, cfg.IsClusterEnabled())
		})
	}
}

func TestMetadataConfig_Fields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata metadata.Config
	}{
		{
			name: "valid minimal metadata",
			metadata: metadata.Config{
				NodeID: "node-1",
				Role:   "gateway",
			},
		},
		{
			name: "valid full metadata",
			metadata: metadata.Config{
				NodeID:      "vm-proxy-auth-prod-1",
				Role:        "gateway",
				Environment: "production",
				Region:      "us-east-1",
			},
		},
		{
			name: "empty metadata",
			metadata: metadata.Config{
				NodeID: "",
				Role:   "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Just verify the fields can be set and retrieved
			assert.IsType(t, "", tt.metadata.NodeID)
			assert.IsType(t, "", tt.metadata.Role)
			assert.IsType(t, "", tt.metadata.Environment)
			assert.IsType(t, "", tt.metadata.Region)
		})
	}
}

func TestLoggingConfig_Fields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		logging logging.Config
	}{
		{
			name: "debug json",
			logging: logging.Config{
				Level:  "debug",
				Format: "json",
			},
		},
		{
			name: "info console",
			logging: logging.Config{
				Level:  "info",
				Format: "console",
			},
		},
		{
			name: "empty values",
			logging: logging.Config{
				Level:  "",
				Format: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Just verify the fields can be set and retrieved
			assert.IsType(t, "", tt.logging.Level)
			assert.IsType(t, "", tt.logging.Format)
		})
	}
}

func TestMetricsConfig_Fields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		metrics metrics.Config
	}{
		{
			name: "enabled metrics",
			metrics: metrics.Config{
				Enabled: true,
				Path:    "/metrics",
			},
		},
		{
			name: "disabled metrics",
			metrics: metrics.Config{
				Enabled: false,
				Path:    "/any-path",
			},
		},
		{
			name: "custom path",
			metrics: metrics.Config{
				Enabled: true,
				Path:    "/custom/metrics/endpoint",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Just verify the fields can be set and retrieved
			assert.IsType(t, false, tt.metrics.Enabled)
			assert.IsType(t, "", tt.metrics.Path)
		})
	}
}

func TestConfig_ModuleIntegration(t *testing.T) {
	t.Parallel()

	// Test that all modules integrate correctly at the config level
	cfg := config.Config{
		Metadata: metadata.Config{
			NodeID:      "integration-test",
			Role:        "gateway",
			Environment: "test",
			Region:      "local",
		},
		Logging: logging.Config{
			Level:  "debug",
			Format: "console",
		},
		Metrics: metrics.Config{
			Enabled: true,
			Path:    "/test-metrics",
		},
		Server: server.Config{
			Address: "127.0.0.1:8080",
			Timeouts: server.Timeouts{
				Read:  15 * time.Second,
				Write: 15 * time.Second,
				Idle:  30 * time.Second,
			},
		},
		Proxy: proxy.Config{
			Upstreams: []proxy.UpstreamConfig{
				{URL: "http://test:8428", Weight: 1},
			},
			Routing: proxy.RoutingConfig{
				Strategy: "round-robin",
				HealthCheck: proxy.HealthCheckConfig{
					Interval:           20 * time.Second,
					Timeout:            5 * time.Second,
					Endpoint:           "/health",
					HealthyThreshold:   1,
					UnhealthyThreshold: 2,
				},
			},
			Reliability: proxy.ReliabilityConfig{
				Timeout: 20 * time.Second,
				Retries: 2,
				Backoff: 50 * time.Millisecond,
				Queue: proxy.QueueConfig{
					Enabled: false,
					MaxSize: 500,
					Timeout: 2 * time.Second,
				},
			},
		},
		Auth: auth.Config{
			JWT: auth.JWTConfig{
				Algorithm: "HS256",
				Secret:    "test-integration-secret-key",
				Cache: auth.CacheConfig{
					TokenTTL: 5 * time.Minute,
					JwksTTL:  30 * time.Minute,
				},
				Validation: auth.ValidationConfig{
					Audience: false,
					Issuer:   false,
				},
				Claims: auth.ClaimsConfig{
					UserGroups: "groups",
				},
			},
		},
		Tenants: tenant.Config{
			Filter: tenant.FilterConfig{
				Strategy: "orConditions",
				Labels: tenant.LabelsConfig{
					Account:      "test_account",
					Project:      "test_project",
					UseProjectID: false,
				},
			},
			Mappings: []tenant.MappingConfig{
				{
					Groups: []string{"test-group"},
					VMTenants: []tenant.VMTenantConfig{
						{Account: "0", Project: "test"},
					},
					Permissions: []string{"read"},
				},
			},
		},
		Storage: storage.Config{
			Type: "local",
		},
		Cluster: cluster.Config{}, // Empty for local storage
	}

	// Validate the entire configuration
	err := cfg.Validate()
	require.NoError(t, err, "Integrated configuration should be valid")

	// Test cluster detection
	assert.False(t, cfg.IsClusterEnabled(), "Local storage should not enable cluster")

	// Test Raft storage enables cluster
	cfg.Storage.Type = "raft"
	cfg.Storage.Raft = storage.RaftConfig{
		BindAddress:       "0.0.0.0:9000",
		DataDir:           "/tmp/test-raft",
		BootstrapExpected: 1,
	}
	cfg.Cluster = cluster.Config{
		Memberlist: cluster.MemberlistConfig{
			BindAddress: "0.0.0.0:7946",
			Gossip: cluster.GossipConfig{
				Interval: 200 * time.Millisecond,
				Nodes:    3,
			},
			Probe: cluster.ProbeConfig{
				Interval: 1 * time.Second,
				Timeout:  500 * time.Millisecond,
			},
		},
		Discovery: cluster.DiscoveryConfig{
			Enabled:  false,            // Disable for test
			Interval: 30 * time.Second, // Still required by validation
		},
	}

	err = cfg.Validate()
	require.NoError(t, err, "Raft configuration should be valid")
	assert.True(t, cfg.IsClusterEnabled(), "Raft storage should enable cluster")
}
