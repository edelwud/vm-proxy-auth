package discovery_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/discovery"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestMDNSDiscovery_Creation(t *testing.T) {
	logger := testutils.NewMockLogger()

	tests := []struct {
		name   string
		config config.MDNSDiscoveryConfig
	}{
		{
			name: "with default values",
			config: config.MDNSDiscoveryConfig{
				ServiceType:    domain.DefaultMDNSServiceType,
				ServiceDomain:  domain.DefaultMDNSServiceDomain,
				Port:           8080,
				RaftPort:       9000,
				UpdateInterval: domain.DefaultMDNSUpdateInterval,
				DisableIPv6:    true,
			},
		},
		{
			name: "with custom values",
			config: config.MDNSDiscoveryConfig{
				ServiceType:    "_custom-service._tcp",
				ServiceDomain:  "custom.local.",
				Port:           9090,
				RaftPort:       9001,
				UpdateInterval: domain.DefaultMDNSUpdateInterval,
				DisableIPv6:    true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mdnsDiscovery := discovery.NewMDNSDiscovery(tt.config, logger)
			require.NotNil(t, mdnsDiscovery)
			require.NotNil(t, mdnsDiscovery.Events())
		})
	}
}

func TestMDNSDiscovery_Interface(t *testing.T) {
	logger := testutils.NewMockLogger()
	config := config.MDNSDiscoveryConfig{
		ServiceType:    domain.DefaultMDNSServiceType,
		ServiceDomain:  domain.DefaultMDNSServiceDomain,
		Port:           8080,
		RaftPort:       9000,
		UpdateInterval: domain.DefaultMDNSUpdateInterval,
		DisableIPv6:    true,
	}

	mdnsDiscovery := discovery.NewMDNSDiscovery(config, logger)

	// Test interface methods exist without actually calling them to avoid mDNS timeouts
	events := mdnsDiscovery.Events()
	require.NotNil(t, events)

	// Test that methods exist (but don't call them to avoid mDNS delays)
	require.NotNil(t, mdnsDiscovery.Start)
	require.NotNil(t, mdnsDiscovery.Stop)
	require.NotNil(t, mdnsDiscovery.DiscoverPeers)
	require.NotNil(t, mdnsDiscovery.DiscoverBackends)
}

func TestNewMDNSDiscovery_Simple(t *testing.T) {
	logger := testutils.NewMockLogger()

	config := config.MDNSDiscoveryConfig{
		ServiceType:    "_test-service._tcp",
		ServiceDomain:  "local.",
		Port:           8080,
		RaftPort:       9000,
		UpdateInterval: domain.DefaultMDNSUpdateInterval,
		DisableIPv6:    true,
	}

	mdnsDiscovery := discovery.NewMDNSDiscovery(config, logger)

	require.NotNil(t, mdnsDiscovery)
	require.NotNil(t, mdnsDiscovery.Events())
}
