package statestorage_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config/modules/storage"
	"github.com/edelwud/vm-proxy-auth/internal/services/statestorage"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestNewStateStorage(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()

	t.Run("local_storage", func(t *testing.T) {
		t.Parallel()
		storage, err := statestorage.NewStateStorage(nil, "local", "test-node", logger)
		require.NoError(t, err)
		assert.NotNil(t, storage)
		t.Cleanup(func() { storage.Close() })
	})

	t.Run("redis_storage_valid_config", func(t *testing.T) {
		t.Parallel()
		redisConfig := storage.RedisConfig{
			Address:   "localhost:6379",
			Database:  0,
			KeyPrefix: "test:",
			Pool: storage.RedisPoolConfig{
				Size:    10,
				MinIdle: 1,
			},
			Timeouts: storage.RedisTimeoutsConfig{
				Connect: 5 * time.Second,
				Read:    3 * time.Second,
				Write:   3 * time.Second,
			},
			Retry: storage.RedisRetryConfig{
				MaxAttempts: 3,
				Backoff: storage.BackoffConfig{
					Min: 100 * time.Millisecond,
					Max: 1 * time.Second,
				},
			},
		}

		// This will fail connection but config validation should pass
		storage, err := statestorage.NewStateStorage(redisConfig, "redis", "test-node", logger)
		if err != nil {
			// Expected in test environment without Redis
			assert.Contains(t, err.Error(), "failed to connect to Redis")
		} else {
			assert.NotNil(t, storage)
			t.Cleanup(func() { storage.Close() })
		}
	})

	t.Run("redis_storage_invalid_config_type", func(t *testing.T) {
		t.Parallel()
		invalidConfig := "not-a-redis-config"
		storage, err := statestorage.NewStateStorage(invalidConfig, "redis", "test-node", logger)
		require.Error(t, err)
		assert.Nil(t, storage)
		assert.Contains(t, err.Error(), "invalid Redis configuration type")
	})

	t.Run("raft_storage_not_implemented", func(t *testing.T) {
		t.Parallel()
		storage, err := statestorage.NewStateStorage(nil, "raft", "test-node", logger)
		require.Error(t, err)
		assert.Nil(t, storage)
		assert.Contains(t, err.Error(), "invalid Raft configuration type")
	})

	t.Run("unsupported_storage_type", func(t *testing.T) {
		t.Parallel()
		storage, err := statestorage.NewStateStorage(nil, "unsupported", "test-node", logger)
		require.Error(t, err)
		assert.Nil(t, storage)
		assert.Contains(t, err.Error(), "unsupported state storage type: unsupported")
	})
}
