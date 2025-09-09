package storage

import (
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// Default storage configuration values.
const (
	DefaultRaftBindAddress        = "127.0.0.1:9000"
	DefaultRaftDataDir            = "/data/raft"
	DefaultRaftBootstrapExpected  = 0
	DefaultWatchChannelBufferSize = 100
	DefaultRaftMaxConnections     = 3
	DefaultRaftApplyTimeout       = 10 * time.Second
	DefaultRaftApplyTimeoutBulk   = 30 * time.Second
	DefaultRaftWatchChannelSize   = 100
	DefaultRaftMinPeerParts       = 2
	DefaultLeaderLeaseTimeout     = 500 * time.Millisecond
	DefaultCommitTimeout          = 50 * time.Millisecond
	DefaultSnapshotRetention      = 3
	DefaultSnapshotThreshold      = 1024
	DefaultTrailingLogs           = 1024
	DefaultRedisDatabase          = 0
	DefaultRedisKeyPrefix         = "vm-proxy-auth:"
	DefaultRedisPoolSize          = 20
	DefaultRedisMinIdleConns      = 10
	DefaultRedisConnectTimeout    = 5 * time.Second
	DefaultRedisReadTimeout       = 3 * time.Second
	DefaultRedisWriteTimeout      = 3 * time.Second
	DefaultRedisMaxRetries        = 3
	DefaultRedisMinRetryBackoff   = 100 * time.Millisecond
	DefaultRedisMaxRetryBackoff   = 1 * time.Second
	DefaultRedisPubSubChannel     = "vm-proxy-auth:events"
	DefaultRedisConnMaxIdleTime   = 30 * time.Minute
	DefaultRedisReceiveTimeout    = 100 * time.Millisecond
)

// GetDefaults returns default storage configuration.
func GetDefaults() Config {
	return Config{
		Type: string(domain.StateStorageTypeLocal),
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
