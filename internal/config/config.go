package config

import (
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

// Config represents the complete application configuration structure.
type Config struct {
	// Shared metadata (only necessary for inter-service communication)
	Metadata metadata.Config `mapstructure:"metadata"`

	// Logging configuration (top level)
	Logging logging.Config `mapstructure:"logging"`

	// Metrics configuration (top level)
	Metrics metrics.Config `mapstructure:"metrics"`

	// Server configuration
	Server server.Config `mapstructure:"server"`

	// Proxy configuration
	Proxy proxy.Config `mapstructure:"proxy"`

	// Authentication configuration
	Auth auth.Config `mapstructure:"auth"`

	// Tenant management configuration
	Tenants tenant.Config `mapstructure:"tenants"`

	// Storage configuration
	Storage storage.Config `mapstructure:"storage"`

	// Cluster configuration
	Cluster cluster.Config `mapstructure:"cluster"`
}

// Validate validates the complete configuration.
func (c *Config) Validate() error {
	// Validate server configuration
	if err := c.Server.Validate(); err != nil {
		return err
	}

	// Validate proxy configuration
	if err := c.Proxy.Validate(); err != nil {
		return err
	}

	// Validate auth configuration
	if err := c.Auth.Validate(); err != nil {
		return err
	}

	// Validate tenant configuration
	if err := c.Tenants.Validate(); err != nil {
		return err
	}

	// Validate storage configuration
	if err := c.Storage.Validate(); err != nil {
		return err
	}

	// Validate cluster configuration (if storage type is raft)
	if c.Storage.Type == "raft" {
		if err := c.Cluster.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// IsClusterEnabled returns true if cluster functionality should be enabled.
func (c *Config) IsClusterEnabled() bool {
	return c.Storage.Type == "raft"
}
