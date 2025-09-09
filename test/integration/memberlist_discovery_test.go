package integration_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config/modules/cluster"
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

	t.Run("memberlist_static_discovery_two_nodes", func(t *testing.T) {
		t.Parallel()
		// Get free ports for memberlist
		port1, err := testutils.GetFreePort()
		require.NoError(t, err)
		port2, err := testutils.GetFreePort()
		require.NoError(t, err)

		// Node 1 memberlist configuration
		memberConfig1 := cluster.MemberlistConfig{
			BindAddress:      fmt.Sprintf("127.0.0.1:%d", port1),
			AdvertiseAddress: fmt.Sprintf("127.0.0.1:%d", port1),
			Peers:            cluster.PeersConfig{},
			Gossip: cluster.GossipConfig{
				Interval: 200 * time.Millisecond,
				Nodes:    3,
			},
			Probe: cluster.ProbeConfig{
				Interval: 1 * time.Second,
				Timeout:  500 * time.Millisecond,
			},
			Metadata: map[string]string{
				"node_name":   "test-node-1",
				"role":        "peer",
				"environment": "integration-test",
			},
		}

		// Node 2 memberlist configuration
		memberConfig2 := cluster.MemberlistConfig{
			BindAddress:      fmt.Sprintf("127.0.0.1:%d", port2),
			AdvertiseAddress: fmt.Sprintf("127.0.0.1:%d", port2),
			Peers:            cluster.PeersConfig{},
			Gossip: cluster.GossipConfig{
				Interval: 200 * time.Millisecond,
				Nodes:    3,
			},
			Probe: cluster.ProbeConfig{
				Interval: 1 * time.Second,
				Timeout:  500 * time.Millisecond,
			},
			Metadata: map[string]string{
				"node_name":   "test-node-2",
				"role":        "peer",
				"environment": "integration-test",
			},
		}

		// Discovery configurations
		discoveryConfig1 := cluster.DiscoveryConfig{
			Enabled:   true,
			Providers: []string{"static"},
			Interval:  200 * time.Millisecond,
			Static: &cluster.StaticConfig{
				Peers: []string{fmt.Sprintf("127.0.0.1:%d", port2)},
			},
		}

		discoveryConfig2 := cluster.DiscoveryConfig{
			Enabled:   true,
			Providers: []string{"static"},
			Interval:  200 * time.Millisecond,
			Static: &cluster.StaticConfig{
				Peers: []string{fmt.Sprintf("127.0.0.1:%d", port1)},
			},
		}

		// Create memberlist services WITHOUT Raft integration
		memberlist1, err := memberlist.NewMemberlistService(
			memberConfig1,
			logger.With(domain.Field{Key: "node", Value: "memberlist1"}),
		)
		require.NoError(t, err)

		memberlist2, err := memberlist.NewMemberlistService(
			memberConfig2,
			logger.With(domain.Field{Key: "node", Value: "memberlist2"}),
		)
		require.NoError(t, err)

		// Create discovery services
		discovery1 := discovery.NewService(
			discoveryConfig1,
			logger.With(domain.Field{Key: "discovery", Value: "node1"}),
		)
		discovery2 := discovery.NewService(
			discoveryConfig2,
			logger.With(domain.Field{Key: "discovery", Value: "node2"}),
		)

		// Connect discovery to memberlist
		memberlist1.SetDiscoveryService(discovery1)
		memberlist2.SetDiscoveryService(discovery2)

		// Start memberlist services (this also starts discovery)
		err = memberlist1.Start(ctx)
		require.NoError(t, err)

		err = memberlist2.Start(ctx)
		require.NoError(t, err)

		// Wait for cluster discovery
		require.Eventually(t, func() bool {
			members1 := memberlist1.GetMembers()
			members2 := memberlist2.GetMembers()
			return len(members1) == 2 && len(members2) == 2
		}, 5*time.Second, 200*time.Millisecond, "Cluster formation should complete")

		// Check memberlist cluster formation
		members1 := memberlist1.GetMembers()
		members2 := memberlist2.GetMembers()

		// Both nodes should see each other in memberlist
		assert.Len(t, members1, 2, "Node 1 memberlist should see both members")
		assert.Len(t, members2, 2, "Node 2 memberlist should see both members")

		// Verify node names
		node1Names := make([]string, len(members1))
		for i, member := range members1 {
			node1Names[i] = member.Name
		}
		assert.Contains(t, node1Names, "test-node-1")
		assert.Contains(t, node1Names, "test-node-2")

		node2Names := make([]string, len(members2))
		for i, member := range members2 {
			node2Names[i] = member.Name
		}
		assert.Contains(t, node2Names, "test-node-1")
		assert.Contains(t, node2Names, "test-node-2")

		t.Log("✅ Memberlist + Discovery integration working correctly")

		// Clean shutdown in correct order
		// Stop memberlist first (this will stop discovery automatically)
		err = memberlist1.Stop()
		if err != nil {
			t.Logf("Warning: Error stopping memberlist1: %v", err)
		}

		err = memberlist2.Stop()
		if err != nil {
			t.Logf("Warning: Error stopping memberlist2: %v", err)
		}
	})
}
