package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/discovery"
	memberlistsvc "github.com/edelwud/vm-proxy-auth/internal/services/memberlist"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestMDNSBroadcast_TwoNodesDiscovery(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)

	// Get free ports for memberlist
	port1, err := testutils.GetFreePort()
	require.NoError(t, err)
	port2, err := testutils.GetFreePort()
	require.NoError(t, err)

	t.Logf("Using memberlist ports: Node1=%d, Node2=%d", port1, port2)

	t.Run("mdns_with_broadcast_address", func(t *testing.T) {
		t.Parallel()
		// Node 1 configuration - uses 0.0.0.0 for mDNS broadcast discovery
		node1MemberlistConfig := config.MemberlistSettings{
			BindAddress:      "0.0.0.0", // Bind to all interfaces
			BindPort:         port1,
			AdvertiseAddress: "127.0.0.1", // Advertise specific address
			AdvertisePort:    port1,
			GossipInterval:   100 * time.Millisecond,
			GossipNodes:      3,
			ProbeInterval:    1 * time.Second,
			ProbeTimeout:     500 * time.Millisecond,
			Metadata: map[string]string{
				"node_name":   "broadcast-node-1",
				"role":        "peer",
				"environment": "broadcast-test",
			},
		}

		node1DiscoveryConfig := config.DiscoverySettings{
			Enabled:   true,
			Providers: []string{"mdns"},
			Interval:  150 * time.Millisecond,
			MDNS: config.MDNSDiscoveryConfig{
				ServiceName: "_broadcast-vm-proxy._tcp",
				Domain:      "local.",
				Hostname:    "0.0.0.0", // Use broadcast address for mDNS
				Port:        port1,
			},
		}

		// Node 2 configuration - also uses 0.0.0.0 for mDNS broadcast discovery
		node2MemberlistConfig := config.MemberlistSettings{
			BindAddress:      "0.0.0.0", // Bind to all interfaces
			BindPort:         port2,
			AdvertiseAddress: "127.0.0.1", // Advertise specific address
			AdvertisePort:    port2,
			GossipInterval:   100 * time.Millisecond,
			GossipNodes:      3,
			ProbeInterval:    1 * time.Second,
			ProbeTimeout:     500 * time.Millisecond,
			Metadata: map[string]string{
				"node_name":   "broadcast-node-2",
				"role":        "peer",
				"environment": "broadcast-test",
			},
		}

		node2DiscoveryConfig := config.DiscoverySettings{
			Enabled:   true,
			Providers: []string{"mdns"},
			Interval:  150 * time.Millisecond,
			MDNS: config.MDNSDiscoveryConfig{
				ServiceName: "_broadcast-vm-proxy._tcp", // Same service name
				Domain:      "local.",
				Hostname:    "0.0.0.0", // Use broadcast address for mDNS
				Port:        port2,
			},
		}

		// Create Node 1
		t.Log("Creating Node 1 with broadcast mDNS...")
		node1, node1Err := memberlistsvc.NewMemberlistService(
			node1MemberlistConfig,
			logger.With(domain.Field{Key: "node", Value: "bcast1"}),
		)
		require.NoError(t, node1Err)
		defer node1.Stop()

		// Create Node 2
		t.Log("Creating Node 2 with broadcast mDNS...")
		node2, node2Err := memberlistsvc.NewMemberlistService(
			node2MemberlistConfig,
			logger.With(domain.Field{Key: "node", Value: "bcast2"}),
		)
		require.NoError(t, node2Err)
		defer node2.Stop()

		// Create discovery services
		t.Log("Creating discovery services with 0.0.0.0 addresses...")
		node1Discovery := discovery.NewService(
			node1DiscoveryConfig,
			logger.With(domain.Field{Key: "discovery", Value: "bcast1"}),
		)
		node2Discovery := discovery.NewService(
			node2DiscoveryConfig,
			logger.With(domain.Field{Key: "discovery", Value: "bcast2"}),
		)

		// Connect discovery to memberlist
		node1.SetDiscoveryService(node1Discovery)
		node2.SetDiscoveryService(node2Discovery)

		// Start Node 1 first and let it establish broadcast presence
		t.Log("Starting Node 1...")
		err = node1.Start(ctx)
		require.NoError(t, err)

		t.Log("Node 1 established, waiting before starting Node 2...")
		time.Sleep(200 * time.Millisecond)

		// Start Node 2
		t.Log("Starting Node 2...")
		err = node2.Start(ctx)
		require.NoError(t, err)

		// Monitor broadcast discovery process
		t.Log("Monitoring mDNS broadcast discovery...")

		// Wait for discovery with eventually check - mDNS can be slow
		require.Eventually(t, func() bool {
			members1 := node1.GetMembers()
			members2 := node2.GetMembers()
			t.Logf("Discovery progress: Node1=%d members, Node2=%d members", len(members1), len(members2))
			return len(members1) >= 2 && len(members2) >= 2
		}, 15*time.Second, 500*time.Millisecond, "Broadcast discovery should work")

		// Get final state for detailed analysis
		finalMembers1 := node1.GetMembers()
		finalMembers2 := node2.GetMembers()

		// Log member details
		t.Logf("Node 1 members:")
		for i, member := range finalMembers1 {
			t.Logf("  %d: %s (%s:%d)", i, member.Name, member.Addr.String(), member.Port)
		}

		t.Logf("Node 2 members:")
		for i, member := range finalMembers2 {
			t.Logf("  %d: %s (%s:%d)", i, member.Name, member.Addr.String(), member.Port)
		}

		// Final analysis
		t.Logf("\n📊 Final Results:")
		t.Logf("Node 1 final members: %d", len(finalMembers1))
		t.Logf("Node 2 final members: %d", len(finalMembers2))

		// Expected behavior with broadcast mDNS: each discovery MUST find 2 nodes
		require.Len(t, finalMembers1, 2, "Node 1 must find both nodes with broadcast mDNS")
		require.Len(t, finalMembers2, 2, "Node 2 must find both nodes with broadcast mDNS")

		t.Log("✅ SUCCESS: mDNS broadcast discovery working - both nodes found each other")

		// Verify both node names are present
		node1Names := make([]string, len(finalMembers1))
		for i, member := range finalMembers1 {
			node1Names[i] = member.Name
		}
		assert.Contains(t, node1Names, "broadcast-node-1")
		assert.Contains(t, node1Names, "broadcast-node-2")

		node2Names := make([]string, len(finalMembers2))
		for i, member := range finalMembers2 {
			node2Names[i] = member.Name
		}
		assert.Contains(t, node2Names, "broadcast-node-1")
		assert.Contains(t, node2Names, "broadcast-node-2")

		// Overall test success criteria
		totalUniqueNodes := make(map[string]bool)
		for _, member := range finalMembers1 {
			totalUniqueNodes[member.Name] = true
		}
		for _, member := range finalMembers2 {
			totalUniqueNodes[member.Name] = true
		}

		assert.Len(t, totalUniqueNodes, 2, "Must discover exactly 2 unique nodes total")
		t.Logf("✅ Total unique nodes discovered: %d", len(totalUniqueNodes))
	})
}
