package server

import (
	"errors"
	"fmt"
	"net"
	"time"
)

// Config represents server configuration.
type Config struct {
	Address  string   `mapstructure:"address"`
	Timeouts Timeouts `mapstructure:"timeouts"`
}

// Timeouts represents server timeout configuration.
type Timeouts struct {
	Read  time.Duration `mapstructure:"read"`
	Write time.Duration `mapstructure:"write"`
	Idle  time.Duration `mapstructure:"idle"`
}

// Validate validates server configuration.
func (c *Config) Validate() error {
	if c.Address == "" {
		return errors.New("server address is required")
	}

	// Validate address format
	if _, _, err := net.SplitHostPort(c.Address); err != nil {
		return fmt.Errorf("invalid server address format: %w", err)
	}

	// Validate timeouts
	if c.Timeouts.Read <= 0 {
		return fmt.Errorf("server read timeout must be positive, got %v", c.Timeouts.Read)
	}

	if c.Timeouts.Write <= 0 {
		return fmt.Errorf("server write timeout must be positive, got %v", c.Timeouts.Write)
	}

	if c.Timeouts.Idle <= 0 {
		return fmt.Errorf("server idle timeout must be positive, got %v", c.Timeouts.Idle)
	}

	return nil
}
