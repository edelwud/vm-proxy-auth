package statestorage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

// TestRedisStorageConfig_Validation tests Redis configuration validation.
func TestRedisStorageConfig_Validation(t *testing.T) {
	tests := []struct {
		name        string
		config      RedisStorageConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid_config",
			config: RedisStorageConfig{
				Address:        "localhost:6379",
				Database:       0,
				KeyPrefix:      "test:",
				ConnectTimeout: 5 * time.Second,
				ReadTimeout:    3 * time.Second,
				WriteTimeout:   3 * time.Second,
				PoolSize:       10,
				MinIdleConns:   2,
			},
			expectError: false,
		},
		{
			name: "missing_address",
			config: RedisStorageConfig{
				Database:       0,
				ConnectTimeout: 5 * time.Second,
			},
			expectError: true,
			errorMsg:    "redis address is required",
		},
		{
			name: "invalid_pool_config",
			config: RedisStorageConfig{
				Address:      "localhost:6379",
				PoolSize:     5,
				MinIdleConns: 10, // More than pool size
			},
			expectError: false, // Constructor will fix this
		},
	}

	logger := testutils.NewMockLogger()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRedisStorage(tt.config, "test-node", logger)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else if err != nil {
				// For valid configs, we expect connection failure in tests
				// but not validation errors
				assert.Contains(t, err.Error(), "failed to connect to Redis")
			}
		})
	}
}

// TestRedisStorage_GetSet tests basic get/set operations.
// Note: This test requires a Redis instance running.
func TestRedisStorage_GetSet(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Redis integration test in short mode")
	}

	config := RedisStorageConfig{
		Address:        "localhost:6379",
		Database:       1, // Use test database
		KeyPrefix:      "test:",
		ConnectTimeout: 2 * time.Second,
		ReadTimeout:    1 * time.Second,
		WriteTimeout:   1 * time.Second,
		PoolSize:       5,
		MinIdleConns:   1,
	}

	logger := testutils.NewMockLogger()
	storage, err := NewRedisStorage(config, "test-node-1", logger)
	// Skip test if Redis is not available
	if err != nil {
		t.Skipf("Redis not available: %v", err)
		return
	}
	defer storage.Close()

	ctx := context.Background()

	t.Run("basic_get_set", func(t *testing.T) {
		key := "test-key"
		value := []byte("test-value")

		// Set value
		err := storage.Set(ctx, key, value, 0)
		require.NoError(t, err)

		// Get value
		result, err := storage.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, result)

		// Delete value
		err = storage.Delete(ctx, key)
		require.NoError(t, err)

		// Verify deleted
		_, err = storage.Get(ctx, key)
		assert.ErrorIs(t, err, domain.ErrKeyNotFound)
	})

	t.Run("ttl_expiration", func(t *testing.T) {
		key := "ttl-key"
		value := []byte("ttl-value")
		ttl := 100 * time.Millisecond

		// Set with TTL
		err := storage.Set(ctx, key, value, ttl)
		require.NoError(t, err)

		// Should exist immediately
		result, err := storage.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, result)

		// Wait for expiration
		time.Sleep(150 * time.Millisecond)

		// Should be expired
		_, err = storage.Get(ctx, key)
		assert.ErrorIs(t, err, domain.ErrKeyNotFound)
	})

	t.Run("nonexistent_key", func(t *testing.T) {
		_, err := storage.Get(ctx, "nonexistent-key")
		assert.ErrorIs(t, err, domain.ErrKeyNotFound)
	})
}

// TestRedisStorage_MultipleOperations tests bulk operations.
func TestRedisStorage_MultipleOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Redis integration test in short mode")
	}

	config := RedisStorageConfig{
		Address:        "localhost:6379",
		Database:       1, // Use test database
		KeyPrefix:      "test:",
		ConnectTimeout: 2 * time.Second,
		ReadTimeout:    1 * time.Second,
		WriteTimeout:   1 * time.Second,
		PoolSize:       5,
		MinIdleConns:   1,
	}

	logger := testutils.NewMockLogger()
	storage, err := NewRedisStorage(config, "test-node-2", logger)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
		return
	}
	defer storage.Close()

	ctx := context.Background()

	t.Run("set_multiple", func(t *testing.T) {
		items := map[string][]byte{
			"key1": []byte("value1"),
			"key2": []byte("value2"),
			"key3": []byte("value3"),
		}

		// Set multiple
		err := storage.SetMultiple(ctx, items, 0)
		require.NoError(t, err)

		// Verify all values
		for key, expectedValue := range items {
			result, getErr := storage.Get(ctx, key)
			require.NoError(t, getErr)
			assert.Equal(t, expectedValue, result)
		}

		// Clean up
		for key := range items {
			storage.Delete(ctx, key)
		}
	})

	t.Run("get_multiple", func(t *testing.T) {
		items := map[string][]byte{
			"multi-key1": []byte("multi-value1"),
			"multi-key2": []byte("multi-value2"),
			"multi-key3": []byte("multi-value3"),
		}

		// Set up test data
		err := storage.SetMultiple(ctx, items, 0)
		require.NoError(t, err)

		// Get multiple including non-existent key
		keys := []string{"multi-key1", "multi-key2", "nonexistent", "multi-key3"}
		results, err := storage.GetMultiple(ctx, keys)
		require.NoError(t, err)

		// Should get 3 out of 4 keys (excluding nonexistent)
		assert.Len(t, results, 3)
		assert.Equal(t, items["multi-key1"], results["multi-key1"])
		assert.Equal(t, items["multi-key2"], results["multi-key2"])
		assert.Equal(t, items["multi-key3"], results["multi-key3"])
		assert.NotContains(t, results, "nonexistent")

		// Clean up
		for key := range items {
			storage.Delete(ctx, key)
		}
	})

	t.Run("empty_operations", func(t *testing.T) {
		// Empty set multiple
		err := storage.SetMultiple(ctx, map[string][]byte{}, 0)
		assert.NoError(t, err)

		// Empty get multiple
		results, err := storage.GetMultiple(ctx, []string{})
		assert.NoError(t, err)
		assert.Empty(t, results)
	})
}

// TestRedisStorage_Watch tests the watch functionality.
func TestRedisStorage_Watch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Redis integration test in short mode")
	}

	config := RedisStorageConfig{
		Address:        "localhost:6379",
		Database:       1,
		KeyPrefix:      "test:",
		ConnectTimeout: 2 * time.Second,
		ReadTimeout:    1 * time.Second,
		WriteTimeout:   1 * time.Second,
		PoolSize:       5,
		MinIdleConns:   1,
	}

	logger := testutils.NewMockLogger()

	// Create two separate storage instances to test distributed events
	storage1, err := NewRedisStorage(config, "test-node-1", logger)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
		return
	}
	defer storage1.Close()

	storage2, err := NewRedisStorage(config, "test-node-2", logger)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
		return
	}
	defer storage2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("watch_events", func(t *testing.T) {
		// Start watching from storage1
		eventCh, err := storage1.Watch(ctx, "watch-test:")
		require.NoError(t, err)

		// Allow time for PubSub setup
		time.Sleep(100 * time.Millisecond)

		// Perform operations from storage2
		key := "watch-test:key1"
		value := []byte("watch-value")

		go func() {
			time.Sleep(50 * time.Millisecond) // Small delay to ensure watcher is ready
			storage2.Set(ctx, key, value, 0)
		}()

		// Should receive set event
		select {
		case event := <-eventCh:
			assert.Equal(t, domain.StateEventSet, event.Type)
			assert.Equal(t, key, event.Key)
			assert.Equal(t, value, event.Value)
			assert.Equal(t, "test-node-2", event.NodeID)
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for set event")
		}

		// Delete the key
		go func() {
			time.Sleep(50 * time.Millisecond)
			storage2.Delete(ctx, key)
		}()

		// Should receive delete event
		select {
		case event := <-eventCh:
			assert.Equal(t, domain.StateEventDelete, event.Type)
			assert.Equal(t, key, event.Key)
			assert.Equal(t, "test-node-2", event.NodeID)
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for delete event")
		}
	})
}

// TestRedisStorage_ConnectionHandling tests connection management.
func TestRedisStorage_ConnectionHandling(t *testing.T) {
	logger := testutils.NewMockLogger()

	t.Run("invalid_address", func(t *testing.T) {
		config := RedisStorageConfig{
			Address:        "invalid:12345",
			ConnectTimeout: 1 * time.Second,
		}

		_, err := NewRedisStorage(config, "test-node", logger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to connect to Redis")
	})

	t.Run("missing_address", func(t *testing.T) {
		config := RedisStorageConfig{
			Address: "",
		}

		_, err := NewRedisStorage(config, "test-node", logger)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "redis address is required")
	})

	t.Run("ping_after_close", func(t *testing.T) {
		// Skip if Redis not available
		config := RedisStorageConfig{
			Address:        "localhost:6379",
			Database:       1,
			ConnectTimeout: 1 * time.Second,
		}

		storage, err := NewRedisStorage(config, "test-node", logger)
		if err != nil {
			t.Skipf("Redis not available: %v", err)
			return
		}

		// Ping should work
		ctx := context.Background()
		err = storage.Ping(ctx)
		require.NoError(t, err)

		// Close storage
		err = storage.Close()
		require.NoError(t, err)

		// Ping should fail after close
		err = storage.Ping(ctx)
		assert.ErrorIs(t, err, domain.ErrStorageUnavailable)
	})
}

// TestRedisStorage_ConcurrentOperations tests thread safety.
func TestRedisStorage_ConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Redis integration test in short mode")
	}

	config := RedisStorageConfig{
		Address:        "localhost:6379",
		Database:       1,
		KeyPrefix:      "concurrent:",
		ConnectTimeout: 2 * time.Second,
		PoolSize:       20, // Larger pool for concurrency
		MinIdleConns:   5,
	}

	logger := testutils.NewMockLogger()
	storage, err := NewRedisStorage(config, "concurrent-node", logger)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
		return
	}
	defer storage.Close()

	ctx := context.Background()

	t.Run("concurrent_set_get", func(t *testing.T) {
		const numRoutines = 10
		const opsPerRoutine = 5

		// Run concurrent set/get operations
		errChan := make(chan error, numRoutines*opsPerRoutine*2)

		for i := range numRoutines {
			go func(routineID int) {
				for j := range opsPerRoutine {
					key := fmt.Sprintf("concurrent-key-%d-%d", routineID, j)
					value := []byte(fmt.Sprintf("value-%d-%d", routineID, j))

					// Set
					if setErr := storage.Set(ctx, key, value, 0); setErr != nil {
						errChan <- setErr
						return
					}

					// Get
					result, getErr := storage.Get(ctx, key)
					if getErr != nil {
						errChan <- getErr
						return
					}

					if !assert.Equal(t, value, result) {
						errChan <- fmt.Errorf("value mismatch for key %s", key)
						return
					}

					// Clean up
					storage.Delete(ctx, key)
				}
			}(i)
		}

		// Wait briefly and check for errors
		time.Sleep(2 * time.Second)
		close(errChan)

		var errors []error
		for err := range errChan {
			errors = append(errors, err)
		}

		if len(errors) > 0 {
			t.Fatalf("Concurrent operations failed with %d errors, first: %v", len(errors), errors[0])
		}
	})
}

// BenchmarkRedisStorage_Operations benchmarks Redis operations.
func BenchmarkRedisStorage_Operations(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping Redis benchmark in short mode")
	}

	config := RedisStorageConfig{
		Address:        "localhost:6379",
		Database:       1,
		KeyPrefix:      "bench:",
		ConnectTimeout: 2 * time.Second,
		PoolSize:       20,
		MinIdleConns:   5,
	}

	logger := testutils.NewMockLogger()
	storage, err := NewRedisStorage(config, "benchmark-node", logger)
	if err != nil {
		b.Skipf("Redis not available: %v", err)
		return
	}
	defer storage.Close()

	ctx := context.Background()
	key := "benchmark-key"
	value := []byte("benchmark-value-with-some-reasonable-length-for-realistic-testing")

	b.Run("Set", func(b *testing.B) {
		b.ResetTimer()
		for i := range b.N {
			err := storage.Set(ctx, fmt.Sprintf("%s-%d", key, i), value, 0)
			if err != nil {
				b.Fatalf("Set failed: %v", err)
			}
		}
	})

	b.Run("Get", func(b *testing.B) {
		// Setup data
		testKey := "get-benchmark-key"
		storage.Set(ctx, testKey, value, 0)

		b.ResetTimer()
		for range b.N {
			_, err := storage.Get(ctx, testKey)
			if err != nil {
				b.Fatalf("Get failed: %v", err)
			}
		}
	})

	b.Run("Pipeline_SetMultiple", func(b *testing.B) {
		items := make(map[string][]byte)
		for i := range 10 {
			items[fmt.Sprintf("pipeline-key-%d", i)] = []byte(fmt.Sprintf("pipeline-value-%d", i))
		}

		b.ResetTimer()
		for range b.N {
			err := storage.SetMultiple(ctx, items, 0)
			if err != nil {
				b.Fatalf("SetMultiple failed: %v", err)
			}
		}
	})
}

// TestRedisStorage_EdgeCases tests edge cases and error conditions.
func TestRedisStorage_EdgeCases(t *testing.T) {
	logger := testutils.NewMockLogger()

	t.Run("operations_after_close", func(t *testing.T) {
		// Use a config that won't connect to test close behavior
		config := RedisStorageConfig{
			Address:        "localhost:6379",
			Database:       1,
			ConnectTimeout: 1 * time.Second,
		}

		storage, err := NewRedisStorage(config, "edge-case-node", logger)
		if err != nil {
			t.Skipf("Redis not available: %v", err)
			return
		}

		// Close storage
		closeErr := storage.Close()
		require.NoError(t, closeErr)

		ctx := context.Background()

		// All operations should return ErrStorageUnavailable
		_, err = storage.Get(ctx, "test-key")
		assert.ErrorIs(t, err, domain.ErrStorageUnavailable)

		err = storage.Set(ctx, "test-key", []byte("value"), 0)
		assert.ErrorIs(t, err, domain.ErrStorageUnavailable)

		err = storage.Delete(ctx, "test-key")
		assert.ErrorIs(t, err, domain.ErrStorageUnavailable)

		_, err = storage.Watch(ctx, "test:")
		assert.ErrorIs(t, err, domain.ErrStorageUnavailable)

		err = storage.Ping(ctx)
		assert.ErrorIs(t, err, domain.ErrStorageUnavailable)
	})

	t.Run("double_close", func(t *testing.T) {
		config := RedisStorageConfig{
			Address:        "localhost:6379",
			Database:       1,
			ConnectTimeout: 1 * time.Second,
		}

		storage, err := NewRedisStorage(config, "double-close-node", logger)
		if err != nil {
			t.Skipf("Redis not available: %v", err)
			return
		}

		// First close
		err = storage.Close()
		require.NoError(t, err)

		// Second close should not error
		err = storage.Close()
		assert.NoError(t, err)
	})
}
