package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

func createValidMultipleUpstreamConfig() *config.ViperConfig {
	return &config.ViperConfig{
		Upstream: config.UpstreamSettings{
			Multiple: config.MultipleUpstreamSettings{
				Enabled: true,
				Backends: []config.BackendSettings{
					{URL: "http://backend1.com", Weight: 1},
					{URL: "http://backend2.com", Weight: 2},
					{URL: "http://backend3.com", Weight: 1},
				},
				LoadBalancing: config.LoadBalancingSettings{
					Strategy: "weighted-round-robin",
				},
				HealthCheck: config.HealthCheckSettings{
					CheckInterval:      30 * time.Second,
					Timeout:            10 * time.Second,
					HealthyThreshold:   2,
					UnhealthyThreshold: 3,
					HealthEndpoint:     "/health",
				},
				Queue: config.QueueSettings{
					Enabled: true,
					MaxSize: 1000,
					Timeout: 5 * time.Second,
				},
				Timeout:      30 * time.Second,
				MaxRetries:   3,
				RetryBackoff: 100 * time.Millisecond,
			},
		},
	}
}

func TestViperConfig_IsMultipleUpstreamsEnabled(t *testing.T) {
	testCases := []struct {
		name     string
		config   *config.ViperConfig
		expected bool
	}{
		{
			name:     "Enabled with backends",
			config:   createValidMultipleUpstreamConfig(),
			expected: true,
		},
		{
			name: "Enabled but no backends",
			config: &config.ViperConfig{
				Upstream: config.UpstreamSettings{
					Multiple: config.MultipleUpstreamSettings{
						Enabled:  true,
						Backends: []config.BackendSettings{},
					},
				},
			},
			expected: false,
		},
		{
			name: "Disabled",
			config: &config.ViperConfig{
				Upstream: config.UpstreamSettings{
					Multiple: config.MultipleUpstreamSettings{
						Enabled: false,
						Backends: []config.BackendSettings{
							{URL: "http://backend1.com", Weight: 1},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Default (disabled)",
			config: &config.ViperConfig{
				Upstream: config.UpstreamSettings{},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.config.IsMultipleUpstreamsEnabled()
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestViperConfig_ValidateMultipleUpstreams(t *testing.T) {
	testCases := []struct {
		name      string
		config    *config.ViperConfig
		expectErr bool
		errMsg    string
	}{
		{
			name:      "Valid configuration",
			config:    createValidMultipleUpstreamConfig(),
			expectErr: false,
		},
		{
			name: "Disabled - no validation",
			config: &config.ViperConfig{
				Upstream: config.UpstreamSettings{
					Multiple: config.MultipleUpstreamSettings{
						Enabled: false,
					},
				},
			},
			expectErr: false,
		},
		{
			name: "Enabled but no backends",
			config: &config.ViperConfig{
				Upstream: config.UpstreamSettings{
					Multiple: config.MultipleUpstreamSettings{
						Enabled:  true,
						Backends: []config.BackendSettings{},
					},
				},
			},
			expectErr: true,
			errMsg:    "no backends configured",
		},
		{
			name: "Invalid load balancing strategy",
			config: &config.ViperConfig{
				Upstream: config.UpstreamSettings{
					Multiple: config.MultipleUpstreamSettings{
						Enabled: true,
						Backends: []config.BackendSettings{
							{URL: "http://backend1.com", Weight: 1},
						},
						LoadBalancing: config.LoadBalancingSettings{
							Strategy: "invalid-strategy",
						},
					},
				},
			},
			expectErr: true,
			errMsg:    "invalid load balancing strategy",
		},
		{
			name: "Backend missing URL",
			config: &config.ViperConfig{
				Upstream: config.UpstreamSettings{
					Multiple: config.MultipleUpstreamSettings{
						Enabled: true,
						Backends: []config.BackendSettings{
							{URL: "", Weight: 1},
						},
						LoadBalancing: config.LoadBalancingSettings{
							Strategy: "round-robin",
						},
					},
				},
			},
			expectErr: true,
			errMsg:    "URL is required",
		},
		{
			name: "Backend negative weight",
			config: &config.ViperConfig{
				Upstream: config.UpstreamSettings{
					Multiple: config.MultipleUpstreamSettings{
						Enabled: true,
						Backends: []config.BackendSettings{
							{URL: "http://backend1.com", Weight: -1},
						},
						LoadBalancing: config.LoadBalancingSettings{
							Strategy: "round-robin",
						},
						Timeout: 30 * time.Second,
					},
				},
			},
			expectErr: true,
			errMsg:    "weight must be non-negative",
		},
		{
			name: "Zero timeout",
			config: &config.ViperConfig{
				Upstream: config.UpstreamSettings{
					Multiple: config.MultipleUpstreamSettings{
						Enabled: true,
						Backends: []config.BackendSettings{
							{URL: "http://backend1.com", Weight: 1},
						},
						LoadBalancing: config.LoadBalancingSettings{
							Strategy: "round-robin",
						},
						Timeout: 0,
					},
				},
			},
			expectErr: true,
			errMsg:    "timeout must be positive",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.ValidateMultipleUpstreams()
			if tc.expectErr {
				require.Error(t, err)
				if tc.errMsg != "" {
					assert.Contains(t, err.Error(), tc.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestViperConfig_ToEnhancedServiceConfig(t *testing.T) {
	config := createValidMultipleUpstreamConfig()

	serviceConfig, err := config.ToEnhancedServiceConfig()
	require.NoError(t, err)
	require.NotNil(t, serviceConfig)

	// Verify backends conversion
	assert.Len(t, serviceConfig.Backends, 3)
	assert.Equal(t, "http://backend1.com", serviceConfig.Backends[0].URL)
	assert.Equal(t, 1, serviceConfig.Backends[0].Weight)
	assert.Equal(t, "http://backend2.com", serviceConfig.Backends[1].URL)
	assert.Equal(t, 2, serviceConfig.Backends[1].Weight)
	assert.Equal(t, "http://backend3.com", serviceConfig.Backends[2].URL)
	assert.Equal(t, 1, serviceConfig.Backends[2].Weight)

	// Verify load balancing
	assert.Equal(t, domain.LoadBalancingWeightedRoundRobin, serviceConfig.LoadBalancing.Strategy)

	// Verify health check settings
	assert.Equal(t, 30*time.Second, serviceConfig.HealthCheck.CheckInterval)
	assert.Equal(t, 10*time.Second, serviceConfig.HealthCheck.Timeout)
	assert.Equal(t, 2, serviceConfig.HealthCheck.HealthyThreshold)
	assert.Equal(t, 3, serviceConfig.HealthCheck.UnhealthyThreshold)
	assert.Equal(t, "/health", serviceConfig.HealthCheck.HealthEndpoint)

	// Verify queue settings
	assert.True(t, serviceConfig.EnableQueueing)
	assert.Equal(t, 1000, serviceConfig.Queue.MaxSize)
	assert.Equal(t, 5*time.Second, serviceConfig.Queue.Timeout)

	// Verify other settings
	assert.Equal(t, 30*time.Second, serviceConfig.Timeout)
	assert.Equal(t, 3, serviceConfig.MaxRetries)
	assert.Equal(t, 100*time.Millisecond, serviceConfig.RetryBackoff)
}

func TestViperConfig_ToEnhancedServiceConfig_Defaults(t *testing.T) {
	config := &config.ViperConfig{
		Upstream: config.UpstreamSettings{
			Multiple: config.MultipleUpstreamSettings{
				Enabled: true,
				Backends: []config.BackendSettings{
					{URL: "http://backend1.com", Weight: 0}, // Zero weight should default to 1
				},
				LoadBalancing: config.LoadBalancingSettings{
					Strategy: "round-robin",
				},
				Timeout: 30 * time.Second,
				// MaxRetries, RetryBackoff not set - should get defaults
				Queue: config.QueueSettings{
					Enabled: true,
					// MaxSize, Timeout not set - should get defaults
				},
			},
		},
	}

	serviceConfig, err := config.ToEnhancedServiceConfig()
	require.NoError(t, err)
	require.NotNil(t, serviceConfig)

	// Verify defaults
	assert.Equal(t, 1, serviceConfig.Backends[0].Weight)                 // Zero weight became 1
	assert.Equal(t, 3, serviceConfig.MaxRetries)                         // Default
	assert.Equal(t, 100*time.Millisecond, serviceConfig.RetryBackoff)    // Default
	assert.Equal(t, "/health", serviceConfig.HealthCheck.HealthEndpoint) // Default
	assert.Equal(t, 1000, serviceConfig.Queue.MaxSize)                   // Default
	assert.Equal(t, 5*time.Second, serviceConfig.Queue.Timeout)          // Default
}

func TestViperConfig_ToEnhancedServiceConfig_NotEnabled(t *testing.T) {
	config := &config.ViperConfig{
		Upstream: config.UpstreamSettings{
			Multiple: config.MultipleUpstreamSettings{
				Enabled: false,
			},
		},
	}

	serviceConfig, err := config.ToEnhancedServiceConfig()
	require.Error(t, err)
	assert.Nil(t, serviceConfig)
	assert.Contains(t, err.Error(), "multiple upstreams not enabled")
}

func TestViperConfig_ToEnhancedServiceConfig_InvalidConfig(t *testing.T) {
	config := &config.ViperConfig{
		Upstream: config.UpstreamSettings{
			Multiple: config.MultipleUpstreamSettings{
				Enabled: true,
				Backends: []config.BackendSettings{
					{URL: "", Weight: 1}, // Invalid: empty URL
				},
				LoadBalancing: config.LoadBalancingSettings{
					Strategy: "round-robin",
				},
				Timeout: 30 * time.Second,
			},
		},
	}

	serviceConfig, err := config.ToEnhancedServiceConfig()
	require.Error(t, err)
	assert.Nil(t, serviceConfig)
	assert.Contains(t, err.Error(), "invalid multiple upstream configuration")
}

func TestViperConfig_LoadBalancingStrategies(t *testing.T) {
	strategies := []string{
		"round-robin",
		"weighted-round-robin",
		"least-connections",
	}

	for _, strategy := range strategies {
		t.Run(strategy, func(t *testing.T) {
			config := &config.ViperConfig{
				Upstream: config.UpstreamSettings{
					Multiple: config.MultipleUpstreamSettings{
						Enabled: true,
						Backends: []config.BackendSettings{
							{URL: "http://backend1.com", Weight: 1},
						},
						LoadBalancing: config.LoadBalancingSettings{
							Strategy: strategy,
						},
						Timeout: 30 * time.Second,
					},
				},
			}

			serviceConfig, err := config.ToEnhancedServiceConfig()
			require.NoError(t, err)
			assert.Equal(t, domain.LoadBalancingStrategy(strategy), serviceConfig.LoadBalancing.Strategy)
		})
	}
}
