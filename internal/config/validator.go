package config

import (
	"errors"
	"fmt"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// ValidateConfig performs comprehensive configuration validation.
func ValidateConfig(config *Config) error {
	// Validate metadata
	if err := config.Metadata.Validate(); err != nil {
		return fmt.Errorf("metadata validation failed: %w", err)
	}

	// Validate logging
	if err := config.Logging.Validate(); err != nil {
		return fmt.Errorf("logging validation failed: %w", err)
	}

	// Validate metrics
	if err := config.Metrics.Validate(); err != nil {
		return fmt.Errorf("metrics validation failed: %w", err)
	}

	// Validate modules using their own validation methods
	if err := config.Server.Validate(); err != nil {
		return fmt.Errorf("server validation failed: %w", err)
	}

	if err := config.Proxy.Validate(); err != nil {
		return fmt.Errorf("proxy validation failed: %w", err)
	}

	if err := config.Auth.Validate(); err != nil {
		return fmt.Errorf("auth validation failed: %w", err)
	}

	if err := config.Tenants.Validate(); err != nil {
		return fmt.Errorf("tenants validation failed: %w", err)
	}

	if err := config.Storage.Validate(); err != nil {
		return fmt.Errorf("storage validation failed: %w", err)
	}

	// Validate cluster only if storage type is raft
	if config.IsClusterEnabled() {
		if err := config.Cluster.Validate(); err != nil {
			return fmt.Errorf("cluster validation failed: %w", err)
		}
	}

	// Cross-module validation
	if err := validateCrossModule(config); err != nil {
		return fmt.Errorf("cross-module validation failed: %w", err)
	}

	return nil
}

// validateCrossModule validates cross-module dependencies.
func validateCrossModule(config *Config) error {
	// Note: If cluster is enabled and MDNS is enabled:
	// - empty hostname uses metadata.nodeId automatically
	// - different hostname from nodeId is allowed (not an error)

	// Validate that auth configuration is consistent
	if config.Auth.JWT.Algorithm == "RS256" && config.Auth.JWT.JwksURL == "" {
		return errors.New("RS256 algorithm requires jwksUrl to be set")
	}

	if config.Auth.JWT.Algorithm == "HS256" && config.Auth.JWT.Secret == "" {
		return errors.New("HS256 algorithm requires secret to be set")
	}

	// Validate that proxy has at least one upstream
	if len(config.Proxy.Upstreams) == 0 {
		return errors.New("at least one proxy upstream is required")
	}

	// Validate storage-specific configurations
	switch domain.StateStorageType(config.Storage.Type) {
	case domain.StateStorageTypeRedis:
		if config.Storage.Redis.Address == "" {
			return errors.New("redis address is required when storage type is redis")
		}
	case domain.StateStorageTypeRaft:
		if config.Storage.Raft.DataDir == "" {
			return errors.New("raft data directory is required when storage type is raft")
		}
		if config.Storage.Raft.BindAddress == "" {
			return errors.New("raft bind address is required when storage type is raft")
		}
	case domain.StateStorageTypeLocal:
		// Local storage doesn't need additional validation
	default:
		return fmt.Errorf("unsupported storage type: %s", config.Storage.Type)
	}

	return nil
}
