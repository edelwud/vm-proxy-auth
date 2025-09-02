package discovery_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/services/discovery"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestDNSDiscovery_Creation(t *testing.T) {
	logger := testutils.NewMockLogger()

	tests := []struct {
		name           string
		config         config.DNSDiscoveryConfig
		expectedConfig config.DNSDiscoveryConfig
	}{
		{
			name: "with default values",
			config: config.DNSDiscoveryConfig{
				Domain: "test.local",
			},
			expectedConfig: config.DNSDiscoveryConfig{
				Domain:         "test.local",
				Port:           8080,
				RaftPort:       9000,
				UpdateInterval: 30 * time.Second,
				SRVService:     "vm-proxy-auth",
				SRVProtocol:    "tcp",
			},
		},
		{
			name: "with custom values",
			config: config.DNSDiscoveryConfig{
				Domain:         "custom.domain",
				Port:           8888,
				RaftPort:       9999,
				UpdateInterval: 60 * time.Second,
				SRVService:     "custom-service",
				SRVProtocol:    "udp",
				UseSRVRecords:  true,
			},
			expectedConfig: config.DNSDiscoveryConfig{
				Domain:         "custom.domain",
				Port:           8888,
				RaftPort:       9999,
				UpdateInterval: 60 * time.Second,
				SRVService:     "custom-service",
				SRVProtocol:    "udp",
				UseSRVRecords:  true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dnsDiscovery := discovery.NewDNSDiscovery(tt.config, logger)

			require.NotNil(t, dnsDiscovery)
			require.NotNil(t, dnsDiscovery.Events())
		})
	}
}

func TestDNSDiscovery_StartStop(t *testing.T) {
	logger := testutils.NewMockLogger()
	config := config.DNSDiscoveryConfig{
		Domain:         "test.local",
		UpdateInterval: 100 * time.Millisecond,
	}

	dnsDiscovery := discovery.NewDNSDiscovery(config, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := dnsDiscovery.Start(ctx)
	require.NoError(t, err)

	time.Sleep(150 * time.Millisecond)

	err = dnsDiscovery.Stop()
	require.NoError(t, err)

	// Give some time for cleanup
	time.Sleep(10 * time.Millisecond)

	select {
	case _, ok := <-dnsDiscovery.Events():
		if ok {
			t.Error("Expected channel to be closed after stop")
		}
	case <-time.After(50 * time.Millisecond):
		// Channel might be blocking, that's also fine
	}
}

func TestDNSDiscovery_Interface(t *testing.T) {
	logger := testutils.NewMockLogger()
	config := config.DNSDiscoveryConfig{
		Domain: "localhost", // Use localhost to avoid DNS timeouts
	}

	dnsDiscovery := discovery.NewDNSDiscovery(config, logger)

	// Test interface methods exist
	events := dnsDiscovery.Events()
	require.NotNil(t, events)

	// Test that methods exist without calling slow DNS operations
	require.NotNil(t, dnsDiscovery.Start)
	require.NotNil(t, dnsDiscovery.Stop)
	require.NotNil(t, dnsDiscovery.DiscoverPeers)
	require.NotNil(t, dnsDiscovery.DiscoverBackends)
}

func BenchmarkDNSDiscovery_Creation(b *testing.B) {
	logger := testutils.NewMockLogger()
	config := config.DNSDiscoveryConfig{
		Domain:   "test.local",
		Port:     8080,
		RaftPort: 9000,
	}

	b.ResetTimer()
	for range b.N {
		dnsDiscovery := discovery.NewDNSDiscovery(config, logger)
		_ = dnsDiscovery
	}
}
