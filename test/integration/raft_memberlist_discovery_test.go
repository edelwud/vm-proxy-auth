package integration_test

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
	"github.com/edelwud/vm-proxy-auth/internal/services/discovery"
	"github.com/edelwud/vm-proxy-auth/internal/services/memberlist"
	"github.com/edelwud/vm-proxy-auth/internal/services/statestorage"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestRaftMemberlistDiscoveryIntegration(t *testing.T) {
	logger := testutils.NewMockLogger()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create temporary directories for Raft data
	tempDir := t.TempDir()
	node1DataDir := filepath.Join(tempDir, "node1")
	node2DataDir := filepath.Join(tempDir, "node2")

	err := os.MkdirAll(node1DataDir, 0o755)
	require.NoError(t, err)
	err = os.MkdirAll(node2DataDir, 0o755)
	require.NoError(t, err)

	t.Run("two_node_raft_cluster_with_autodiscovery", func(t *testing.T) {
		// Get free ports for Raft and memberlist
		raftPort1, raftErr1 := getFreePort()
		require.NoError(t, raftErr1)
		raftPort2, raftErr2 := getFreePort()
		require.NoError(t, raftErr2)
		memberPort1, memberErr1 := getFreePort()
		require.NoError(t, memberErr1)
		memberPort2, memberErr2 := getFreePort()
		require.NoError(t, memberErr2)

		// Node 1 Raft configuration
		raftConfig1 := config.RaftSettings{
			NodeID:            "node-1",
			BindAddress:       fmt.Sprintf("127.0.0.1:%d", raftPort1),
			DataDir:           node1DataDir,
			Peers:             []string{},
			BootstrapExpected: 2,
		}

		// Node 2 Raft configuration
		raftConfig2 := config.RaftSettings{
			NodeID:            "node-2",
			BindAddress:       fmt.Sprintf("127.0.0.1:%d", raftPort2),
			DataDir:           node2DataDir,
			Peers:             []string{},
			BootstrapExpected: 2,
		}

		// Create Raft storages using factory
		raftStorage1, raftErr1 := statestorage.NewStateStorage(
			raftConfig1,
			"raft",
			"node-1",
			logger.With(domain.Field{Key: "node", Value: "raft1"}),
		)
		require.NoError(t, raftErr1)
		defer func() {
			if closeErr := raftStorage1.Close(); closeErr != nil {
				t.Logf("Error closing raft storage 1: %v", closeErr)
			}
		}()

		raftStorage2, raftErr2 := statestorage.NewStateStorage(
			raftConfig2,
			"raft",
			"node-2",
			logger.With(domain.Field{Key: "node", Value: "raft2"}),
		)
		require.NoError(t, raftErr2)
		defer func() {
			if closeErr := raftStorage2.Close(); closeErr != nil {
				t.Logf("Error closing raft storage 2: %v", closeErr)
			}
		}()

		// Cast to RaftStorage to access Raft-specific methods
		raftStorage1Impl := raftStorage1.(*statestorage.RaftStorage)
		raftStorage2Impl := raftStorage2.(*statestorage.RaftStorage)

		// Create node metadata
		nodeMetadata1, metaErr1 := memberlist.CreateNodeMetadata(
			"node-1",
			"127.0.0.1:8080",
			fmt.Sprintf("127.0.0.1:%d", raftPort1),
			map[string]string{"role": "peer", "environment": "test"},
		)
		require.NoError(t, metaErr1)

		nodeMetadata2, metaErr2 := memberlist.CreateNodeMetadata(
			"node-2",
			"127.0.0.1:8081",
			fmt.Sprintf("127.0.0.1:%d", raftPort2),
			map[string]string{"role": "peer", "environment": "test"},
		)
		require.NoError(t, metaErr2)

		// Node 1 memberlist configuration
		memberConfig1 := config.MemberlistSettings{
			BindAddress:      "127.0.0.1",
			BindPort:         memberPort1,
			AdvertiseAddress: "127.0.0.1",
			AdvertisePort:    memberPort1,
			GossipInterval:   200 * time.Millisecond,
			GossipNodes:      3,
			ProbeInterval:    1 * time.Second,
			ProbeTimeout:     500 * time.Millisecond,
			Metadata: map[string]string{
				"node_name":   "raft-test-node-1",
				"role":        "peer",
				"environment": "integration-test",
			},
		}

		// Node 2 memberlist configuration
		memberConfig2 := config.MemberlistSettings{
			BindAddress:      "127.0.0.1",
			BindPort:         memberPort2,
			AdvertiseAddress: "127.0.0.1",
			AdvertisePort:    memberPort2,
			GossipInterval:   200 * time.Millisecond,
			GossipNodes:      3,
			ProbeInterval:    1 * time.Second,
			ProbeTimeout:     500 * time.Millisecond,
			Metadata: map[string]string{
				"node_name":   "raft-test-node-2",
				"role":        "peer",
				"environment": "integration-test",
			},
		}

		// Discovery configurations
		discoveryConfig1 := config.DiscoverySettings{
			Enabled:   true,
			Providers: []string{"static"},
			Interval:  3 * time.Second,
			Static: config.StaticDiscoveryConfig{
				Peers: []string{fmt.Sprintf("127.0.0.1:%d", memberPort2)},
			},
		}

		discoveryConfig2 := config.DiscoverySettings{
			Enabled:   true,
			Providers: []string{"static"},
			Interval:  3 * time.Second,
			Static: config.StaticDiscoveryConfig{
				Peers: []string{fmt.Sprintf("127.0.0.1:%d", memberPort1)},
			},
		}

		// Create memberlist services with Raft integration
		memberlist1, memberErr1 := memberlist.NewMemberlistServiceWithMetadata(
			memberConfig1,
			logger.With(domain.Field{Key: "node", Value: "memberlist1"}),
			nodeMetadata1,
			raftStorage1Impl,
		)
		require.NoError(t, memberErr1)
		defer memberlist1.Stop()

		memberlist2, memberErr2 := memberlist.NewMemberlistServiceWithMetadata(
			memberConfig2,
			logger.With(domain.Field{Key: "node", Value: "memberlist2"}),
			nodeMetadata2,
			raftStorage2Impl,
		)
		require.NoError(t, memberErr2)
		defer memberlist2.Stop()

		// Create discovery services
		discovery1 := discovery.NewService(
			discoveryConfig1,
			logger.With(domain.Field{Key: "discovery", Value: "node1"}),
		)
		discovery2 := discovery.NewService(
			discoveryConfig2,
			logger.With(domain.Field{Key: "discovery", Value: "node2"}),
		)

		// Connect discovery to Raft for delayed bootstrap
		discovery1.SetRaftManager(raftStorage1Impl)
		discovery2.SetRaftManager(raftStorage2Impl)

		// Connect discovery to memberlist
		memberlist1.SetDiscoveryService(discovery1)
		memberlist2.SetDiscoveryService(discovery2)

		// Start memberlist services (this also starts discovery)
		err = memberlist1.Start(ctx)
		require.NoError(t, err)

		err = memberlist2.Start(ctx)
		require.NoError(t, err)

		// Wait for cluster discovery and Raft bootstrap
		require.Eventually(t, func() bool {
			members1 := memberlist1.GetMembers()
			members2 := memberlist2.GetMembers()
			return len(members1) == 2 && len(members2) == 2
		}, 10*time.Second, 500*time.Millisecond, "Cluster formation should complete")

		// Check memberlist cluster formation
		members1 := memberlist1.GetMembers()
		members2 := memberlist2.GetMembers()

		// Both nodes should see each other in memberlist
		assert.Len(t, members1, 2, "Node 1 memberlist should see both members")
		assert.Len(t, members2, 2, "Node 2 memberlist should see both members")

		// Check Raft cluster state - at least one node should be leader
		isLeader1 := raftStorage1Impl.IsLeader()
		isLeader2 := raftStorage2Impl.IsLeader()
		hasLeader := isLeader1 || isLeader2
		assert.True(t, hasLeader, "At least one node should be Raft leader")

		// Test state storage operations
		testKey := "test-cluster-key"
		testValue := []byte("test-cluster-value")

		// Set value on node 1
		err = raftStorage1.Set(ctx, testKey, testValue, 10*time.Second)
		require.NoError(t, err)

		// Wait for Raft replication
		require.Eventually(t, func() bool {
			value2, getErr := raftStorage2.Get(ctx, testKey)
			return getErr == nil && string(value2) == string(testValue)
		}, 5*time.Second, 200*time.Millisecond, "Value should replicate across cluster")

		// Get value from node 2 - should be replicated
		value2, finalErr := raftStorage2.Get(ctx, testKey)
		require.NoError(t, finalErr)
		assert.Equal(t, testValue, value2, "Value should be replicated across Raft cluster")

		t.Log("✅ Full Raft + Memberlist + Discovery integration working correctly")
	})
}
