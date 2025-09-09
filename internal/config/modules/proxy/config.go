package proxy

import (
	"errors"
	"fmt"
	"net/url"
	"time"
)

// Config represents proxy configuration.
type Config struct {
	Upstreams   []UpstreamConfig  `mapstructure:"upstreams"`
	Routing     RoutingConfig     `mapstructure:"routing"`
	Reliability ReliabilityConfig `mapstructure:"reliability"`
}

// UpstreamConfig represents a single upstream configuration.
type UpstreamConfig struct {
	URL    string `mapstructure:"url"`
	Weight int    `mapstructure:"weight"`
}

// RoutingConfig represents routing configuration.
type RoutingConfig struct {
	Strategy    string            `mapstructure:"strategy"`
	HealthCheck HealthCheckConfig `mapstructure:"healthCheck"`
}

// HealthCheckConfig represents health check configuration.
type HealthCheckConfig struct {
	Interval           time.Duration `mapstructure:"interval"`
	Timeout            time.Duration `mapstructure:"timeout"`
	Endpoint           string        `mapstructure:"endpoint"`
	HealthyThreshold   int           `mapstructure:"healthyThreshold"`
	UnhealthyThreshold int           `mapstructure:"unhealthyThreshold"`
}

// ReliabilityConfig represents reliability configuration.
type ReliabilityConfig struct {
	Timeout time.Duration `mapstructure:"timeout"`
	Retries int           `mapstructure:"retries"`
	Backoff time.Duration `mapstructure:"backoff"`
	Queue   QueueConfig   `mapstructure:"queue"`
}

// QueueConfig represents queue configuration.
type QueueConfig struct {
	Enabled bool          `mapstructure:"enabled"`
	MaxSize int           `mapstructure:"maxSize"`
	Timeout time.Duration `mapstructure:"timeout"`
}

// Validate validates proxy configuration.
func (c *Config) Validate() error {
	// Validate upstreams
	if len(c.Upstreams) == 0 {
		return errors.New("at least one upstream is required")
	}

	for i, upstream := range c.Upstreams {
		if err := upstream.Validate(); err != nil {
			return fmt.Errorf("upstream %d validation failed: %w", i, err)
		}
	}

	// Validate routing
	if err := c.Routing.Validate(); err != nil {
		return fmt.Errorf("routing validation failed: %w", err)
	}

	// Validate reliability
	if err := c.Reliability.Validate(); err != nil {
		return fmt.Errorf("reliability validation failed: %w", err)
	}

	return nil
}

// Validate validates upstream configuration.
func (u *UpstreamConfig) Validate() error {
	if u.URL == "" {
		return errors.New("upstream URL is required")
	}

	// Parse URL to validate format
	if _, err := url.Parse(u.URL); err != nil {
		return fmt.Errorf("invalid upstream URL: %w", err)
	}

	if u.Weight < 0 {
		return fmt.Errorf("upstream weight must be non-negative, got %d", u.Weight)
	}

	return nil
}

// Validate validates routing configuration.
func (r *RoutingConfig) Validate() error {
	// Validate strategy
	validStrategies := []string{"round-robin", "weighted-round-robin", "least-connections"}
	found := false
	for _, valid := range validStrategies {
		if r.Strategy == valid {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("invalid routing strategy: %s (valid: %v)", r.Strategy, validStrategies)
	}

	// Validate health check
	return r.HealthCheck.Validate()
}

// Validate validates health check configuration.
func (h *HealthCheckConfig) Validate() error {
	if h.Interval <= 0 {
		return fmt.Errorf("health check interval must be positive, got %v", h.Interval)
	}

	if h.Timeout <= 0 {
		return fmt.Errorf("health check timeout must be positive, got %v", h.Timeout)
	}

	if h.Timeout >= h.Interval {
		return fmt.Errorf("health check timeout (%v) must be less than interval (%v)", h.Timeout, h.Interval)
	}

	if h.HealthyThreshold <= 0 {
		return fmt.Errorf("healthy threshold must be positive, got %d", h.HealthyThreshold)
	}

	if h.UnhealthyThreshold <= 0 {
		return fmt.Errorf("unhealthy threshold must be positive, got %d", h.UnhealthyThreshold)
	}

	if h.Endpoint == "" {
		return errors.New("health check endpoint is required")
	}

	return nil
}

// Validate validates reliability configuration.
func (r *ReliabilityConfig) Validate() error {
	if r.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive, got %v", r.Timeout)
	}

	if r.Retries < 0 {
		return fmt.Errorf("retries must be non-negative, got %d", r.Retries)
	}

	if r.Backoff < 0 {
		return fmt.Errorf("backoff must be non-negative, got %v", r.Backoff)
	}

	// Validate queue
	return r.Queue.Validate()
}

// Validate validates queue configuration.
func (q *QueueConfig) Validate() error {
	if q.Enabled {
		if q.MaxSize <= 0 {
			return fmt.Errorf("queue max size must be positive when enabled, got %d", q.MaxSize)
		}

		if q.Timeout <= 0 {
			return fmt.Errorf("queue timeout must be positive when enabled, got %v", q.Timeout)
		}
	}

	return nil
}
