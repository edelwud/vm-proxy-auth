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

func TestAutoDiscoveryIntegration(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	t.Cleanup(cancel)

	t.Run("static_discovery_two_nodes", func(t *testing.T) {
		t.Parallel()
		testStaticDiscoveryTwoNodes(ctx, t, logger)
	})

	t.Run("mdns_discovery_two_nodes", func(t *testing.T) {
		t.Parallel()
		testMDNSDiscoveryTwoNodes(ctx, t)
	})
}

func testStaticDiscoveryTwoNodes(ctx context.Context, t *testing.T, logger domain.Logger) {
	port1, port2 := getFreePorts(t)
	node1MemberlistConfig, node1DiscoveryConfig := createStaticNode1Config(port1, port2)
	node2MemberlistConfig, node2DiscoveryConfig := createStaticNode2Config(port1, port2)
	runStaticDiscoveryTest(
		ctx,
		t,
		logger,
		node1MemberlistConfig,
		node1DiscoveryConfig,
		node2MemberlistConfig,
		node2DiscoveryConfig,
	)
}

func testMDNSDiscoveryTwoNodes(ctx context.Context, t *testing.T) {
	debugLogger := testutils.NewMockLogger()
	serviceName := "_mdns-integration-test._tcp"
	port1, port2 := getFreePorts(t)

	t.Logf("Using ports: Node1 memberlist=%d, Node2 memberlist=%d", port1, port2)

	node1MemberlistConfig, node1DiscoveryConfig := createMDNSNode1Config(port1, serviceName)
	node2MemberlistConfig, node2DiscoveryConfig := createMDNSNode2Config(port2, serviceName)
	runMDNSDiscoveryTest(
		ctx,
		t,
		debugLogger,
		node1MemberlistConfig,
		node1DiscoveryConfig,
		node2MemberlistConfig,
		node2DiscoveryConfig,
	)
}

func getFreePorts(t *testing.T) (int, int) {
	ports, err := testutils.GetFreePorts(2)
	require.NoError(t, err)
	return ports[0], ports[1]
}

func createStaticNode1Config(
	port1, port2 int,
) (config.MemberlistSettings, config.DiscoverySettings) {
	memberlistConfig := createMemberlistConfig(
		port1,
		"auto-test-node-1",
		"127.0.0.1",
		"integration-test",
	)
	discoveryConfig := config.DiscoverySettings{
		Enabled:   true,
		Providers: []string{"static"},
		Interval:  200 * time.Millisecond,
		Static: config.StaticDiscoveryConfig{
			Peers: []string{fmt.Sprintf("127.0.0.1:%d", port2)},
		},
	}
	return memberlistConfig, discoveryConfig
}

func createStaticNode2Config(
	port1, port2 int,
) (config.MemberlistSettings, config.DiscoverySettings) {
	memberlistConfig := createMemberlistConfig(
		port2,
		"auto-test-node-2",
		"127.0.0.1",
		"integration-test",
	)
	discoveryConfig := config.DiscoverySettings{
		Enabled:   true,
		Providers: []string{"static"},
		Interval:  200 * time.Millisecond,
		Static: config.StaticDiscoveryConfig{
			Peers: []string{fmt.Sprintf("127.0.0.1:%d", port1)},
		},
	}
	return memberlistConfig, discoveryConfig
}

func createMDNSNode1Config(
	port1 int,
	serviceName string,
) (config.MemberlistSettings, config.DiscoverySettings) {
	memberlistConfig := createMemberlistConfig(port1, "mdns-auto-node-1", "0.0.0.0", "mdns-test")
	discoveryConfig := createMDNSDiscoveryConfig(
		serviceName,
		port1,
		fmt.Sprintf("node-1-%d", port1),
	)
	return memberlistConfig, discoveryConfig
}

func createMDNSNode2Config(
	port2 int,
	serviceName string,
) (config.MemberlistSettings, config.DiscoverySettings) {
	memberlistConfig := createMemberlistConfig(port2, "mdns-auto-node-2", "0.0.0.0", "mdns-test")
	discoveryConfig := createMDNSDiscoveryConfig(
		serviceName,
		port2,
		fmt.Sprintf("node-2-%d", port2),
	)
	return memberlistConfig, discoveryConfig
}

func createMemberlistConfig(
	port int,
	nodeName, bindAddress, environment string,
) config.MemberlistSettings {
	return config.MemberlistSettings{
		BindAddress:      bindAddress,
		BindPort:         port,
		AdvertiseAddress: "",
		AdvertisePort:    port,
		GossipInterval:   100 * time.Millisecond,
		GossipNodes:      3,
		ProbeInterval:    1 * time.Second,
		ProbeTimeout:     500 * time.Millisecond,
		Metadata: map[string]string{
			"node_name":   nodeName,
			"role":        "peer",
			"environment": environment,
		},
	}
}

func createMDNSDiscoveryConfig(
	serviceName string,
	port int,
	hostname string,
) config.DiscoverySettings {
	return config.DiscoverySettings{
		Enabled:   true,
		Providers: []string{"mdns"},
		Interval:  200 * time.Millisecond,
		MDNS: config.MDNSDiscoveryConfig{
			ServiceName: serviceName,
			Domain:      "local.",
			Hostname:    hostname,
			Port:        port,
		},
	}
}

func runStaticDiscoveryTest(
	ctx context.Context,
	t *testing.T,
	logger domain.Logger,
	node1MemberlistConfig config.MemberlistSettings,
	node1DiscoveryConfig config.DiscoverySettings,
	node2MemberlistConfig config.MemberlistSettings,
	node2DiscoveryConfig config.DiscoverySettings,
) {
	node1, node2 := createAndStartNodes(
		ctx,
		t,
		logger,
		node1MemberlistConfig,
		node1DiscoveryConfig,
		node2MemberlistConfig,
		node2DiscoveryConfig,
		"node",
	)
	defer node1.Stop()
	defer node2.Stop()

	waitForDiscovery(t, node1, node2, 3*time.Second)
	verifyClusterFormation(t, node1, node2, "auto-test-node-1", "auto-test-node-2")
}

func runMDNSDiscoveryTest(
	ctx context.Context,
	t *testing.T,
	logger domain.Logger,
	node1MemberlistConfig config.MemberlistSettings,
	node1DiscoveryConfig config.DiscoverySettings,
	node2MemberlistConfig config.MemberlistSettings,
	node2DiscoveryConfig config.DiscoverySettings,
) {
	node1, node2 := createAndStartMDNSNodes(
		ctx,
		t,
		logger,
		node1MemberlistConfig,
		node1DiscoveryConfig,
		node2MemberlistConfig,
		node2DiscoveryConfig,
	)
	defer node1.Stop()
	defer node2.Stop()

	waitForDiscovery(t, node1, node2, 3*time.Second)
	verifyClusterFormation(t, node1, node2, "mdns-auto-node-1", "mdns-auto-node-2")
}

func createAndStartNodes(ctx context.Context, t *testing.T, logger domain.Logger,
	node1MemberlistConfig config.MemberlistSettings, node1DiscoveryConfig config.DiscoverySettings,
	node2MemberlistConfig config.MemberlistSettings, node2DiscoveryConfig config.DiscoverySettings,
	logPrefix string,
) (*memberlist.Service, *memberlist.Service) {
	// Create nodes
	node1, err := memberlist.NewMemberlistService(
		node1MemberlistConfig,
		logger.With(domain.Field{Key: "node", Value: logPrefix + "1"}),
	)
	require.NoError(t, err)

	node2, err := memberlist.NewMemberlistService(
		node2MemberlistConfig,
		logger.With(domain.Field{Key: "node", Value: logPrefix + "2"}),
	)
	require.NoError(t, err)

	// Create discovery services
	node1Discovery := discovery.NewService(
		node1DiscoveryConfig,
		logger.With(domain.Field{Key: "discovery", Value: logPrefix + "1"}),
	)
	node2Discovery := discovery.NewService(
		node2DiscoveryConfig,
		logger.With(domain.Field{Key: "discovery", Value: logPrefix + "2"}),
	)

	// Connect discovery to memberlist
	node1.SetDiscoveryService(node1Discovery)
	node2.SetDiscoveryService(node2Discovery)

	// Start both nodes
	err = node1.Start(ctx)
	require.NoError(t, err)

	err = node2.Start(ctx)
	require.NoError(t, err)

	return node1, node2
}

func createAndStartMDNSNodes(
	ctx context.Context,
	t *testing.T,
	logger domain.Logger,
	node1MemberlistConfig config.MemberlistSettings,
	node1DiscoveryConfig config.DiscoverySettings,
	node2MemberlistConfig config.MemberlistSettings,
	node2DiscoveryConfig config.DiscoverySettings,
) (*memberlist.Service, *memberlist.Service) {
	// Create nodes
	node1, err := memberlist.NewMemberlistService(
		node1MemberlistConfig,
		logger.With(domain.Field{Key: "node", Value: "mdns1"}),
	)
	require.NoError(t, err)

	node2, err := memberlist.NewMemberlistService(
		node2MemberlistConfig,
		logger.With(domain.Field{Key: "node", Value: "mdns2"}),
	)
	require.NoError(t, err)

	// Create discovery services
	node1Discovery := discovery.NewService(
		node1DiscoveryConfig,
		logger.With(domain.Field{Key: "discovery", Value: "mdns1"}),
	)
	node2Discovery := discovery.NewService(
		node2DiscoveryConfig,
		logger.With(domain.Field{Key: "discovery", Value: "mdns2"}),
	)

	// Connect discovery to memberlist
	node1.SetDiscoveryService(node1Discovery)
	node2.SetDiscoveryService(node2Discovery)

	// Start Node 1 first
	t.Log("Starting Node 1...")
	err = node1.Start(ctx)
	require.NoError(t, err)

	t.Log("Node 1 established, waiting before starting Node 2...")
	time.Sleep(200 * time.Millisecond)

	// Start Node 2
	t.Log("Starting Node 2...")
	err = node2.Start(ctx)
	require.NoError(t, err)

	t.Log("Waiting for mDNS autodiscovery...")
	time.Sleep(800 * time.Millisecond)

	return node1, node2
}

func waitForDiscovery(t *testing.T, node1, node2 *memberlist.Service, timeout time.Duration) {
	require.Eventually(t, func() bool {
		node1Members := node1.GetMembers()
		node2Members := node2.GetMembers()
		return len(node1Members) == 2 && len(node2Members) == 2
	}, timeout, 100*time.Millisecond, "Both nodes should discover each other")
}

func verifyClusterFormation(
	t *testing.T,
	node1, node2 *memberlist.Service,
	node1Name, node2Name string,
) {
	node1Members := node1.GetMembers()
	node2Members := node2.GetMembers()

	t.Logf("Node 1 cluster size: %d", len(node1Members))
	t.Logf("Node 2 cluster size: %d", len(node2Members))

	assert.Len(t, node1Members, 2, "Node 1 should see both members")
	assert.Len(t, node2Members, 2, "Node 2 should see both members")

	// Check that both nodes are present
	node1Names := make([]string, len(node1Members))
	for i, member := range node1Members {
		node1Names[i] = member.Name
	}
	assert.Contains(t, node1Names, node1Name)
	assert.Contains(t, node1Names, node2Name)

	node2Names := make([]string, len(node2Members))
	for i, member := range node2Members {
		node2Names[i] = member.Name
	}
	assert.Contains(t, node2Names, node1Name)
	assert.Contains(t, node2Names, node2Name)
}
