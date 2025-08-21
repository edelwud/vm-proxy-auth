package domain

import "time"

// Default timeout and duration constants.
const (
	// HTTP timeouts.
	DefaultReadTimeout  = 30 * time.Second
	DefaultWriteTimeout = 30 * time.Second
	DefaultIdleTimeout  = 60 * time.Second
	DefaultTimeout      = 30 * time.Second

	// Cache settings.
	DefaultCacheTTL = 5 * time.Minute
	DefaultTokenTTL = time.Hour

	// Retry settings.
	DefaultMaxRetries = 3
	DefaultRetryDelay = time.Second

	// Shutdown timeout.
	DefaultShutdownTimeout = 30 * time.Second

	// Test loop counts.
	DefaultTestRetries = 5
	DefaultTestCount   = 3
	DefaultBenchCount  = 10
)
