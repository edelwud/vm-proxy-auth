package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	clusterconfig "github.com/edelwud/vm-proxy-auth/internal/config/modules/cluster"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/storage"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/discovery"
	"github.com/edelwud/vm-proxy-auth/internal/services/memberlist"
	"github.com/edelwud/vm-proxy-auth/internal/services/statestorage"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

// RaftDiscoveryNode represents a complete Raft node with memberlist and discovery.
type RaftDiscoveryNode struct {
	NodeID      string
	RaftStorage domain.StateStorage
	RaftImpl    *statestorage.RaftStorage
	Memberlist  *memberlist.Service
	Discovery   *discovery.Service
	DataDir     string
	RaftPort    int
	MemberPort  int
	t           *testing.T
}

// RaftDiscoveryCluster manages a cluster of Raft nodes with memberlist and discovery.
type RaftDiscoveryCluster struct {
	nodes   []*RaftDiscoveryNode
	tempDir string
	nodeIDs []string
	t       *testing.T
}

// NewRaftDiscoveryCluster creates a new Raft cluster with memberlist and discovery.
func NewRaftDiscoveryCluster(t *testing.T, nodeCount int) *RaftDiscoveryCluster {
	t.Helper()

	cluster := &RaftDiscoveryCluster{
		nodes:   make([]*RaftDiscoveryNode, nodeCount),
		nodeIDs: make([]string, nodeCount),
		t:       t,
	}

	// Create main temporary directory
	cluster.tempDir = t.TempDir()

	logger := testutils.NewMockLogger()

	// Get all ports first to avoid conflicts
	raftPorts := make([]int, nodeCount)
	memberPorts := make([]int, nodeCount)

	for i := range nodeCount {
		raftPort, err := testutils.GetFreePort()
		require.NoError(t, err, "Failed to get Raft port for node %d", i)
		raftPorts[i] = raftPort

		memberPort, err := testutils.GetFreePort()
		require.NoError(t, err, "Failed to get memberlist port for node %d", i)
		memberPorts[i] = memberPort
	}

	// Create nodes
	for i := range nodeCount {
		node := &RaftDiscoveryNode{
			NodeID:     fmt.Sprintf("node-%d", i),
			RaftPort:   raftPorts[i],
			MemberPort: memberPorts[i],
			t:          t,
		}
		cluster.nodeIDs[i] = node.NodeID

		// Create node data directory
		node.DataDir = filepath.Join(cluster.tempDir, node.NodeID)
		err := os.MkdirAll(node.DataDir, 0o755)
		require.NoError(t, err, "Failed to create data dir for node %d", i)

		// Configure Raft storage
		raftConfig := storage.RaftConfig{
			BindAddress:       fmt.Sprintf("127.0.0.1:%d", node.RaftPort),
			DataDir:           node.DataDir,
			Peers:             []string{}, // Empty - discovery will handle
			BootstrapExpected: nodeCount,
		}

		// Create Raft storage
		raftStorage, err := statestorage.NewStateStorage(
			raftConfig,
			"raft",
			node.NodeID,
			logger.With(domain.Field{Key: "node", Value: node.NodeID}),
		)
		require.NoError(t, err, "Failed to create Raft storage for node %d", i)
		node.RaftStorage = raftStorage
		node.RaftImpl = raftStorage.(*statestorage.RaftStorage)

		// Create node metadata for memberlist
		nodeMetadata, metaErr := memberlist.CreateNodeMetadata(
			node.NodeID,
			fmt.Sprintf("127.0.0.1:%d", 8080+i), // HTTP address for this node
			fmt.Sprintf("127.0.0.1:%d", node.RaftPort),
			map[string]string{"role": "peer", "environment": "test"},
		)
		require.NoError(t, metaErr, "Failed to create node metadata for node %d", i)

		// Configure memberlist
		memberConfig := clusterconfig.MemberlistConfig{
			BindAddress:      fmt.Sprintf("127.0.0.1:%d", node.MemberPort),
			AdvertiseAddress: fmt.Sprintf("127.0.0.1:%d", node.MemberPort),
			Peers:            clusterconfig.PeersConfig{},
			Gossip: clusterconfig.GossipConfig{
				Interval: 200 * time.Millisecond,
				Nodes:    3,
			},
			Probe: clusterconfig.ProbeConfig{
				Interval: 1 * time.Second,
				Timeout:  500 * time.Millisecond,
			},
			Metadata: map[string]string{
				"node_name":   fmt.Sprintf("test-node-%d", i),
				"role":        "peer",
				"environment": "integration-test",
			},
		}

		// Create memberlist service
		memberlistService, memberErr := memberlist.NewMemberlistServiceWithMetadata(
			memberConfig,
			logger.With(domain.Field{Key: "node", Value: fmt.Sprintf("memberlist-%d", i)}),
			nodeMetadata,
			node.RaftImpl,
		)
		require.NoError(t, memberErr, "Failed to create memberlist for node %d", i)
		node.Memberlist = memberlistService

		// Build static peers list for discovery (other nodes)
		var staticPeers []string
		for j := range nodeCount {
			if i != j {
				staticPeers = append(staticPeers, fmt.Sprintf("127.0.0.1:%d", memberPorts[j]))
			}
		}

		// Configure discovery
		discoveryConfig := clusterconfig.DiscoveryConfig{
			Enabled:   true,
			Providers: []string{"static"},
			Interval:  200 * time.Millisecond,
			Static: &clusterconfig.StaticConfig{
				Peers: staticPeers,
			},
		}

		// Create discovery service
		discoveryService := discovery.NewService(
			discoveryConfig,
			logger.With(domain.Field{Key: "discovery", Value: fmt.Sprintf("node-%d", i)}),
		)
		node.Discovery = discoveryService

		// Connect discovery to Raft for delayed bootstrap
		discoveryService.SetRaftManager(node.RaftImpl)

		// Connect discovery to memberlist
		memberlistService.SetDiscoveryService(discoveryService)

		cluster.nodes[i] = node
	}

	// Setup cleanup
	t.Cleanup(cluster.cleanup)

	return cluster
}

// Start starts all nodes in the cluster.
func (c *RaftDiscoveryCluster) Start(ctx context.Context) error {
	c.t.Helper()

	// Start all memberlist services (this also starts discovery)
	for i, node := range c.nodes {
		err := node.Memberlist.Start(ctx)
		if err != nil {
			return fmt.Errorf("failed to start memberlist for node %d: %w", i, err)
		}
	}

	return nil
}

// WaitForClusterFormation waits for memberlist cluster formation.
func (c *RaftDiscoveryCluster) WaitForClusterFormation(timeout time.Duration) {
	c.t.Helper()

	require.Eventually(c.t, func() bool {
		expectedMembers := len(c.nodes)
		for _, node := range c.nodes {
			members := node.Memberlist.GetMembers()
			if len(members) != expectedMembers {
				return false
			}
		}
		return true
	}, timeout, 200*time.Millisecond, "Cluster formation should complete")
}

// WaitForRaftLeader waits for Raft leader election.
func (c *RaftDiscoveryCluster) WaitForRaftLeader(timeout time.Duration) *RaftDiscoveryNode {
	c.t.Helper()

	var leader *RaftDiscoveryNode
	require.Eventually(c.t, func() bool {
		for _, node := range c.nodes {
			if node.RaftImpl.IsLeader() {
				leader = node
				return true
			}
		}
		return false
	}, timeout, 200*time.Millisecond, "At least one node should become Raft leader")

	return leader
}

// GetNodes returns all nodes in the cluster.
func (c *RaftDiscoveryCluster) GetNodes() []*RaftDiscoveryNode {
	return c.nodes
}

// GetNodeByID returns a node by its ID.
func (c *RaftDiscoveryCluster) GetNodeByID(nodeID string) *RaftDiscoveryNode {
	for _, node := range c.nodes {
		if node.NodeID == nodeID {
			return node
		}
	}
	return nil
}

// TestReplication tests state replication across the cluster.
func (c *RaftDiscoveryCluster) TestReplication(ctx context.Context, testKey string, testValue []byte) {
	c.t.Helper()

	// Set value on first node
	err := c.nodes[0].RaftStorage.Set(ctx, testKey, testValue, 10*time.Second)
	require.NoError(c.t, err, "Failed to set value on first node")

	// Wait for replication to all other nodes
	for i := 1; i < len(c.nodes); i++ {
		node := c.nodes[i]
		require.Eventually(c.t, func() bool {
			value, getErr := node.RaftStorage.Get(ctx, testKey)
			return getErr == nil && string(value) == string(testValue)
		}, 3*time.Second, 100*time.Millisecond, "Value should replicate to node %d", i)
	}
}

// GetLeader returns the current Raft leader node.
func (c *RaftDiscoveryCluster) GetLeader() *RaftDiscoveryNode {
	for _, node := range c.nodes {
		if node.RaftImpl.IsLeader() {
			return node
		}
	}
	return nil
}

// GetFollowers returns all non-leader nodes.
func (c *RaftDiscoveryCluster) GetFollowers() []*RaftDiscoveryNode {
	var followers []*RaftDiscoveryNode
	for _, node := range c.nodes {
		if !node.RaftImpl.IsLeader() {
			followers = append(followers, node)
		}
	}
	return followers
}

// cleanup closes all nodes and cleans up resources.
func (c *RaftDiscoveryCluster) cleanup() {
	for i, node := range c.nodes {
		c.cleanupNode(i, node)
	}
	// Note: tempDir is cleaned up automatically by t.TempDir()
}

// cleanupNode cleans up a single node.
func (c *RaftDiscoveryCluster) cleanupNode(index int, node *RaftDiscoveryNode) {
	if node == nil {
		return
	}

	c.stopMemberlist(index, node)
	c.closeRaftStorage(index, node)
}

// stopMemberlist stops memberlist service for a node.
func (c *RaftDiscoveryCluster) stopMemberlist(index int, node *RaftDiscoveryNode) {
	if node.Memberlist == nil {
		return
	}

	if stopErr := node.Memberlist.Stop(); stopErr != nil {
		c.t.Logf("Error stopping memberlist for node %d: %v", index, stopErr)
	}
}

// closeRaftStorage closes raft storage for a node.
func (c *RaftDiscoveryCluster) closeRaftStorage(index int, node *RaftDiscoveryNode) {
	if node.RaftStorage == nil {
		return
	}

	if closeErr := node.RaftStorage.Close(); closeErr != nil {
		c.t.Logf("Error closing Raft storage for node %d: %v", index, closeErr)
	}
}

// CreateSimpleRaftDiscoveryCluster creates a 4-node cluster for basic testing.
func CreateSimpleRaftDiscoveryCluster(t *testing.T) *RaftDiscoveryCluster {
	t.Helper()
	return NewRaftDiscoveryCluster(t, 4)
}

// CreateTestRaftDiscoveryCluster creates a 3-node cluster for comprehensive testing.
func CreateTestRaftDiscoveryCluster(t *testing.T) *RaftDiscoveryCluster {
	t.Helper()
	return NewRaftDiscoveryCluster(t, 3)
}
