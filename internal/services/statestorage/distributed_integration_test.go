package statestorage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

// TestDistributedStateEventPropagation tests that state events propagate
// correctly between multiple storage instances.
func TestDistributedStateEventPropagation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping distributed integration test in short mode")
	}

	config := RedisStorageConfig{
		Address:        "localhost:6379",
		Database:       2, // Use separate test database
		KeyPrefix:      "distributed-test:",
		ConnectTimeout: 2 * time.Second,
		ReadTimeout:    1 * time.Second,
		WriteTimeout:   1 * time.Second,
		PoolSize:       5,
		MinIdleConns:   1,
	}

	logger := testutils.NewMockLogger()

	// Create two separate storage instances to simulate distributed deployment
	storage1, err := NewRedisStorage(config, "node-1", logger)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
		return
	}
	defer storage1.Close()

	storage2, err := NewRedisStorage(config, "node-2", logger)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
		return
	}
	defer storage2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("set_event_propagation", func(t *testing.T) {
		// Start watching from storage2
		eventCh, watchErr := storage2.Watch(ctx, "distributed-test:set:")
		require.NoError(t, watchErr)

		// Allow time for pub/sub setup
		time.Sleep(200 * time.Millisecond)

		// Set value from storage1
		key := "distributed-test:set:key1"
		value := []byte("distributed-value-1")

		go func() {
			time.Sleep(100 * time.Millisecond)
			setErr := storage1.Set(ctx, key, value, 0)
			require.NoError(t, setErr)
		}()

		// Should receive event on storage2
		select {
		case event := <-eventCh:
			assert.Equal(t, domain.StateEventSet, event.Type)
			assert.Equal(t, key, event.Key)
			assert.Equal(t, value, event.Value)
			assert.Equal(t, "node-1", event.NodeID)
			assert.True(t, event.IsValid())
		case <-time.After(3 * time.Second):
			t.Fatal("Timeout waiting for distributed set event")
		}

		// Verify the value is accessible from both instances
		result1, err := storage1.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, result1)

		result2, err := storage2.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, result2)
	})

	t.Run("delete_event_propagation", func(t *testing.T) {
		// Set up data first
		key := "distributed-test:delete:key1"
		value := []byte("to-be-deleted")

		err := storage1.Set(ctx, key, value, 0)
		require.NoError(t, err)

		// Start watching from storage2
		eventCh, err := storage2.Watch(ctx, "distributed-test:delete:")
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		// Delete from storage1
		go func() {
			time.Sleep(100 * time.Millisecond)
			err := storage1.Delete(ctx, key)
			require.NoError(t, err)
		}()

		// Should receive delete event on storage2
		select {
		case event := <-eventCh:
			assert.Equal(t, domain.StateEventDelete, event.Type)
			assert.Equal(t, key, event.Key)
			assert.Equal(t, "node-1", event.NodeID)
			assert.True(t, event.IsValid())
		case <-time.After(3 * time.Second):
			t.Fatal("Timeout waiting for distributed delete event")
		}

		// Verify key is deleted from both instances
		_, err = storage1.Get(ctx, key)
		assert.ErrorIs(t, err, domain.ErrKeyNotFound)

		_, err = storage2.Get(ctx, key)
		assert.ErrorIs(t, err, domain.ErrKeyNotFound)
	})

	t.Run("multiple_watchers_same_prefix", func(t *testing.T) {
		// Create multiple watchers on the same prefix from different nodes
		prefix := "distributed-test:multi:"

		eventCh1, err := storage1.Watch(ctx, prefix)
		require.NoError(t, err)

		eventCh2, err := storage2.Watch(ctx, prefix)
		require.NoError(t, err)

		time.Sleep(200 * time.Millisecond)

		// Set value from storage1
		key := prefix + "shared-key"
		value := []byte("shared-value")

		err = storage1.Set(ctx, key, value, 0)
		require.NoError(t, err)

		// Both watchers should receive the event (but node-1 watcher should skip it)
		receivedCount := 0
		timeout := time.After(2 * time.Second)

		for receivedCount < 1 {
			select {
			case event := <-eventCh2: // Only storage2 should receive events from storage1
				assert.Equal(t, domain.StateEventSet, event.Type)
				assert.Equal(t, key, event.Key)
				assert.Equal(t, "node-1", event.NodeID)
				receivedCount++
			case <-eventCh1:
				t.Error("storage1 should not receive its own events")
			case <-timeout:
				t.Fatal("Timeout waiting for distributed events")
			}
		}

		// Verify only one event was received (from the other node)
		assert.Equal(t, 1, receivedCount)
	})
}

// TestDistributedStateConsistency tests data consistency across multiple
// storage instances with concurrent operations.
func TestDistributedStateConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping distributed consistency test in short mode")
	}

	config := RedisStorageConfig{
		Address:        "localhost:6379",
		Database:       3, // Use separate test database
		KeyPrefix:      "consistency-test:",
		ConnectTimeout: 2 * time.Second,
		ReadTimeout:    1 * time.Second,
		WriteTimeout:   1 * time.Second,
		PoolSize:       10,
		MinIdleConns:   2,
	}

	logger := testutils.NewMockLogger()

	// Create three storage instances to simulate real distributed deployment
	storages := make([]*RedisStorage, 3)
	for i := range storages {
		storage, err := NewRedisStorage(config, nodeID(i), logger)
		if err != nil {
			t.Skipf("Redis not available: %v", err)
			return
		}
		defer storage.Close()
		storages[i] = storage
	}

	ctx := context.Background()

	t.Run("eventual_consistency", func(t *testing.T) {
		key := "consistency-test:eventual"
		value := []byte("consistent-value")

		// Set from first instance
		err := storages[0].Set(ctx, key, value, 0)
		require.NoError(t, err)

		// Allow time for Redis replication (should be near-instant)
		time.Sleep(50 * time.Millisecond)

		// Verify all instances can read the value
		for i, storage := range storages {
			result, err := storage.Get(ctx, key)
			require.NoError(t, err, "instance %d should read the value", i)
			assert.Equal(t, value, result, "instance %d should have consistent value", i)
		}
	})

	t.Run("concurrent_writes", func(t *testing.T) {
		baseKey := "consistency-test:concurrent:"

		// Perform concurrent writes from different instances
		done := make(chan bool, 3)

		for i, storage := range storages {
			go func(instance int, stor *RedisStorage) {
				defer func() { done <- true }()

				for j := 0; j < 10; j++ {
					key := baseKey + nodeID(instance) + ":" + string(rune('0'+j))
					value := []byte("value-from-" + nodeID(instance) + "-" + string(rune('0'+j)))

					err := stor.Set(ctx, key, value, 0)
					require.NoError(t, err)
				}
			}(i, storage)
		}

		// Wait for all concurrent writes to complete
		for range 3 {
			<-done
		}

		// Verify all values are consistent across all instances
		for i := 0; i < 3; i++ {
			for j := 0; j < 10; j++ {
				key := baseKey + nodeID(i) + ":" + string(rune('0'+j))
				expectedValue := []byte("value-from-" + nodeID(i) + "-" + string(rune('0'+j)))

				for k, storage := range storages {
					result, err := storage.Get(ctx, key)
					require.NoError(t, err, "instance %d should read key %s", k, key)
					assert.Equal(t, expectedValue, result, "instance %d should have consistent value for key %s", k, key)
				}
			}
		}
	})

	t.Run("bulk_operations_consistency", func(t *testing.T) {
		prefix := "consistency-test:bulk:"

		// Prepare bulk data
		items := make(map[string][]byte)
		for i := 0; i < 50; i++ {
			key := prefix + string(rune('a'+i%26)) + string(rune('0'+i/26))
			value := []byte("bulk-value-" + string(rune('0'+i)))
			items[key] = value
		}

		// Set bulk data from first instance
		err := storages[0].SetMultiple(ctx, items, 0)
		require.NoError(t, err)

		// Allow time for all operations to replicate
		time.Sleep(100 * time.Millisecond)

		// Verify bulk read consistency from all instances
		for i, storage := range storages {
			keys := make([]string, 0, len(items))
			for key := range items {
				keys = append(keys, key)
			}

			results, err := storage.GetMultiple(ctx, keys)
			require.NoError(t, err, "instance %d should perform bulk read", i)
			assert.Equal(t, len(items), len(results), "instance %d should have all items", i)

			for key, expectedValue := range items {
				actualValue, exists := results[key]
				assert.True(t, exists, "instance %d should have key %s", i, key)
				assert.Equal(t, expectedValue, actualValue,
					"instance %d should have correct value for key %s", i, key)
			}
		}
	})
}

// TestDistributedStateFailover tests behavior when one storage instance fails.
func TestDistributedStateFailover(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping distributed failover test in short mode")
	}

	config := RedisStorageConfig{
		Address:        "localhost:6379",
		Database:       4, // Use separate test database
		KeyPrefix:      "failover-test:",
		ConnectTimeout: 2 * time.Second,
		ReadTimeout:    1 * time.Second,
		WriteTimeout:   1 * time.Second,
		PoolSize:       5,
		MinIdleConns:   1,
	}

	logger := testutils.NewMockLogger()

	storage1, err := NewRedisStorage(config, "primary-node", logger)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
		return
	}

	storage2, err := NewRedisStorage(config, "backup-node", logger)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
		return
	}

	ctx := context.Background()

	t.Run("continue_after_node_failure", func(t *testing.T) {
		// Set initial data
		key := "failover-test:data"
		value := []byte("persistent-data")

		err := storage1.Set(ctx, key, value, 0)
		require.NoError(t, err)

		// Verify both can read
		result1, err := storage1.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, result1)

		result2, err := storage2.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, result2)

		// Simulate node1 failure by closing it
		err = storage1.Close()
		require.NoError(t, err)

		// Node2 should still be able to read the data
		result2After, err := storage2.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, result2After)

		// Node2 should be able to update data
		newValue := []byte("updated-after-failover")
		err = storage2.Set(ctx, key, newValue, 0)
		require.NoError(t, err)

		// Verify update
		resultUpdated, err := storage2.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, newValue, resultUpdated)
	})

	// Clean up
	storage2.Close()
}

// nodeID generates a node ID for testing.
func nodeID(i int) string {
	return "test-node-" + string(rune('1'+i))
}
