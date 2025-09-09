package metadata

import (
	"errors"
	"fmt"
)

const (
	// MaxNodeIDLength is the maximum allowed length for a node ID.
	MaxNodeIDLength = 64
)

// Config represents metadata configuration.
type Config struct {
	NodeID      string `mapstructure:"nodeId"`
	Role        string `mapstructure:"role"`
	Environment string `mapstructure:"environment"`
	Region      string `mapstructure:"region"`
}

// Validate validates metadata configuration.
func (m *Config) Validate() error {
	if m.NodeID == "" {
		return errors.New("node ID is required")
	}
	if m.Role == "" {
		return errors.New("role is required")
	}
	if m.Environment == "" {
		return errors.New("environment is required")
	}
	if m.Region == "" {
		return errors.New("region is required")
	}

	// Validate node ID format (basic validation)
	if len(m.NodeID) > MaxNodeIDLength {
		return fmt.Errorf("node ID is too long (max %d characters): %d", MaxNodeIDLength, len(m.NodeID))
	}

	return nil
}
