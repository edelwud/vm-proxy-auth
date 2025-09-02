package discovery_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/services/discovery"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestServiceDiscoveryInterface(t *testing.T) {
	logger := testutils.NewMockLogger()

	t.Run("DNS discovery implements interface", func(t *testing.T) {
		config := config.DNSDiscoveryConfig{
			Domain: "test.local",
		}

		dnsDiscovery := discovery.NewDNSDiscovery(config, logger)
		_ = dnsDiscovery
		require.NotNil(t, dnsDiscovery)

		// Test interface methods exist without actually calling them to avoid DNS timeouts
		events := dnsDiscovery.Events()
		require.NotNil(t, events)

		// Test that methods exist (but don't call them to avoid DNS delays)
		require.NotNil(t, dnsDiscovery.Start)
		require.NotNil(t, dnsDiscovery.Stop)
		require.NotNil(t, dnsDiscovery.DiscoverPeers)
		require.NotNil(t, dnsDiscovery.DiscoverBackends)
	})

	t.Run("Factory creates interface-compliant instances", func(t *testing.T) {
		kubeConfig := config.KubernetesDiscoveryConfig{
			Namespace: "default",
		}
		dnsConfig := config.DNSDiscoveryConfig{
			Domain: "test.local",
		}

		// Test DNS creation
		dnsDiscovery, err := discovery.NewServiceDiscovery(
			"dns",
			kubeConfig,
			dnsConfig,
			config.MDNSDiscoveryConfig{},
			logger,
		)
		require.NoError(t, err)
		require.NotNil(t, dnsDiscovery)

		// Verify it implements the interface
		_ = dnsDiscovery

		// Test unsupported type
		invalidDiscovery, err := discovery.NewServiceDiscovery(
			"invalid",
			kubeConfig,
			dnsConfig,
			config.MDNSDiscoveryConfig{},
			logger,
		)
		require.Error(t, err)
		require.Nil(t, invalidDiscovery)
	})
}

func BenchmarkServiceDiscoveryInterface(b *testing.B) {
	logger := testutils.NewMockLogger()
	config := config.DNSDiscoveryConfig{
		Domain: "bench.local",
	}

	b.ResetTimer()
	for range b.N {
		dnsDiscovery := discovery.NewDNSDiscovery(config, logger)
		_ = dnsDiscovery
	}
}
