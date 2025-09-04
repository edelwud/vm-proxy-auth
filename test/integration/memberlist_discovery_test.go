package integration_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/discovery"
	"github.com/edelwud/vm-proxy-auth/internal/services/memberlist"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestMemberlistDiscoveryIntegration(t *testing.T) {
	t.Parallel()
	logger := testutils.NewMockLogger()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)

	t.Run("two_nodes_discovery_via_static", func(t *testing.T) {
		t.Parallel()
		// Get free ports for both nodes
		port1, err := getFreePort()
		require.NoError(t, err)
		port2, err := getFreePort()
		require.NoError(t, err)

		// Node 1 configuration
		node1Config := config.MemberlistSettings{
			BindAddress:      "127.0.0.1",
			BindPort:         port1,
			AdvertiseAddress: "127.0.0.1",
			AdvertisePort:    port1,
			GossipInterval:   200 * time.Millisecond,
			GossipNodes:      3,
			ProbeInterval:    1 * time.Second,
			ProbeTimeout:     500 * time.Millisecond,
			Metadata: map[string]string{
				"node_name":   "test-node-1",
				"role":        "peer",
				"environment": "test",
			},
		}

		// Node 2 configuration
		node2Config := config.MemberlistSettings{
			BindAddress:      "127.0.0.1",
			BindPort:         port2,
			AdvertiseAddress: "127.0.0.1",
			AdvertisePort:    port2,
			GossipInterval:   200 * time.Millisecond,
			GossipNodes:      3,
			ProbeInterval:    1 * time.Second,
			ProbeTimeout:     500 * time.Millisecond,
			Metadata: map[string]string{
				"node_name":   "test-node-2",
				"role":        "peer",
				"environment": "test",
			},
		}

		// Create Node 1 memberlist service
		node1, err := memberlist.NewMemberlistService(
			node1Config,
			logger.With(domain.Field{Key: "node", Value: "node1"}),
		)
		require.NoError(t, err)
		defer node1.Stop()

		// Create Node 2 memberlist service
		node2, err := memberlist.NewMemberlistService(
			node2Config,
			logger.With(domain.Field{Key: "node", Value: "node2"}),
		)
		require.NoError(t, err)
		defer node2.Stop()

		// Start both nodes
		err = node1.Start(ctx)
		require.NoError(t, err)

		err = node2.Start(ctx)
		require.NoError(t, err)

		// Node 1 should see only itself initially
		node1Members := node1.GetMembers()
		assert.Len(t, node1Members, 1, "Node 1 should see only itself initially")
		assert.Equal(t, "test-node-1", node1Members[0].Name)

		// Node 2 should see only itself initially
		node2Members := node2.GetMembers()
		assert.Len(t, node2Members, 1, "Node 2 should see only itself initially")
		assert.Equal(t, "test-node-2", node2Members[0].Name)

		// Manual join - Node 2 joins Node 1
		joinNodes := []string{fmt.Sprintf("127.0.0.1:%d", port1)}
		err = node2.Join(joinNodes)
		require.NoError(t, err)

		// Wait for cluster formation
		require.Eventually(t, func() bool {
			members1 := node1.GetMembers()
			members2 := node2.GetMembers()
			return len(members1) == 2 && len(members2) == 2
		}, 5*time.Second, 100*time.Millisecond, "Both nodes should join cluster")

		// Both nodes should now see each other
		node1MembersAfter := node1.GetMembers()
		node2MembersAfter := node2.GetMembers()

		// Both should see 2 members
		assert.Len(t, node1MembersAfter, 2, "Node 1 should see both members")
		assert.Len(t, node2MembersAfter, 2, "Node 2 should see both members")

		// Check member names
		node1Names := make([]string, len(node1MembersAfter))
		for i, member := range node1MembersAfter {
			node1Names[i] = member.Name
		}
		assert.Contains(t, node1Names, "test-node-1")
		assert.Contains(t, node1Names, "test-node-2")
	})

	t.Run("discovery_service_integration", func(t *testing.T) {
		t.Parallel()
		// Get free port
		port, err := getFreePort()
		require.NoError(t, err)

		// Test discovery service with static provider
		discoveryConfig := config.DiscoverySettings{
			Enabled:   true,
			Providers: []string{"static"},
			Interval:  1 * time.Second,
			Static: config.StaticDiscoveryConfig{
				Peers: []string{fmt.Sprintf("127.0.0.1:%d", port)}, // Target for discovery
			},
		}

		memberlistConfig := config.MemberlistSettings{
			BindAddress:      "127.0.0.1",
			BindPort:         port,
			AdvertiseAddress: "127.0.0.1",
			AdvertisePort:    port,
			Metadata: map[string]string{
				"node_name": "discovery-test-node",
				"role":      "peer",
			},
		}

		// Create memberlist service
		mlService, err := memberlist.NewMemberlistService(memberlistConfig, logger)
		require.NoError(t, err)
		defer mlService.Stop()

		// Create discovery service
		discoveryService := discovery.NewService(discoveryConfig, logger)

		// Connect discovery to memberlist
		mlService.SetDiscoveryService(discoveryService)

		// Start memberlist (this should also start discovery)
		err = mlService.Start(ctx)
		require.NoError(t, err)

		// Wait for discovery cycle
		require.Eventually(t, func() bool {
			members := mlService.GetMembers()
			return len(members) == 1
		}, 5*time.Second, 200*time.Millisecond, "Single node should see itself")

		// Should have exactly 1 member (self) since no peers are configured
		members := mlService.GetMembers()
		assert.Len(t, members, 1, "Should have exactly self as member")
	})
}
