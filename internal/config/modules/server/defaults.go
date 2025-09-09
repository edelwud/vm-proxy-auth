package server

import (
	"time"
)

// Default server configuration values.
const (
	DefaultAddress      = "0.0.0.0:8080"
	DefaultReadTimeout  = 30 * time.Second
	DefaultWriteTimeout = 30 * time.Second
	DefaultIdleTimeout  = 60 * time.Second
)

// GetDefaults returns default server configuration.
func GetDefaults() Config {
	return Config{
		Address: DefaultAddress,
		Timeouts: Timeouts{
			Read:  DefaultReadTimeout,
			Write: DefaultWriteTimeout,
			Idle:  DefaultIdleTimeout,
		},
	}
}
