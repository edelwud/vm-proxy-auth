package statestorage

import (
	"fmt"

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
		// Future: Implement Raft storage
		return nil, fmt.Errorf("Raft state storage not yet implemented")

	default:
		return nil, fmt.Errorf("unsupported state storage type: %s", storageType)
	}
}
