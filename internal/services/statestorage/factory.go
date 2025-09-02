package statestorage

import (
	"fmt"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/config"
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
		redisConfig, ok := storageConfig.(config.RedisSettings)
		if !ok {
			return nil, fmt.Errorf("invalid Redis configuration type: %T", storageConfig)
		}

		// Convert config.RedisSettings to RedisStorageConfig
		storageConfigRedis := RedisStorageConfig{
			Address:         redisConfig.Address,
			Password:        redisConfig.Password,
			Database:        redisConfig.Database,
			KeyPrefix:       redisConfig.KeyPrefix,
			ConnectTimeout:  redisConfig.ConnectTimeout,
			ReadTimeout:     redisConfig.ReadTimeout,
			WriteTimeout:    redisConfig.WriteTimeout,
			PoolSize:        redisConfig.PoolSize,
			MinIdleConns:    redisConfig.MinIdleConns,
			MaxRetries:      redisConfig.MaxRetries,
			MinRetryBackoff: redisConfig.MinRetryBackoff,
			MaxRetryBackoff: redisConfig.MaxRetryBackoff,
		}

		logger.Info("Creating Redis state storage",
			domain.Field{Key: "address", Value: redisConfig.Address},
			domain.Field{Key: "database", Value: redisConfig.Database},
			domain.Field{Key: "node_id", Value: nodeID})

		return NewRedisStorage(storageConfigRedis, nodeID, logger)

	case "raft":
		raftConfig, ok := storageConfig.(config.RaftSettings)
		if !ok {
			return nil, fmt.Errorf("invalid Raft configuration type: %T", storageConfig)
		}

		// Convert config.RaftSettings to RaftStorageConfig with defaults
		storageConfigRaft := RaftStorageConfig{
			NodeID:             raftConfig.NodeID,
			BindAddress:        "127.0.0.1:9000", // Default bind address
			DataDir:            raftConfig.DataDir,
			Peers:              raftConfig.Peers,
			PeerDiscovery:      &raftConfig.PeerDiscovery,
			HeartbeatTimeout:   1 * time.Second,
			ElectionTimeout:    1 * time.Second,
			LeaderLeaseTimeout: domain.DefaultLeaderLeaseTimeout,
			CommitTimeout:      domain.DefaultCommitTimeout,
			SnapshotRetention:  domain.DefaultSnapshotRetention,
			SnapshotThreshold:  domain.DefaultSnapshotThreshold,
			TrailingLogs:       domain.DefaultTrailingLogs,
		}

		logger.Info("Creating Raft state storage",
			domain.Field{Key: "node_id", Value: raftConfig.NodeID},
			domain.Field{Key: "data_dir", Value: raftConfig.DataDir},
			domain.Field{Key: "peers_count", Value: len(raftConfig.Peers)})

		return NewRaftStorage(storageConfigRaft, nodeID, logger)

	default:
		return nil, fmt.Errorf("unsupported state storage type: %s", storageType)
	}
}
