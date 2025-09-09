package statestorage

import (
	"fmt"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/config/modules/storage"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// NewStateStorage creates a state storage instance based on configuration.
func NewStateStorage(
	storageConfig interface{},
	storageType string,
	nodeID string,
	logger domain.Logger,
) (domain.StateStorage, error) {
	switch storageType {
	case "local":
		logger.Info("Creating local state storage", domain.Field{Key: "node_id", Value: nodeID})
		return NewLocalStorage(nodeID), nil

	case "redis":
		redisConfig, ok := storageConfig.(storage.RedisConfig)
		if !ok {
			return nil, fmt.Errorf("invalid Redis configuration type: %T", storageConfig)
		}

		// Convert storage.RedisConfig to RedisStorageConfig
		storageConfigRedis := RedisStorageConfig{
			Address:         redisConfig.Address,
			Password:        redisConfig.Password,
			Database:        redisConfig.Database,
			KeyPrefix:       redisConfig.KeyPrefix,
			ConnectTimeout:  redisConfig.Timeouts.Connect,
			ReadTimeout:     redisConfig.Timeouts.Read,
			WriteTimeout:    redisConfig.Timeouts.Write,
			PoolSize:        redisConfig.Pool.Size,
			MinIdleConns:    redisConfig.Pool.MinIdle,
			MaxRetries:      redisConfig.Retry.MaxAttempts,
			MinRetryBackoff: redisConfig.Retry.Backoff.Min,
			MaxRetryBackoff: redisConfig.Retry.Backoff.Max,
		}

		logger.Info("Creating Redis state storage",
			domain.Field{Key: "address", Value: redisConfig.Address},
			domain.Field{Key: "database", Value: redisConfig.Database},
			domain.Field{Key: "node_id", Value: nodeID})

		return NewRedisStorage(storageConfigRedis, nodeID, logger)

	case "raft":
		raftConfig, ok := storageConfig.(storage.RaftConfig)
		if !ok {
			return nil, fmt.Errorf("invalid Raft configuration type: %T", storageConfig)
		}

		// Convert storage.RaftConfig to RaftStorageConfig with defaults
		storageConfigRaft := RaftStorageConfig{
			NodeID:             nodeID, // Use provided nodeID
			BindAddress:        raftConfig.BindAddress,
			DataDir:            raftConfig.DataDir,
			Peers:              raftConfig.Peers,
			BootstrapExpected:  raftConfig.BootstrapExpected,
			HeartbeatTimeout:   1 * time.Second,
			ElectionTimeout:    1 * time.Second,
			LeaderLeaseTimeout: storage.DefaultLeaderLeaseTimeout,
			CommitTimeout:      storage.DefaultCommitTimeout,
			SnapshotRetention:  storage.DefaultSnapshotRetention,
			SnapshotThreshold:  storage.DefaultSnapshotThreshold,
			TrailingLogs:       storage.DefaultTrailingLogs,
		}

		logger.Info("Creating Raft state storage",
			domain.Field{Key: "node_id", Value: nodeID},
			domain.Field{Key: "data_dir", Value: raftConfig.DataDir},
			domain.Field{Key: "peers_count", Value: len(raftConfig.Peers)})

		return NewRaftStorage(storageConfigRaft, nodeID, logger)

	default:
		return nil, fmt.Errorf("unsupported state storage type: %s", storageType)
	}
}
