package statestorage_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/statestorage"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestRaftStorage_SingleNode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Raft integration test in short mode")
	}

	logger := testutils.NewMockLogger()
	tempDir := t.TempDir()

	raftConfig := config.RaftSettings{
		NodeID:  "test-node-1",
		DataDir: tempDir,
		Peers:   []string{}, // Single node cluster
	}

	storage, err := statestorage.NewStateStorage(raftConfig, "raft", "test-node-1", logger)
	require.NoError(t, err)
	require.NotNil(t, storage)
	defer storage.Close()

	ctx := context.Background()

	// Wait for leadership
	time.Sleep(2 * time.Second)

	t.Run("basic_operations", func(t *testing.T) {
		key := "test-key"
		value := []byte("test-value")

		// Test Set
		err := storage.Set(ctx, key, value, time.Minute)
		require.NoError(t, err)

		// Test Get
		retrieved, err := storage.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, retrieved)

		// Test Delete
		err = storage.Delete(ctx, key)
		require.NoError(t, err)

		// Verify deletion
		_, err = storage.Get(ctx, key)
		assert.Equal(t, domain.ErrKeyNotFound, err)
	})

	t.Run("ttl_expiration", func(t *testing.T) {
		key := "expire-key"
		value := []byte("expire-value")

		// Set with short TTL
		err := storage.Set(ctx, key, value, 100*time.Millisecond)
		require.NoError(t, err)

		// Should be available immediately
		retrieved, err := storage.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, retrieved)

		// Wait for expiration
		time.Sleep(200 * time.Millisecond)

		// Should be expired
		_, err = storage.Get(ctx, key)
		assert.Equal(t, domain.ErrKeyNotFound, err)
	})

	t.Run("multiple_operations", func(t *testing.T) {
		items := map[string][]byte{
			"key1": []byte("value1"),
			"key2": []byte("value2"),
			"key3": []byte("value3"),
		}

		// Test SetMultiple
		err := storage.SetMultiple(ctx, items, time.Minute)
		require.NoError(t, err)

		// Test GetMultiple
		retrieved, err := storage.GetMultiple(ctx, []string{"key1", "key2", "key3", "nonexistent"})
		require.NoError(t, err)

		assert.Equal(t, []byte("value1"), retrieved["key1"])
		assert.Equal(t, []byte("value2"), retrieved["key2"])
		assert.Equal(t, []byte("value3"), retrieved["key3"])
		assert.NotContains(t, retrieved, "nonexistent")
	})

	t.Run("watch_functionality", func(t *testing.T) {
		// Create watcher
		watchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		events, err := storage.Watch(watchCtx, "watch:")
		require.NoError(t, err)

		// Set a key that should trigger the watcher
		go func() {
			time.Sleep(100 * time.Millisecond)
			storage.Set(ctx, "watch:test", []byte("watched-value"), time.Minute)
		}()

		// Wait for event
		select {
		case event := <-events:
			assert.Equal(t, domain.StateEventSet, event.Type)
			assert.Equal(t, "watch:test", event.Key)
			assert.Equal(t, []byte("watched-value"), event.Value)
			assert.Equal(t, "test-node-1", event.NodeID)
		case <-time.After(2 * time.Second):
			t.Fatal("Did not receive watch event within timeout")
		}
	})

	t.Run("ping_health_check", func(t *testing.T) {
		err := storage.Ping(ctx)
		require.NoError(t, err)
	})
}

func TestRaftStorage_MultiNode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Raft multi-node test in short mode")
	}

	// Create 3-node Raft cluster
	nodes := make([]domain.StateStorage, 3)
	tempDirs := make([]string, 3)

	// Setup nodes
	for i := 0; i < 3; i++ {
		tempDirs[i] = t.TempDir()
		logger := testutils.NewMockLogger()

		raftConfig := config.RaftSettings{
			NodeID:  fmt.Sprintf("node-%d", i),
			DataDir: tempDirs[i],
			Peers: []string{
				"node-0:127.0.0.1:9000",
				"node-1:127.0.0.1:9001",
				"node-2:127.0.0.1:9002",
			},
		}

		// Override bind address for each node
		storage, err := createRaftStorageWithAddress(raftConfig, fmt.Sprintf("127.0.0.1:%d", 9000+i), logger)
		require.NoError(t, err)
		nodes[i] = storage
	}

	defer func() {
		for _, node := range nodes {
			if node != nil {
				node.Close()
			}
		}
	}()

	// Wait for leader election
	time.Sleep(5 * time.Second)

	// Find leader
	var leader domain.StateStorage
	for _, node := range nodes {
		// Check if this node is the leader by trying a write operation
		testErr := node.Set(context.Background(), "leadership-test", []byte("test"), time.Second)
		if testErr == nil {
			leader = node
			break
		}
	}

	require.NotNil(t, leader, "No leader elected in cluster")

	t.Run("replication_test", func(t *testing.T) {
		ctx := context.Background()
		key := "replicated-key"
		value := []byte("replicated-value")

		// Write to leader
		err := leader.Set(ctx, key, value, time.Minute)
		require.NoError(t, err)

		// Wait for replication
		time.Sleep(1 * time.Second)

		// Verify all nodes have the data
		for i, node := range nodes {
			retrieved, err := node.Get(ctx, key)
			require.NoError(t, err, "Node %d should have replicated data", i)
			assert.Equal(t, value, retrieved, "Node %d has incorrect data", i)
		}
	})

	t.Run("leader_failover", func(t *testing.T) {
		ctx := context.Background()

		// Store initial data
		key := "failover-test"
		value := []byte("failover-value")
		err := leader.Set(ctx, key, value, time.Minute)
		require.NoError(t, err)

		// Close current leader to trigger failover
		leader.Close()

		// Wait for new leader election
		time.Sleep(3 * time.Second)

		// Find new leader
		var newLeader domain.StateStorage
		for _, node := range nodes {
			if node == leader {
				continue // Skip closed leader
			}
			// Check if this node is the leader by trying a write operation
			testErr := node.Set(ctx, "leadership-test-2", []byte("test"), time.Second)
			if testErr == nil {
				newLeader = node
				break
			}
		}

		require.NotNil(t, newLeader, "No new leader elected after failover")

		// Verify data is still accessible from new leader
		retrieved, err := newLeader.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, value, retrieved)

		// Verify new leader can write
		newKey := "post-failover-key"
		newValue := []byte("post-failover-value")
		err = newLeader.Set(ctx, newKey, newValue, time.Minute)
		require.NoError(t, err)
	})
}

func TestRaftStorage_ErrorCases(t *testing.T) {
	logger := testutils.NewMockLogger()

	t.Run("invalid_data_directory", func(t *testing.T) {
		raftConfig := config.RaftSettings{
			NodeID:  "test-node",
			DataDir: "/invalid/readonly/path",
			Peers:   []string{},
		}

		storage, err := statestorage.NewStateStorage(raftConfig, "raft", "test-node", logger)
		require.Error(t, err)
		assert.Nil(t, storage)
		assert.Contains(t, err.Error(), "failed to create data directory")
	})

	t.Run("operations_on_closed_storage", func(t *testing.T) {
		tempDir := t.TempDir()
		raftConfig := config.RaftSettings{
			NodeID:  "test-node",
			DataDir: tempDir,
			Peers:   []string{},
		}

		storage, err := statestorage.NewStateStorage(raftConfig, "raft", "test-node", logger)
		require.NoError(t, err)

		// Close storage
		err = storage.Close()
		require.NoError(t, err)

		ctx := context.Background()

		// Test operations on closed storage
		_, err = storage.Get(ctx, "key")
		assert.Equal(t, domain.ErrStorageClosed, err)

		err = storage.Set(ctx, "key", []byte("value"), time.Minute)
		assert.Equal(t, domain.ErrStorageClosed, err)

		err = storage.Delete(ctx, "key")
		assert.Equal(t, domain.ErrStorageClosed, err)

		_, err = storage.GetMultiple(ctx, []string{"key"})
		assert.Equal(t, domain.ErrStorageClosed, err)

		err = storage.SetMultiple(ctx, map[string][]byte{"key": []byte("value")}, time.Minute)
		assert.Equal(t, domain.ErrStorageClosed, err)

		_, err = storage.Watch(ctx, "prefix")
		assert.Equal(t, domain.ErrStorageClosed, err)

		err = storage.Ping(ctx)
		assert.Equal(t, domain.ErrStorageClosed, err)
	})
}

func TestRaftStorage_Watch_MultipleWatchers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Raft watch test in short mode")
	}

	logger := testutils.NewMockLogger()
	tempDir := t.TempDir()

	raftConfig := config.RaftSettings{
		NodeID:  "watch-test-node",
		DataDir: tempDir,
		Peers:   []string{},
	}

	storage, err := statestorage.NewStateStorage(raftConfig, "raft", "watch-test-node", logger)
	require.NoError(t, err)
	defer storage.Close()

	// Wait for leadership
	time.Sleep(2 * time.Second)

	ctx := context.Background()

	// Create multiple watchers
	watchCtx1, cancel1 := context.WithCancel(ctx)
	defer cancel1()
	events1, err := storage.Watch(watchCtx1, "prefix1:")
	require.NoError(t, err)

	watchCtx2, cancel2 := context.WithCancel(ctx)
	defer cancel2()
	events2, err := storage.Watch(watchCtx2, "prefix2:")
	require.NoError(t, err)

	watchCtx3, cancel3 := context.WithCancel(ctx)
	defer cancel3()
	events3, err := storage.Watch(watchCtx3, "prefix1:") // Same prefix as events1
	require.NoError(t, err)

	// Set values that should trigger different watchers
	err = storage.Set(ctx, "prefix1:key1", []byte("value1"), time.Minute)
	require.NoError(t, err)

	err = storage.Set(ctx, "prefix2:key2", []byte("value2"), time.Minute)
	require.NoError(t, err)

	// Check events1 and events3 receive prefix1: events
	for i := 0; i < 2; i++ {
		var events <-chan domain.StateEvent
		var name string
		if i == 0 {
			events = events1
			name = "events1"
		} else {
			events = events3
			name = "events3"
		}

		select {
		case event := <-events:
			assert.Equal(t, domain.StateEventSet, event.Type)
			assert.Equal(t, "prefix1:key1", event.Key)
			assert.Equal(t, []byte("value1"), event.Value)
		case <-time.After(2 * time.Second):
			t.Fatalf("Watcher %s did not receive event within timeout", name)
		}
	}

	// Check events2 receives prefix2: event
	select {
	case event := <-events2:
		assert.Equal(t, domain.StateEventSet, event.Type)
		assert.Equal(t, "prefix2:key2", event.Key)
		assert.Equal(t, []byte("value2"), event.Value)
	case <-time.After(2 * time.Second):
		t.Fatal("Watcher events2 did not receive event within timeout")
	}

	// Cancel one watcher and verify it stops receiving events
	cancel1()
	time.Sleep(100 * time.Millisecond)

	err = storage.Set(ctx, "prefix1:key3", []byte("value3"), time.Minute)
	require.NoError(t, err)

	// events3 should still receive the event
	select {
	case event := <-events3:
		assert.Equal(t, "prefix1:key3", event.Key)
	case <-time.After(2 * time.Second):
		t.Fatal("Active watcher events3 did not receive event")
	}

	// events1 should not receive anything (channel should be closed)
	select {
	case _, ok := <-events1:
		assert.False(t, ok, "events1 channel should be closed")
	case <-time.After(500 * time.Millisecond):
		// This is also acceptable - channel might not be closed yet
	}
}

func TestRaftStorage_ConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Raft concurrent test in short mode")
	}

	logger := testutils.NewMockLogger()
	tempDir := t.TempDir()

	raftConfig := config.RaftSettings{
		NodeID:  "concurrent-test-node",
		DataDir: tempDir,
		Peers:   []string{},
	}

	storage, err := statestorage.NewStateStorage(raftConfig, "raft", "concurrent-test-node", logger)
	require.NoError(t, err)
	defer storage.Close()

	// Wait for leadership
	time.Sleep(2 * time.Second)

	ctx := context.Background()
	const numOperations = 50

	t.Run("concurrent_writes", func(t *testing.T) {
		errCh := make(chan error, numOperations)

		// Concurrent writes
		for i := 0; i < numOperations; i++ {
			go func(i int) {
				key := fmt.Sprintf("concurrent-key-%d", i)
				value := []byte(fmt.Sprintf("value-%d", i))
				errCh <- storage.Set(ctx, key, value, time.Minute)
			}(i)
		}

		// Check all writes succeeded
		for i := 0; i < numOperations; i++ {
			err := <-errCh
			require.NoError(t, err, "Write %d should succeed", i)
		}

		// Verify all values
		for i := 0; i < numOperations; i++ {
			key := fmt.Sprintf("concurrent-key-%d", i)
			expected := []byte(fmt.Sprintf("value-%d", i))

			value, err := storage.Get(ctx, key)
			require.NoError(t, err, "Read %d should succeed", i)
			assert.Equal(t, expected, value, "Value %d should match", i)
		}
	})
}

func TestRaftStorage_PersistenceAndRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Raft persistence test in short mode")
	}

	logger := testutils.NewMockLogger()
	tempDir := t.TempDir()

	raftConfig := config.RaftSettings{
		NodeID:  "persistence-test-node",
		DataDir: tempDir,
		Peers:   []string{},
	}

	// First instance - write data
	storage1, err := statestorage.NewStateStorage(raftConfig, "raft", "persistence-test-node", logger)
	require.NoError(t, err)

	// Wait for leadership
	time.Sleep(2 * time.Second)

	ctx := context.Background()
	testData := map[string][]byte{
		"persistent-key-1": []byte("persistent-value-1"),
		"persistent-key-2": []byte("persistent-value-2"),
		"persistent-key-3": []byte("persistent-value-3"),
	}

	// Write test data
	for key, value := range testData {
		err = storage1.Set(ctx, key, value, time.Hour) // Long TTL
		require.NoError(t, err)
	}

	// Verify data is written
	for key, expectedValue := range testData {
		value, err := storage1.Get(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, expectedValue, value)
	}

	// Close first instance
	err = storage1.Close()
	require.NoError(t, err)

	// Wait a bit
	time.Sleep(1 * time.Second)

	// Create second instance with same configuration
	storage2, err := statestorage.NewStateStorage(raftConfig, "raft", "persistence-test-node", logger)
	require.NoError(t, err)
	defer storage2.Close()

	// Wait for startup
	time.Sleep(3 * time.Second)

	// Verify data persisted
	for key, expectedValue := range testData {
		value, err := storage2.Get(ctx, key)
		require.NoError(t, err, "Key %s should persist after restart", key)
		assert.Equal(t, expectedValue, value, "Value for %s should match after restart", key)
	}

	// Verify we can write new data
	newKey := "post-restart-key"
	newValue := []byte("post-restart-value")
	err = storage2.Set(ctx, newKey, newValue, time.Hour)
	assert.NoError(t, err)

	retrievedNewValue, err := storage2.Get(ctx, newKey)
	assert.NoError(t, err)
	assert.Equal(t, newValue, retrievedNewValue)
}

// createRaftStorageWithAddress creates a Raft storage with custom bind address.
func createRaftStorageWithAddress(raftConfig config.RaftSettings, bindAddress string, logger domain.Logger) (domain.StateStorage, error) {
	// Create custom RaftStorageConfig
	storageConfig := statestorage.RaftStorageConfig{
		NodeID:             raftConfig.NodeID,
		BindAddress:        bindAddress,
		DataDir:            raftConfig.DataDir,
		Peers:              raftConfig.Peers,
		HeartbeatTimeout:   1 * time.Second,
		ElectionTimeout:    1 * time.Second,
		LeaderLeaseTimeout: 500 * time.Millisecond,
		CommitTimeout:      50 * time.Millisecond,
		SnapshotRetention:  3,
		SnapshotThreshold:  1024,
		TrailingLogs:       1024,
	}

	return statestorage.NewRaftStorage(storageConfig, raftConfig.NodeID, logger)
}

func TestRaftStorage_Factory_Integration(t *testing.T) {
	logger := testutils.NewMockLogger()

	t.Run("raft_storage_creation", func(t *testing.T) {
		tempDir := t.TempDir()
		raftConfig := config.RaftSettings{
			NodeID:  "factory-test-node",
			DataDir: tempDir,
			Peers:   []string{},
		}

		storage, err := statestorage.NewStateStorage(raftConfig, "raft", "factory-test-node", logger)
		require.NoError(t, err)
		assert.NotNil(t, storage)
		defer storage.Close()

		// Test basic operation
		ctx := context.Background()
		time.Sleep(2 * time.Second) // Wait for leadership

		err = storage.Set(ctx, "factory-test", []byte("factory-value"), time.Minute)
		require.NoError(t, err)

		value, err := storage.Get(ctx, "factory-test")
		require.NoError(t, err)
		assert.Equal(t, []byte("factory-value"), value)
	})

	t.Run("invalid_raft_config_type", func(t *testing.T) {
		invalidConfig := "not-a-raft-config"
		storage, err := statestorage.NewStateStorage(invalidConfig, "raft", "test-node", logger)
		require.Error(t, err)
		assert.Nil(t, storage)
		assert.Contains(t, err.Error(), "invalid Raft configuration type")
	})
}

func TestRaftStorage_DataDir_Management(t *testing.T) {
	logger := testutils.NewMockLogger()

	t.Run("creates_missing_data_directory", func(t *testing.T) {
		tempDir := t.TempDir()
		dataDirPath := filepath.Join(tempDir, "missing", "nested", "path")

		raftConfig := config.RaftSettings{
			NodeID:  "datadir-test-node",
			DataDir: dataDirPath,
			Peers:   []string{},
		}

		// Should create missing directories
		storage, err := statestorage.NewStateStorage(raftConfig, "raft", "datadir-test-node", logger)
		require.NoError(t, err)
		defer storage.Close()

		// Verify directory was created
		info, err := os.Stat(dataDirPath)
		require.NoError(t, err)
		assert.True(t, info.IsDir())

		// Verify Raft files are created
		files := []string{"raft-log.db", "raft-stable.db"}
		for _, file := range files {
			_, err := os.Stat(filepath.Join(dataDirPath, file))
			assert.NoError(t, err, "File %s should be created", file)
		}
	})
}
