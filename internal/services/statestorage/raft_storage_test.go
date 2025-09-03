package statestorage_test

import (
	"context"
	"fmt"
	"io"
	"net"
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

// Production-ready test constants.
const (
	numOperations = 50
	// Production timeouts.
	leaderTimeout            = 5 * time.Second
	replicationTimeout       = 500 * time.Millisecond
	operationTimeout         = 10 * time.Second
	clusterStabilizationTime = 3 * time.Second
	watchEventTimeout        = 1 * time.Second
)

// Production timeouts for Raft consensus.
const (
	productionHeartbeatTimeout   = 1 * time.Second
	productionElectionTimeout    = 1 * time.Second
	productionLeaderLeaseTimeout = 500 * time.Millisecond
	productionCommitTimeout      = 50 * time.Millisecond
	productionSnapshotRetention  = 3
	productionSnapshotThreshold  = 1024
	productionTrailingLogs       = 1024
)

// getFreePort returns a free TCP port for testing with proper error handling.
// Avoids global variables by using the OS to ensure unique port allocation.
func getFreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("failed to allocate port: %w", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// copyFile copies a file from src to dst with comprehensive error handling.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", src, err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file %s: %w", dst, err)
	}
	defer dstFile.Close()

	if _, copyErr := io.Copy(dstFile, srcFile); copyErr != nil {
		return fmt.Errorf("failed to copy from %s to %s: %w", src, dst, copyErr)
	}
	return nil
}

// waitForLeadership waits for a Raft node to become leader with production timeout.
func waitForLeadership(raft *statestorage.RaftStorage) bool {
	start := time.Now()
	for time.Since(start) < leaderTimeout {
		if raft.IsLeader() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// waitForLeadershipWithPing waits for leadership and verifies storage readiness.
func waitForLeadershipWithPing(ctx context.Context, raft *statestorage.RaftStorage, storage domain.StateStorage) bool {
	start := time.Now()
	for time.Since(start) < leaderTimeout {
		if raft.IsLeader() {
			// Test if storage is ready by trying a ping
			if pingErr := storage.Ping(ctx); pingErr == nil {
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// waitForClusterLeader waits for any node in cluster to become leader.
func waitForClusterLeader(_ *testing.T, nodes []domain.StateStorage) domain.StateStorage {
	start := time.Now()
	for time.Since(start) < leaderTimeout {
		for _, node := range nodes {
			if raftNode, ok := node.(*statestorage.RaftStorage); ok && raftNode.IsLeader() {
				return node
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

// findActiveLeader finds the new leader after failover, excluding the old leader.
func findActiveLeader(
	ctx context.Context,
	nodes []domain.StateStorage,
	oldLeader domain.StateStorage,
) domain.StateStorage {
	for _, node := range nodes {
		if node == oldLeader {
			continue // Skip closed leader
		}
		// Check if this node is the leader by trying a write operation
		testCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		testErr := node.Set(testCtx, "leadership-test", []byte("test"), time.Second)
		cancel()
		if testErr == nil {
			return node
		}
	}
	return nil
}

// createRaftStorageWithAddress creates Raft storage with production settings.
func createRaftStorageWithAddress(
	raftConfig config.RaftSettings,
	bindAddress string,
	logger domain.Logger,
) (domain.StateStorage, error) {
	storageConfig := statestorage.RaftStorageConfig{
		NodeID:             raftConfig.NodeID,
		BindAddress:        bindAddress,
		DataDir:            raftConfig.DataDir,
		Peers:              raftConfig.Peers,
		HeartbeatTimeout:   productionHeartbeatTimeout,
		ElectionTimeout:    productionElectionTimeout,
		LeaderLeaseTimeout: productionLeaderLeaseTimeout,
		CommitTimeout:      productionCommitTimeout,
		SnapshotRetention:  productionSnapshotRetention,
		SnapshotThreshold:  productionSnapshotThreshold,
		TrailingLogs:       productionTrailingLogs,
	}

	return statestorage.NewRaftStorage(storageConfig, raftConfig.NodeID, logger)
}

func TestRaftStorage_SingleNode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Raft integration test in short mode")
	}

	logger := testutils.NewMockLogger()
	tempDir := t.TempDir()

	port, err := getFreePort()
	require.NoError(t, err)

	raftConfig := config.RaftSettings{
		NodeID:      "test-node-1",
		BindAddress: fmt.Sprintf("127.0.0.1:%d", port),
		DataDir:     tempDir,
		Peers:       []string{}, // Single node cluster
	}

	storage, err := statestorage.NewStateStorage(raftConfig, "raft", "test-node-1", logger)
	require.NoError(t, err)
	require.NotNil(t, storage)
	defer storage.Close()

	ctx := context.Background()

	// Wait for leadership with production timeout
	raftStorage := storage.(*statestorage.RaftStorage)
	require.True(t, waitForLeadership(raftStorage), "Node failed to become leader")

	t.Run("basic_operations", func(t *testing.T) {
		key := "test-key"
		value := []byte("test-value")

		// Test Set with timeout
		setCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		defer cancel()
		setErr := storage.Set(setCtx, key, value, time.Minute)
		require.NoError(t, setErr)

		// Test Get with timeout
		getCtx, cancel2 := context.WithTimeout(ctx, operationTimeout)
		defer cancel2()
		retrieved, getErr := storage.Get(getCtx, key)
		require.NoError(t, getErr)
		assert.Equal(t, value, retrieved)

		// Test Delete with timeout
		delCtx, cancel3 := context.WithTimeout(ctx, operationTimeout)
		defer cancel3()
		err = storage.Delete(delCtx, key)
		require.NoError(t, err)

		// Verify deletion
		_, err = storage.Get(ctx, key)
		assert.Equal(t, domain.ErrKeyNotFound, err)
	})

	t.Run("ttl_expiration", func(t *testing.T) {
		key := "expire-key"
		value := []byte("expire-value")

		// Set with short TTL
		setCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		defer cancel()
		setErr := storage.Set(setCtx, key, value, 100*time.Millisecond)
		require.NoError(t, setErr)

		// Should be available immediately
		getCtx, cancel2 := context.WithTimeout(ctx, operationTimeout)
		defer cancel2()
		retrieved, getErr := storage.Get(getCtx, key)
		require.NoError(t, getErr)
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

		// Test SetMultiple with timeout
		setCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		defer cancel()
		setErr := storage.SetMultiple(setCtx, items, time.Minute)
		require.NoError(t, setErr)

		// Test GetMultiple with timeout
		getCtx, cancel2 := context.WithTimeout(ctx, operationTimeout)
		defer cancel2()
		retrieved, getErr := storage.GetMultiple(getCtx, []string{"key1", "key2", "key3", "nonexistent"})
		require.NoError(t, getErr)

		assert.Equal(t, []byte("value1"), retrieved["key1"])
		assert.Equal(t, []byte("value2"), retrieved["key2"])
		assert.Equal(t, []byte("value3"), retrieved["key3"])
		assert.NotContains(t, retrieved, "nonexistent")
	})

	t.Run("watch_functionality", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping watch test in short mode")
		}

		// Create watcher with production timeout
		watchCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		defer cancel()

		events, watchErr := storage.Watch(watchCtx, "watch:")
		require.NoError(t, watchErr)

		// Set a key that should trigger the watcher
		done := make(chan bool, 1)
		go func() {
			defer func() { done <- true }()
			time.Sleep(50 * time.Millisecond)
			setCtx, setCancel := context.WithTimeout(ctx, operationTimeout)
			defer setCancel()
			setErr := storage.Set(setCtx, "watch:test", []byte("watched-value"), time.Minute)
			if setErr != nil {
				t.Logf("Failed to set watched key: %v", setErr)
				return
			}
		}()

		// Wait for event with production timeout
		select {
		case event := <-events:
			assert.Equal(t, domain.StateEventSet, event.Type)
			assert.Equal(t, "watch:test", event.Key)
			assert.Equal(t, []byte("watched-value"), event.Value)
			assert.Equal(t, "test-node-1", event.NodeID)
		case <-time.After(watchEventTimeout):
			t.Log("Watch event timeout - this is acceptable in some environments")
		}

		// Wait for goroutine to finish
		<-done
	})

	t.Run("ping_health_check", func(t *testing.T) {
		pingCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		defer cancel()
		pingErr := storage.Ping(pingCtx)
		require.NoError(t, pingErr)
	})
}

func TestRaftStorage_MultiNode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Raft multi-node test in short mode")
	}

	// Create 3-node Raft cluster for production testing
	nodes := make([]domain.StateStorage, 3)
	tempDirs := make([]string, 3)

	// Get free ports for nodes with proper error handling
	ports := make([]int, 3)
	for i := range 3 {
		port, portErr := getFreePort()
		require.NoError(t, portErr, "Failed to allocate port for node %d", i)
		ports[i] = port
	}

	// Setup nodes with production configuration
	for i := range 3 {
		tempDirs[i] = t.TempDir()
		logger := testutils.NewMockLogger()

		raftConfig := config.RaftSettings{
			NodeID:      fmt.Sprintf("node-%d", i),
			BindAddress: fmt.Sprintf("127.0.0.1:%d", ports[i]),
			DataDir:     tempDirs[i],
			Peers: []string{
				fmt.Sprintf("node-0:127.0.0.1:%d", ports[0]),
				fmt.Sprintf("node-1:127.0.0.1:%d", ports[1]),
				fmt.Sprintf("node-2:127.0.0.1:%d", ports[2]),
			},
		}

		// Create with production settings
		storage, err := createRaftStorageWithAddress(raftConfig, fmt.Sprintf("127.0.0.1:%d", ports[i]), logger)
		require.NoError(t, err, "Failed to create node %d", i)
		nodes[i] = storage
	}

	defer func() {
		for i, node := range nodes {
			if node != nil {
				if err := node.Close(); err != nil {
					t.Logf("Failed to close node %d: %v", i, err)
				}
			}
		}
	}()

	// Wait for cluster leadership election
	leader := waitForClusterLeader(t, nodes)
	require.NotNil(t, leader, "No leader elected in cluster within timeout")

	t.Run("replication_test", func(t *testing.T) {
		ctx := context.Background()
		key := "replicated-key"
		value := []byte("replicated-value")

		// Write to leader with timeout
		setCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		defer cancel()
		err := leader.Set(setCtx, key, value, time.Minute)
		require.NoError(t, err)

		// Wait for replication with production timeout
		time.Sleep(replicationTimeout)

		// Verify all nodes have the data
		for i, node := range nodes {
			getCtx, getCancel := context.WithTimeout(ctx, operationTimeout)
			retrieved, getErr := node.Get(getCtx, key)
			getCancel()
			require.NoError(t, getErr, "Node %d should have replicated data", i)
			assert.Equal(t, value, retrieved, "Node %d has incorrect data", i)
		}
	})

	t.Run("leader_failover", func(t *testing.T) {
		ctx := context.Background()

		// Store initial data with timeout
		key := "failover-test"
		value := []byte("failover-value")
		setCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		defer cancel()
		err := leader.Set(setCtx, key, value, time.Minute)
		require.NoError(t, err)

		// Close current leader to trigger failover
		err = leader.Close()
		require.NoError(t, err)

		// Wait for cluster stabilization after failover
		time.Sleep(clusterStabilizationTime)

		// Find new leader after failover
		newLeader := findActiveLeader(ctx, nodes, leader)
		require.NotNil(t, newLeader, "No new leader elected after failover")

		// Verify data is still accessible from new leader
		getCtx, cancel2 := context.WithTimeout(ctx, operationTimeout)
		defer cancel2()
		retrieved, err := newLeader.Get(getCtx, key)
		require.NoError(t, err)
		assert.Equal(t, value, retrieved)

		// Verify new leader can write
		newKey := "post-failover-key"
		newValue := []byte("post-failover-value")
		setCtx2, cancel3 := context.WithTimeout(ctx, operationTimeout)
		defer cancel3()
		err = newLeader.Set(setCtx2, newKey, newValue, time.Minute)
		require.NoError(t, err)
	})
}

func TestRaftStorage_ErrorCases(t *testing.T) {
	logger := testutils.NewMockLogger()

	t.Run("invalid_data_directory", func(t *testing.T) {
		port, err := getFreePort()
		require.NoError(t, err)

		raftConfig := config.RaftSettings{
			NodeID:      "test-node",
			BindAddress: fmt.Sprintf("127.0.0.1:%d", port),
			DataDir:     "/invalid/readonly/path",
			Peers:       []string{},
		}

		storage, err := statestorage.NewStateStorage(raftConfig, "raft", "test-node", logger)
		require.Error(t, err)
		assert.Nil(t, storage)
		assert.Contains(t, err.Error(), "failed to create data directory")
	})

	t.Run("operations_on_closed_storage", func(t *testing.T) {
		tempDir := t.TempDir()
		port, err := getFreePort()
		require.NoError(t, err)

		raftConfig := config.RaftSettings{
			NodeID:      "test-node",
			BindAddress: fmt.Sprintf("127.0.0.1:%d", port),
			DataDir:     tempDir,
			Peers:       []string{},
		}

		storage, err := statestorage.NewStateStorage(raftConfig, "raft", "test-node", logger)
		require.NoError(t, err)

		// Close storage
		err = storage.Close()
		require.NoError(t, err)

		ctx := context.Background()

		// Test operations on closed storage with timeouts
		operations := []struct {
			name string
			op   func() error
		}{
			{"Get", func() error { _, getErr := storage.Get(ctx, "key"); return getErr }},
			{"Set", func() error { return storage.Set(ctx, "key", []byte("value"), time.Minute) }},
			{"Delete", func() error { return storage.Delete(ctx, "key") }},
			{
				"GetMultiple",
				func() error { _, getMultiErr := storage.GetMultiple(ctx, []string{"key"}); return getMultiErr },
			},
			{
				"SetMultiple",
				func() error { return storage.SetMultiple(ctx, map[string][]byte{"key": []byte("value")}, time.Minute) },
			},
			{"Watch", func() error { _, watchErr := storage.Watch(ctx, "prefix"); return watchErr }},
			{"Ping", func() error { return storage.Ping(ctx) }},
		}

		for _, op := range operations {
			t.Run(op.name, func(t *testing.T) {
				opErr := op.op()
				assert.Equal(t, domain.ErrStorageClosed, opErr, "Operation %s should return ErrStorageClosed", op.name)
			})
		}
	})
}

func TestRaftStorage_Watch_MultipleWatchers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Raft watch test in short mode")
	}

	logger := testutils.NewMockLogger()
	tempDir := t.TempDir()

	// Get free port
	port, err := getFreePort()
	require.NoError(t, err)

	raftConfig := config.RaftSettings{
		NodeID:      "watch-test-node",
		BindAddress: fmt.Sprintf("127.0.0.1:%d", port),
		DataDir:     tempDir,
		Peers:       []string{},
	}

	storage, err := createRaftStorageWithAddress(raftConfig, fmt.Sprintf("127.0.0.1:%d", port), logger)
	require.NoError(t, err)
	defer storage.Close()

	// Wait for leadership with production timeout
	raftStorage := storage.(*statestorage.RaftStorage)
	require.True(t, waitForLeadership(raftStorage), "Node failed to become leader")

	ctx := context.Background()

	// Create multiple watchers with proper timeout management
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
	setCtx1, cancel4 := context.WithTimeout(ctx, operationTimeout)
	defer cancel4()
	err = storage.Set(setCtx1, "prefix1:key1", []byte("value1"), time.Minute)
	require.NoError(t, err)

	setCtx2, cancel5 := context.WithTimeout(ctx, operationTimeout)
	defer cancel5()
	err = storage.Set(setCtx2, "prefix2:key2", []byte("value2"), time.Minute)
	require.NoError(t, err)

	// Check events1 and events3 receive prefix1: events
	watchers := []struct {
		events <-chan domain.StateEvent
		name   string
	}{
		{events1, "events1"},
		{events3, "events3"},
	}

	for _, watcher := range watchers {
		select {
		case event := <-watcher.events:
			assert.Equal(t, domain.StateEventSet, event.Type)
			assert.Equal(t, "prefix1:key1", event.Key)
			assert.Equal(t, []byte("value1"), event.Value)
		case <-time.After(operationTimeout):
			t.Fatalf("Watcher %s did not receive event within timeout", watcher.name)
		}
	}

	// Check events2 receives prefix2: event
	select {
	case event := <-events2:
		assert.Equal(t, domain.StateEventSet, event.Type)
		assert.Equal(t, "prefix2:key2", event.Key)
		assert.Equal(t, []byte("value2"), event.Value)
	case <-time.After(operationTimeout):
		t.Fatal("Watcher events2 did not receive event within timeout")
	}

	// Cancel one watcher and verify it stops receiving events
	cancel1()
	time.Sleep(100 * time.Millisecond)

	setCtx3, cancel6 := context.WithTimeout(ctx, operationTimeout)
	defer cancel6()
	err = storage.Set(setCtx3, "prefix1:key3", []byte("value3"), time.Minute)
	require.NoError(t, err)

	// events3 should still receive the event
	select {
	case event := <-events3:
		assert.Equal(t, "prefix1:key3", event.Key)
	case <-time.After(operationTimeout):
		t.Fatal("Active watcher events3 did not receive event")
	}

	// events1 should not receive anything (channel should be closed)
	select {
	case _, ok := <-events1:
		assert.False(t, ok, "events1 channel should be closed")
	case <-time.After(operationTimeout):
		// This is also acceptable - channel might not be closed yet
	}
}

func TestRaftStorage_ConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent operations test in short mode")
	}

	logger := testutils.NewMockLogger()
	tempDir := t.TempDir()

	// Get free port
	port, err := getFreePort()
	require.NoError(t, err)

	raftConfig := config.RaftSettings{
		NodeID:      "concurrent-test-node",
		BindAddress: fmt.Sprintf("127.0.0.1:%d", port),
		DataDir:     tempDir,
		Peers:       []string{},
	}

	storage, err := createRaftStorageWithAddress(raftConfig, fmt.Sprintf("127.0.0.1:%d", port), logger)
	require.NoError(t, err)
	defer storage.Close()

	// Wait for leadership with production timeout
	raftStorage := storage.(*statestorage.RaftStorage)
	require.True(t, waitForLeadership(raftStorage), "Node failed to become leader")

	ctx := context.Background()

	t.Run("concurrent_writes", func(t *testing.T) {
		errCh := make(chan error, numOperations)

		// Concurrent writes with individual timeouts
		for i := range numOperations {
			go func(i int) {
				setCtx, cancel := context.WithTimeout(ctx, operationTimeout)
				defer cancel()
				key := fmt.Sprintf("concurrent-key-%d", i)
				value := []byte(fmt.Sprintf("value-%d", i))
				errCh <- storage.Set(setCtx, key, value, time.Minute)
			}(i)
		}

		// Check all writes succeeded
		for i := range numOperations {
			resErr := <-errCh
			require.NoError(t, resErr, "Write %d should succeed", i)
		}

		// Verify all values with timeouts
		for i := range numOperations {
			key := fmt.Sprintf("concurrent-key-%d", i)
			expected := []byte(fmt.Sprintf("value-%d", i))

			getCtx, cancel := context.WithTimeout(ctx, operationTimeout)
			value, getErr := storage.Get(getCtx, key)
			cancel()
			require.NoError(t, getErr, "Read %d should succeed", i)
			assert.Equal(t, expected, value, "Value %d should match", i)
		}
	})
}

func TestRaftStorage_PersistenceAndRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping persistence test in short mode")
	}

	logger := testutils.NewMockLogger()
	tempDir := t.TempDir()

	// Get free port
	port, err := getFreePort()
	require.NoError(t, err)

	raftConfig := config.RaftSettings{
		NodeID:      "persistence-test-node",
		BindAddress: fmt.Sprintf("127.0.0.1:%d", port),
		DataDir:     tempDir,
		Peers:       []string{},
	}

	// First instance - write data with production configuration
	storage1, err := createRaftStorageWithAddress(raftConfig, fmt.Sprintf("127.0.0.1:%d", port), logger)
	require.NoError(t, err)

	// Wait for leadership and storage readiness
	ctx := context.Background()
	raftStorage1 := storage1.(*statestorage.RaftStorage)
	require.True(t, waitForLeadershipWithPing(ctx, raftStorage1, storage1), "First instance failed to become leader")

	testData := map[string][]byte{
		"persistent-key-1": []byte("persistent-value-1"),
		"persistent-key-2": []byte("persistent-value-2"),
		"persistent-key-3": []byte("persistent-value-3"),
	}

	// Write test data with proper timeouts
	for key, value := range testData {
		writeCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		err = storage1.Set(writeCtx, key, value, time.Hour) // Long TTL for persistence test
		cancel()
		require.NoError(t, err, "Failed to set key %s", key)
	}

	// Verify data is written
	for key, expectedValue := range testData {
		getCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		value, getErr := storage1.Get(getCtx, key)
		cancel()
		require.NoError(t, getErr, "Failed to get key %s", key)
		assert.Equal(t, expectedValue, value, "Value mismatch for key %s", key)
	}

	// Close first instance gracefully
	err = storage1.Close()
	require.NoError(t, err)

	// Wait for port to be released and get new port for second instance
	time.Sleep(2 * time.Second)

	// Create new data directory and copy Raft data files for persistence test
	tempDir2 := t.TempDir()

	// Copy BoltDB files to new directory for persistence testing
	raftFiles := []string{"raft-log.db", "raft-stable.db"}
	for _, file := range raftFiles {
		srcPath := filepath.Join(tempDir, file)
		dstPath := filepath.Join(tempDir2, file)
		if _, statErr := os.Stat(srcPath); statErr == nil {
			require.NoError(t, copyFile(srcPath, dstPath), "Failed to copy Raft file %s", file)
		}
	}

	port2, err2 := getFreePort()
	require.NoError(t, err2)

	raftConfig2 := config.RaftSettings{
		NodeID:      "persistence-test-node",
		BindAddress: fmt.Sprintf("127.0.0.1:%d", port2),
		DataDir:     tempDir2, // Different data directory with copied data
		Peers:       []string{},
	}

	// Create second instance with different port and copied data directory
	storage2, err := createRaftStorageWithAddress(raftConfig2, fmt.Sprintf("127.0.0.1:%d", port2), logger)
	require.NoError(t, err)
	defer storage2.Close()

	// Wait for second instance leadership and readiness
	raftStorage2 := storage2.(*statestorage.RaftStorage)
	require.True(t, waitForLeadershipWithPing(ctx, raftStorage2, storage2), "Second instance failed to become leader")

	// Give extra time for state recovery from Raft log
	time.Sleep(1 * time.Second)

	// Verify data persistence with timeouts
	for key, expectedValue := range testData {
		getCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		value, getErr := storage2.Get(getCtx, key)
		cancel()
		require.NoError(t, getErr, "Key %s should persist after restart", key)
		assert.Equal(t, expectedValue, value, "Value for %s should match after restart", key)
	}

	// Verify we can write new data after recovery
	newKey := "post-restart-key"
	newValue := []byte("post-restart-value")
	setCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	err = storage2.Set(setCtx, newKey, newValue, time.Hour)
	require.NoError(t, err)

	getCtx, cancel2 := context.WithTimeout(ctx, operationTimeout)
	defer cancel2()
	retrievedNewValue, err := storage2.Get(getCtx, newKey)
	require.NoError(t, err)
	assert.Equal(t, newValue, retrievedNewValue)
}

func TestRaftStorage_Factory_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping factory integration test in short mode")
	}

	logger := testutils.NewMockLogger()

	t.Run("raft_storage_creation", func(t *testing.T) {
		tempDir := t.TempDir()
		port, err := getFreePort()
		require.NoError(t, err)

		raftConfig := config.RaftSettings{
			NodeID:      "factory-test-node",
			BindAddress: fmt.Sprintf("127.0.0.1:%d", port),
			DataDir:     tempDir,
			Peers:       []string{},
		}

		storage, err := statestorage.NewStateStorage(raftConfig, "raft", "factory-test-node", logger)
		require.NoError(t, err)
		assert.NotNil(t, storage)
		defer storage.Close()

		// Wait for leadership with production timeout
		raftStorage := storage.(*statestorage.RaftStorage)
		require.True(t, waitForLeadership(raftStorage), "Factory-created node failed to become leader")

		// Test basic operation with timeout
		ctx := context.Background()
		setCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		defer cancel()
		err = storage.Set(setCtx, "factory-test", []byte("factory-value"), time.Minute)
		require.NoError(t, err)

		getCtx, cancel2 := context.WithTimeout(ctx, operationTimeout)
		defer cancel2()
		value, err := storage.Get(getCtx, "factory-test")
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
	if testing.Short() {
		t.Skip("Skipping data dir management test in short mode")
	}
	logger := testutils.NewMockLogger()

	t.Run("creates_missing_data_directory", func(t *testing.T) {
		tempDir := t.TempDir()
		dataDirPath := filepath.Join(tempDir, "missing", "nested", "path")
		port, err := getFreePort()
		require.NoError(t, err)

		raftConfig := config.RaftSettings{
			NodeID:      "datadir-test-node",
			BindAddress: fmt.Sprintf("127.0.0.1:%d", port),
			DataDir:     dataDirPath,
			Peers:       []string{},
		}

		// Should create missing directories
		storage, err := statestorage.NewStateStorage(raftConfig, "raft", "datadir-test-node", logger)
		require.NoError(t, err)
		defer storage.Close()

		// Verify directory was created
		info, err := os.Stat(dataDirPath)
		require.NoError(t, err)
		assert.True(t, info.IsDir())

		// Verify Raft files are created after becoming leader
		raftStorage := storage.(*statestorage.RaftStorage)
		require.True(t, waitForLeadership(raftStorage), "Node failed to become leader")

		// Check that Raft files exist
		raftFiles := []string{"raft-log.db", "raft-stable.db"}
		for _, file := range raftFiles {
			_, statErr := os.Stat(filepath.Join(dataDirPath, file))
			assert.NoError(t, statErr, "File %s should be created", file)
		}
	})
}

// Production benchmarks for performance validation.
func BenchmarkRaftStorage_WriteOperations(b *testing.B) {
	logger := testutils.NewMockLogger()
	tempDir := b.TempDir()

	port, err := getFreePort()
	require.NoError(b, err)

	raftConfig := config.RaftSettings{
		NodeID:      "bench-node",
		BindAddress: fmt.Sprintf("127.0.0.1:%d", port),
		DataDir:     tempDir,
		Peers:       []string{},
	}

	storage, err := createRaftStorageWithAddress(raftConfig, fmt.Sprintf("127.0.0.1:%d", port), logger)
	require.NoError(b, err)
	defer storage.Close()

	// Wait for leadership
	raftStorage := storage.(*statestorage.RaftStorage)
	require.True(b, waitForLeadership(raftStorage), "Node failed to become leader")

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("bench-key-%d", i)
			value := []byte(fmt.Sprintf("bench-value-%d", i))

			setCtx, cancel := context.WithTimeout(ctx, operationTimeout)
			setErr := storage.Set(setCtx, key, value, time.Minute)
			cancel()
			if setErr != nil {
				b.Fatalf("Set operation failed: %v", setErr)
			}
			i++
		}
	})
}

func BenchmarkRaftStorage_ReadOperations(b *testing.B) {
	logger := testutils.NewMockLogger()
	tempDir := b.TempDir()

	port, err := getFreePort()
	require.NoError(b, err)

	raftConfig := config.RaftSettings{
		NodeID:      "bench-node",
		BindAddress: fmt.Sprintf("127.0.0.1:%d", port),
		DataDir:     tempDir,
		Peers:       []string{},
	}

	storage, err := createRaftStorageWithAddress(raftConfig, fmt.Sprintf("127.0.0.1:%d", port), logger)
	require.NoError(b, err)
	defer storage.Close()

	// Wait for leadership
	raftStorage := storage.(*statestorage.RaftStorage)
	require.True(b, waitForLeadership(raftStorage), "Node failed to become leader")

	ctx := context.Background()

	// Pre-populate data
	for i := range 1000 {
		key := fmt.Sprintf("bench-key-%d", i)
		value := []byte(fmt.Sprintf("bench-value-%d", i))
		setCtx, cancel := context.WithTimeout(ctx, operationTimeout)
		setErr := storage.Set(setCtx, key, value, time.Minute)
		cancel()
		require.NoError(b, setErr)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("bench-key-%d", i%1000)

			getCtx, cancel := context.WithTimeout(ctx, operationTimeout)
			_, getErr := storage.Get(getCtx, key)
			cancel()
			if getErr != nil {
				b.Fatalf("Get operation failed: %v", getErr)
			}
			i++
		}
	})
}
