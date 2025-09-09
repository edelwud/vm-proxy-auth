package logging

import (
	"fmt"
	"slices"
)

// Config represents logging configuration.
type Config struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// Validate validates logging configuration.
func (l *Config) Validate() error {
	validLevels := []string{"debug", "info", "warn", "error"}
	if !slices.Contains(validLevels, l.Level) {
		return fmt.Errorf("invalid log level: %s (valid: %v)", l.Level, validLevels)
	}

	validFormats := []string{"json", "text"}
	if !slices.Contains(validFormats, l.Format) {
		return fmt.Errorf("invalid log format: %s (valid: %v)", l.Format, validFormats)
	}

	return nil
}
