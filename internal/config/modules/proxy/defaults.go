package proxy

import (
	"time"
)

// Default proxy configuration values.
const (
	DefaultStrategy            = "round-robin"
	DefaultHealthCheckInterval = 30 * time.Second
	DefaultHealthCheckTimeout  = 10 * time.Second
	DefaultHealthCheckEndpoint = "/health"
	DefaultHealthyThreshold    = 2
	DefaultUnhealthyThreshold  = 3
	DefaultTimeout             = 30 * time.Second
	DefaultRetries             = 3
	DefaultBackoff             = 100 * time.Millisecond
	DefaultQueueEnabled        = true
	DefaultQueueMaxSize        = 1000
	DefaultQueueTimeout        = 5 * time.Second
	DefaultUpstreamWeight      = 1
)

// GetDefaults returns default proxy configuration.
func GetDefaults() Config {
	return Config{
		Upstreams: []UpstreamConfig{},
		Routing: RoutingConfig{
			Strategy: DefaultStrategy,
			HealthCheck: HealthCheckConfig{
				Interval:           DefaultHealthCheckInterval,
				Timeout:            DefaultHealthCheckTimeout,
				Endpoint:           DefaultHealthCheckEndpoint,
				HealthyThreshold:   DefaultHealthyThreshold,
				UnhealthyThreshold: DefaultUnhealthyThreshold,
			},
		},
		Reliability: ReliabilityConfig{
			Timeout: DefaultTimeout,
			Retries: DefaultRetries,
			Backoff: DefaultBackoff,
			Queue: QueueConfig{
				Enabled: DefaultQueueEnabled,
				MaxSize: DefaultQueueMaxSize,
				Timeout: DefaultQueueTimeout,
			},
		},
	}
}
