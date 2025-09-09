package proxy_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config/modules/proxy"
)

func TestProxyConfig_Validate_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config proxy.Config
	}{
		{
			name: "valid minimal config",
			config: proxy.Config{
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
		},
		{
			name: "valid multi-upstream weighted config",
			config: proxy.Config{
				Upstreams: []proxy.UpstreamConfig{
					{URL: "http://vm-1:8428", Weight: 3},
					{URL: "http://vm-2:8428", Weight: 2},
					{URL: "http://vm-3:8428", Weight: 1},
				},
				Routing: proxy.RoutingConfig{
					Strategy: "weighted-round-robin",
					HealthCheck: proxy.HealthCheckConfig{
						Interval:           15 * time.Second,
						Timeout:            5 * time.Second,
						Endpoint:           "/metrics",
						HealthyThreshold:   1,
						UnhealthyThreshold: 2,
					},
				},
				Reliability: proxy.ReliabilityConfig{
					Timeout: 60 * time.Second,
					Retries: 5,
					Backoff: 200 * time.Millisecond,
					Queue: proxy.QueueConfig{
						Enabled: true,
						MaxSize: 5000,
						Timeout: 30 * time.Second,
					},
				},
			},
		},
		{
			name: "valid with zero weights (defaults applied)",
			config: proxy.Config{
				Upstreams: []proxy.UpstreamConfig{
					{URL: "http://vm-1:8428", Weight: 0}, // Should be treated as weight 1
					{URL: "http://vm-2:8428", Weight: 2},
				},
				Routing: proxy.RoutingConfig{
					Strategy: "least-connections",
					HealthCheck: proxy.HealthCheckConfig{
						Interval:           45 * time.Second,
						Timeout:            15 * time.Second,
						Endpoint:           "/ready",
						HealthyThreshold:   3,
						UnhealthyThreshold: 5,
					},
				},
				Reliability: proxy.ReliabilityConfig{
					Timeout: 45 * time.Second,
					Retries: 2,
					Backoff: 50 * time.Millisecond,
					Queue: proxy.QueueConfig{
						Enabled: false,
						MaxSize: 100,
						Timeout: 1 * time.Second,
					},
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

func TestProxyConfig_Validate_Failure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      proxy.Config
		expectedErr string
	}{
		{
			name: "no upstreams",
			config: proxy.Config{
				Upstreams: []proxy.UpstreamConfig{},
			},
			expectedErr: "at least one upstream is required",
		},
		{
			name: "upstream with empty URL",
			config: proxy.Config{
				Upstreams: []proxy.UpstreamConfig{
					{URL: "", Weight: 1},
				},
			},
			expectedErr: "upstream URL is required",
		},
		{
			name: "upstream with negative weight",
			config: proxy.Config{
				Upstreams: []proxy.UpstreamConfig{
					{URL: "http://localhost:8428", Weight: -1},
				},
			},
			expectedErr: "upstream weight must be non-negative",
		},
		{
			name: "invalid routing strategy",
			config: proxy.Config{
				Upstreams: []proxy.UpstreamConfig{
					{URL: "http://localhost:8428", Weight: 1},
				},
				Routing: proxy.RoutingConfig{
					Strategy: "invalid-strategy",
				},
			},
			expectedErr: "invalid routing strategy",
		},
		{
			name: "negative health check interval",
			config: proxy.Config{
				Upstreams: []proxy.UpstreamConfig{
					{URL: "http://localhost:8428", Weight: 1},
				},
				Routing: proxy.RoutingConfig{
					Strategy: "round-robin",
					HealthCheck: proxy.HealthCheckConfig{
						Interval: -1 * time.Second,
					},
				},
			},
			expectedErr: "health check interval must be positive",
		},
		{
			name: "zero health check timeout",
			config: proxy.Config{
				Upstreams: []proxy.UpstreamConfig{
					{URL: "http://localhost:8428", Weight: 1},
				},
				Routing: proxy.RoutingConfig{
					Strategy: "round-robin",
					HealthCheck: proxy.HealthCheckConfig{
						Interval: 30 * time.Second,
						Timeout:  0,
					},
				},
			},
			expectedErr: "health check timeout must be positive",
		},
		{
			name: "empty health check endpoint",
			config: proxy.Config{
				Upstreams: []proxy.UpstreamConfig{
					{URL: "http://localhost:8428", Weight: 1},
				},
				Routing: proxy.RoutingConfig{
					Strategy: "round-robin",
					HealthCheck: proxy.HealthCheckConfig{
						Interval:           30 * time.Second,
						Timeout:            10 * time.Second,
						Endpoint:           "",
						HealthyThreshold:   2,
						UnhealthyThreshold: 3,
					},
				},
			},
			expectedErr: "health check endpoint is required",
		},
		{
			name: "zero healthy threshold",
			config: proxy.Config{
				Upstreams: []proxy.UpstreamConfig{
					{URL: "http://localhost:8428", Weight: 1},
				},
				Routing: proxy.RoutingConfig{
					Strategy: "round-robin",
					HealthCheck: proxy.HealthCheckConfig{
						Interval:         30 * time.Second,
						Timeout:          10 * time.Second,
						Endpoint:         "/health",
						HealthyThreshold: 0,
					},
				},
			},
			expectedErr: "healthy threshold must be positive",
		},
		{
			name: "zero unhealthy threshold",
			config: proxy.Config{
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
						UnhealthyThreshold: 0,
					},
				},
			},
			expectedErr: "unhealthy threshold must be positive",
		},
		{
			name: "zero reliability timeout",
			config: proxy.Config{
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
					Timeout: 0,
				},
			},
			expectedErr: "timeout must be positive",
		},
		{
			name: "negative retries",
			config: proxy.Config{
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
					Retries: -1,
				},
			},
			expectedErr: "retries must be non-negative",
		},
		{
			name: "negative queue max size",
			config: proxy.Config{
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
						Enabled: true,
						MaxSize: -1,
					},
				},
			},
			expectedErr: "queue max size must be positive",
		},
		{
			name: "zero queue timeout",
			config: proxy.Config{
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
						Enabled: true,
						MaxSize: 1000,
						Timeout: 0,
					},
				},
			},
			expectedErr: "queue timeout must be positive",
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

func TestProxyConfig_Defaults(t *testing.T) {
	t.Parallel()

	defaults := proxy.GetDefaults()

	// Note: GetDefaults returns empty upstreams since they're required to be configured
	assert.Empty(t, defaults.Upstreams)
	assert.Equal(t, "round-robin", defaults.Routing.Strategy)
	assert.Equal(t, 30*time.Second, defaults.Routing.HealthCheck.Interval)
	assert.Equal(t, 10*time.Second, defaults.Routing.HealthCheck.Timeout)
	assert.Equal(t, "/health", defaults.Routing.HealthCheck.Endpoint)
	assert.Equal(t, 2, defaults.Routing.HealthCheck.HealthyThreshold)
	assert.Equal(t, 3, defaults.Routing.HealthCheck.UnhealthyThreshold)
	assert.Equal(t, 30*time.Second, defaults.Reliability.Timeout)
	assert.Equal(t, 3, defaults.Reliability.Retries)
	assert.Equal(t, 100*time.Millisecond, defaults.Reliability.Backoff)
	assert.True(t, defaults.Reliability.Queue.Enabled)
	assert.Equal(t, 1000, defaults.Reliability.Queue.MaxSize)
	assert.Equal(t, 5*time.Second, defaults.Reliability.Queue.Timeout)
}

func TestUpstreamConfig_Validation_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		upstream proxy.UpstreamConfig
		isValid  bool
	}{
		{
			name:     "HTTPS URL",
			upstream: proxy.UpstreamConfig{URL: "https://vm.example.com:8428", Weight: 1},
			isValid:  true,
		},
		{
			name:     "URL with path",
			upstream: proxy.UpstreamConfig{URL: "http://vm.example.com:8428/vm/api/v1", Weight: 1},
			isValid:  true,
		},
		{
			name:     "localhost URL",
			upstream: proxy.UpstreamConfig{URL: "http://localhost:8428", Weight: 1},
			isValid:  true,
		},
		{
			name:     "IPv4 URL",
			upstream: proxy.UpstreamConfig{URL: "http://192.168.1.100:8428", Weight: 1},
			isValid:  true,
		},
		{
			name:     "IPv6 URL",
			upstream: proxy.UpstreamConfig{URL: "http://[::1]:8428", Weight: 1},
			isValid:  true,
		},
		{
			name:     "Weight zero (valid, treated as 1)",
			upstream: proxy.UpstreamConfig{URL: "http://localhost:8428", Weight: 0},
			isValid:  true,
		},
		{
			name:     "Very high weight",
			upstream: proxy.UpstreamConfig{URL: "http://localhost:8428", Weight: 1000},
			isValid:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.upstream.Validate()
			if tt.isValid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
