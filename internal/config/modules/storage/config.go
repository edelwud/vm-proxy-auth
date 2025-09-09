package storage

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// Config represents storage configuration.
type Config struct {
	Type  string      `mapstructure:"type"`
	Redis RedisConfig `mapstructure:"redis,omitempty"`
	Raft  RaftConfig  `mapstructure:"raft,omitempty"`
}

// RedisConfig represents Redis storage configuration.
type RedisConfig struct {
	Address   string              `mapstructure:"address"`
	Password  string              `mapstructure:"password"`
	Database  int                 `mapstructure:"database"`
	KeyPrefix string              `mapstructure:"keyPrefix"`
	Pool      RedisPoolConfig     `mapstructure:"pool"`
	Timeouts  RedisTimeoutsConfig `mapstructure:"timeouts"`
	Retry     RedisRetryConfig    `mapstructure:"retry"`
}

// RedisPoolConfig represents Redis connection pool configuration.
type RedisPoolConfig struct {
	Size    int `mapstructure:"size"`
	MinIdle int `mapstructure:"minIdle"`
}

// RedisTimeoutsConfig represents Redis timeout configuration.
type RedisTimeoutsConfig struct {
	Connect time.Duration `mapstructure:"connect"`
	Read    time.Duration `mapstructure:"read"`
	Write   time.Duration `mapstructure:"write"`
}

// RedisRetryConfig represents Redis retry configuration.
type RedisRetryConfig struct {
	MaxAttempts int           `mapstructure:"maxAttempts"`
	Backoff     BackoffConfig `mapstructure:"backoff"`
}

// BackoffConfig represents backoff configuration.
type BackoffConfig struct {
	Min time.Duration `mapstructure:"min"`
	Max time.Duration `mapstructure:"max"`
}

// RaftConfig represents Raft storage configuration.
type RaftConfig struct {
	BindAddress       string   `mapstructure:"bindAddress"`
	DataDir           string   `mapstructure:"dataDir"`
	BootstrapExpected int      `mapstructure:"bootstrapExpected"`
	Peers             []string `mapstructure:"peers,omitempty"` // Optional
}

// Validate validates storage configuration.
func (c *Config) Validate() error {
	validTypes := []string{
		string(domain.StateStorageTypeLocal),
		string(domain.StateStorageTypeRedis),
		string(domain.StateStorageTypeRaft),
	}
	if !slices.Contains(validTypes, c.Type) {
		return fmt.Errorf("invalid storage type: %s (valid: %v)", c.Type, validTypes)
	}

	switch domain.StateStorageType(c.Type) {
	case domain.StateStorageTypeRedis:
		return c.Redis.Validate()
	case domain.StateStorageTypeRaft:
		return c.Raft.Validate()
	case domain.StateStorageTypeLocal:
		// Local storage doesn't need validation
		return nil
	default:
		return fmt.Errorf("unsupported storage type: %s", c.Type)
	}
}

// Validate validates Redis configuration.
func (r *RedisConfig) Validate() error {
	if r.Address == "" {
		return errors.New("redis address is required")
	}

	if r.Database < 0 {
		return fmt.Errorf("redis database must be non-negative, got %d", r.Database)
	}

	// Validate pool configuration
	if err := r.Pool.Validate(); err != nil {
		return fmt.Errorf("redis pool validation failed: %w", err)
	}

	// Validate timeouts
	if err := r.Timeouts.Validate(); err != nil {
		return fmt.Errorf("redis timeouts validation failed: %w", err)
	}

	// Validate retry configuration
	if err := r.Retry.Validate(); err != nil {
		return fmt.Errorf("redis retry validation failed: %w", err)
	}

	return nil
}

// Validate validates Redis pool configuration.
func (p *RedisPoolConfig) Validate() error {
	if p.Size <= 0 {
		return fmt.Errorf("redis pool size must be positive, got %d", p.Size)
	}

	if p.MinIdle < 0 {
		return fmt.Errorf("redis min idle connections cannot be negative, got %d", p.MinIdle)
	}

	if p.MinIdle > p.Size {
		return fmt.Errorf("redis min idle connections (%d) cannot exceed pool size (%d)", p.MinIdle, p.Size)
	}

	return nil
}

// Validate validates Redis timeouts configuration.
func (t *RedisTimeoutsConfig) Validate() error {
	if t.Connect <= 0 {
		return fmt.Errorf("redis connect timeout must be positive, got %v", t.Connect)
	}

	if t.Read <= 0 {
		return fmt.Errorf("redis read timeout must be positive, got %v", t.Read)
	}

	if t.Write <= 0 {
		return fmt.Errorf("redis write timeout must be positive, got %v", t.Write)
	}

	return nil
}

// Validate validates Redis retry configuration.
func (r *RedisRetryConfig) Validate() error {
	if r.MaxAttempts < 0 {
		return fmt.Errorf("redis max retry attempts cannot be negative, got %d", r.MaxAttempts)
	}

	if r.Backoff.Min < 0 {
		return fmt.Errorf("redis min retry backoff cannot be negative, got %v", r.Backoff.Min)
	}

	if r.Backoff.Max < 0 {
		return fmt.Errorf("redis max retry backoff cannot be negative, got %v", r.Backoff.Max)
	}

	if r.Backoff.Min > r.Backoff.Max {
		return fmt.Errorf("redis min retry backoff (%v) cannot exceed max backoff (%v)", r.Backoff.Min, r.Backoff.Max)
	}

	return nil
}

// Validate validates Raft configuration.
func (r *RaftConfig) Validate() error {
	if r.BindAddress == "" {
		return errors.New("raft bind address is required")
	}

	if r.DataDir == "" {
		return errors.New("raft data directory is required")
	}

	if r.BootstrapExpected < 0 {
		return fmt.Errorf("raft bootstrap expected must be non-negative, got %d", r.BootstrapExpected)
	}

	// If both bootstrap expected and static peers are specified, it's an error
	if r.BootstrapExpected > 0 && len(r.Peers) > 0 {
		return errors.New("cannot specify both bootstrap expected and static peers for raft")
	}

	return nil
}
