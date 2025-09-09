package metrics

import (
	"errors"
	"strings"
)

// Config represents metrics configuration.
type Config struct {
	Enabled bool   `mapstructure:"enabled"`
	Path    string `mapstructure:"path"`
}

// Validate validates metrics configuration.
func (m *Config) Validate() error {
	if m.Enabled && m.Path == "" {
		return errors.New("metrics path is required when metrics are enabled")
	}

	if m.Path != "" && !strings.HasPrefix(m.Path, "/") {
		return errors.New("metrics path must start with /")
	}

	return nil
}
