package statestorage_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/statestorage"
)

func TestLocalStorage_BasicOperations(t *testing.T) {
	storage := statestorage.NewLocalStorage("test-node")
	defer storage.Close()

	ctx := context.Background()
	key := "test-key"
	value := []byte("test-value")

	// Test Set and Get
	err := storage.Set(ctx, key, value, time.Hour)
	require.NoError(t, err)

	retrieved, err := storage.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, value, retrieved)

	// Test Delete
	err = storage.Delete(ctx, key)
	require.NoError(t, err)

	_, err = storage.Get(ctx, key)
	assert.Equal(t, domain.ErrKeyNotFound, err)
}

func TestLocalStorage_TTLExpiration(t *testing.T) {
	storage := statestorage.NewLocalStorage("test-node")
	defer storage.Close()

	ctx := context.Background()
	key := "expire-key"
	value := []byte("expire-value")

	// Set with short TTL
	err := storage.Set(ctx, key, value, 50*time.Millisecond)
	require.NoError(t, err)

	// Should exist immediately
	retrieved, err := storage.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, value, retrieved)

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Should be expired
	_, err = storage.Get(ctx, key)
	assert.Equal(t, domain.ErrKeyNotFound, err)
}

func TestLocalStorage_NoTTL(t *testing.T) {
	storage := statestorage.NewLocalStorage("test-node")
	defer storage.Close()

	ctx := context.Background()
	key := "no-ttl-key"
	value := []byte("no-ttl-value")

	// Set with no TTL (0 duration)
	err := storage.Set(ctx, key, value, 0)
	require.NoError(t, err)

	// Should exist after some time
	time.Sleep(100 * time.Millisecond)

	retrieved, err := storage.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, value, retrieved)
}

func TestLocalStorage_Watch(t *testing.T) {
	storage := statestorage.NewLocalStorage("test-node")
	defer storage.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup watcher
	events, err := storage.Watch(ctx, "test:")
	require.NoError(t, err)

	// Set a key that matches the prefix
	key := "test:watched-key"
	value := []byte("watched-value")

	go func() {
		time.Sleep(10 * time.Millisecond)
		storage.Set(context.Background(), key, value, time.Hour)
	}()

	// Wait for event
	select {
	case event := <-events:
		assert.Equal(t, domain.StateEventSet, event.Type)
		assert.Equal(t, key, event.Key)
		assert.Equal(t, value, event.Value)
		assert.Equal(t, "test-node", event.NodeID)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Expected watch event not received")
	}
}

func TestLocalStorage_WatchWithDelete(t *testing.T) {
	storage := statestorage.NewLocalStorage("test-node")
	defer storage.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup watcher
	events, err := storage.Watch(ctx, "test:")
	require.NoError(t, err)

	// Set then delete a key
	key := "test:delete-key"
	value := []byte("delete-value")

	// First set
	storage.Set(context.Background(), key, value, time.Hour)

	// Consume set event
	select {
	case event := <-events:
		assert.Equal(t, domain.StateEventSet, event.Type)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected set event not received")
	}

	// Then delete
	go func() {
		time.Sleep(10 * time.Millisecond)
		storage.Delete(context.Background(), key)
	}()

	// Wait for delete event
	select {
	case event := <-events:
		assert.Equal(t, domain.StateEventDelete, event.Type)
		assert.Equal(t, key, event.Key)
		assert.Equal(t, "test-node", event.NodeID)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Expected delete event not received")
	}
}

func TestLocalStorage_WatchContextCancellation(t *testing.T) {
	storage := statestorage.NewLocalStorage("test-node")
	defer storage.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Setup watcher
	events, err := storage.Watch(ctx, "test:")
	require.NoError(t, err)

	// Cancel context
	cancel()

	// Channel should be closed
	select {
	case _, ok := <-events:
		assert.False(t, ok, "Channel should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Channel should be closed")
	}
}

func TestLocalStorage_WatchKeyPrefixFiltering(t *testing.T) {
	storage := statestorage.NewLocalStorage("test-node")
	defer storage.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup watcher for specific prefix
	events, err := storage.Watch(ctx, "backend:")
	require.NoError(t, err)

	// Set keys with different prefixes
	storage.Set(context.Background(), "backend:health:test", []byte("should-match"), time.Hour)
	storage.Set(context.Background(), "other:key", []byte("should-not-match"), time.Hour)
	storage.Set(context.Background(), "backend:status:test", []byte("should-also-match"), time.Hour)

	receivedEvents := make([]domain.StateEvent, 0)

	// Collect events
	timeout := time.After(200 * time.Millisecond)
	for len(receivedEvents) < 2 {
		select {
		case event := <-events:
			receivedEvents = append(receivedEvents, event)
		case <-timeout:
			break
		}
	}

	// Should only receive events for keys matching the prefix
	require.Len(t, receivedEvents, 2)
	assert.Equal(t, "backend:health:test", receivedEvents[0].Key)
	assert.Equal(t, "backend:status:test", receivedEvents[1].Key)
}

func TestLocalStorage_ConcurrentAccess(t *testing.T) {
	storage := statestorage.NewLocalStorage("test-node")
	defer storage.Close()

	ctx := context.Background()
	var wg sync.WaitGroup
	numGoroutines := 50

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("concurrent-key-%d", i)
			value := []byte(fmt.Sprintf("value-%d", i))

			err := storage.Set(ctx, key, value, time.Hour)
			assert.NoError(t, err)
		}(i)
	}

	wg.Wait()

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("concurrent-key-%d", i)
			expected := []byte(fmt.Sprintf("value-%d", i))

			value, err := storage.Get(ctx, key)
			assert.NoError(t, err)
			assert.Equal(t, expected, value)
		}(i)
	}

	wg.Wait()
}

func TestLocalStorage_GetMultiple(t *testing.T) {
	storage := statestorage.NewLocalStorage("test-node")
	defer storage.Close()

	ctx := context.Background()

	// Set multiple keys
	keys := []string{"key1", "key2", "key3"}
	values := [][]byte{[]byte("value1"), []byte("value2"), []byte("value3")}

	for i, key := range keys {
		err := storage.Set(ctx, key, values[i], time.Hour)
		require.NoError(t, err)
	}

	// Get multiple
	result, err := storage.GetMultiple(ctx, keys)
	require.NoError(t, err)

	assert.Len(t, result, 3)
	for i, key := range keys {
		assert.Equal(t, values[i], result[key])
	}
}

func TestLocalStorage_GetMultiple_MissingKeys(t *testing.T) {
	storage := statestorage.NewLocalStorage("test-node")
	defer storage.Close()

	ctx := context.Background()

	// Set only some keys
	storage.Set(ctx, "key1", []byte("value1"), time.Hour)
	storage.Set(ctx, "key3", []byte("value3"), time.Hour)

	// Try to get multiple including missing key
	keys := []string{"key1", "key2", "key3"}
	result, err := storage.GetMultiple(ctx, keys)
	require.NoError(t, err)

	// Should only contain existing keys
	assert.Len(t, result, 2)
	assert.Equal(t, []byte("value1"), result["key1"])
	assert.Equal(t, []byte("value3"), result["key3"])
	assert.NotContains(t, result, "key2")
}

func TestLocalStorage_SetMultiple(t *testing.T) {
	storage := statestorage.NewLocalStorage("test-node")
	defer storage.Close()

	ctx := context.Background()

	// Set multiple keys at once
	items := map[string][]byte{
		"multi1": []byte("multivalue1"),
		"multi2": []byte("multivalue2"),
		"multi3": []byte("multivalue3"),
	}

	err := storage.SetMultiple(ctx, items, time.Hour)
	require.NoError(t, err)

	// Verify all were set
	for key, expectedValue := range items {
		value, err := storage.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, expectedValue, value)
	}
}

func TestLocalStorage_Ping(t *testing.T) {
	storage := statestorage.NewLocalStorage("test-node")
	defer storage.Close()

	ctx := context.Background()
	err := storage.Ping(ctx)
	assert.NoError(t, err)
}

func TestLocalStorage_ContextCancellation(t *testing.T) {
	storage := statestorage.NewLocalStorage("test-node")
	defer storage.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Operations should still work (local storage doesn't depend on context for cancellation)
	err := storage.Set(ctx, "test", []byte("value"), time.Hour)
	assert.NoError(t, err)

	_, err = storage.Get(ctx, "test")
	assert.NoError(t, err)
}

func TestLocalStorage_CleanupExpiredKeys(t *testing.T) {
	storage := statestorage.NewLocalStorage("test-node")
	defer storage.Close()

	ctx := context.Background()

	// Set keys with different TTLs
	storage.Set(ctx, "short-ttl", []byte("value1"), 50*time.Millisecond)
	storage.Set(ctx, "long-ttl", []byte("value2"), time.Hour)
	storage.Set(ctx, "no-ttl", []byte("value3"), 0)

	// Wait for short TTL to expire
	time.Sleep(100 * time.Millisecond)

	// Trigger cleanup by trying to access expired key
	_, err := storage.Get(ctx, "short-ttl")
	assert.Equal(t, domain.ErrKeyNotFound, err)

	// Other keys should still exist
	_, err = storage.Get(ctx, "long-ttl")
	assert.NoError(t, err)

	_, err = storage.Get(ctx, "no-ttl")
	assert.NoError(t, err)
}

func TestLocalStorage_Close(t *testing.T) {
	storage := statestorage.NewLocalStorage("test-node")

	// Should be able to close
	err := storage.Close()
	assert.NoError(t, err)

	// Should be able to close multiple times
	err = storage.Close()
	assert.NoError(t, err)
}

func TestLocalStorage_WatchAfterClose(t *testing.T) {
	storage := statestorage.NewLocalStorage("test-node")

	ctx := context.Background()

	// Close storage
	storage.Close()

	// Watch should still work but no events will be sent
	events, err := storage.Watch(ctx, "test:")
	require.NoError(t, err)

	// Set a key
	storage.Set(ctx, "test:key", []byte("value"), time.Hour)

	// Should not receive any events (storage is closed)
	select {
	case <-events:
		t.Fatal("Should not receive events after close")
	case <-time.After(100 * time.Millisecond):
		// Expected - no events
	}
}

func BenchmarkLocalStorage_Set(b *testing.B) {
	storage := statestorage.NewLocalStorage("bench-node")
	defer storage.Close()

	ctx := context.Background()
	value := []byte("benchmark-value")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("bench-key-%d", i)
			storage.Set(ctx, key, value, time.Hour)
			i++
		}
	})
}

func BenchmarkLocalStorage_Get(b *testing.B) {
	storage := statestorage.NewLocalStorage("bench-node")
	defer storage.Close()

	ctx := context.Background()
	key := "bench-get-key"
	value := []byte("benchmark-value")

	// Setup
	storage.Set(ctx, key, value, time.Hour)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			storage.Get(ctx, key)
		}
	})
}

func BenchmarkLocalStorage_SetMultiple(b *testing.B) {
	storage := statestorage.NewLocalStorage("bench-node")
	defer storage.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items := make(map[string][]byte)
		for j := 0; j < 10; j++ {
			key := fmt.Sprintf("batch-%d-%d", i, j)
			items[key] = []byte(fmt.Sprintf("value-%d-%d", i, j))
		}
		storage.SetMultiple(ctx, items, time.Hour)
	}
}
