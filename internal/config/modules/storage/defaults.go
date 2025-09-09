package storage

import (
	"time"
)

// Default storage configuration values.
const (
	DefaultType                  = "local"
	DefaultRedisDatabase         = 0
	DefaultRedisKeyPrefix        = "vm-proxy-auth:"
	DefaultRedisPoolSize         = 20
	DefaultRedisMinIdleConns     = 10
	DefaultRedisConnectTimeout   = 5 * time.Second
	DefaultRedisReadTimeout      = 3 * time.Second
	DefaultRedisWriteTimeout     = 3 * time.Second
	DefaultRedisMaxRetries       = 3
	DefaultRedisMinRetryBackoff  = 100 * time.Millisecond
	DefaultRedisMaxRetryBackoff  = 1 * time.Second
	DefaultRaftBindAddress       = "127.0.0.1:9000"
	DefaultRaftDataDir           = "/data/raft"
	DefaultRaftBootstrapExpected = 0
)

// GetDefaults returns default storage configuration.
func GetDefaults() Config {
	return Config{
		Type: DefaultType,
		Redis: RedisConfig{
			Database:  DefaultRedisDatabase,
			KeyPrefix: DefaultRedisKeyPrefix,
			Pool: RedisPoolConfig{
				Size:    DefaultRedisPoolSize,
				MinIdle: DefaultRedisMinIdleConns,
			},
			Timeouts: RedisTimeoutsConfig{
				Connect: DefaultRedisConnectTimeout,
				Read:    DefaultRedisReadTimeout,
				Write:   DefaultRedisWriteTimeout,
			},
			Retry: RedisRetryConfig{
				MaxAttempts: DefaultRedisMaxRetries,
				Backoff: BackoffConfig{
					Min: DefaultRedisMinRetryBackoff,
					Max: DefaultRedisMaxRetryBackoff,
				},
			},
		},
		Raft: RaftConfig{
			BindAddress:       DefaultRaftBindAddress,
			DataDir:           DefaultRaftDataDir,
			BootstrapExpected: DefaultRaftBootstrapExpected,
		},
	}
}
