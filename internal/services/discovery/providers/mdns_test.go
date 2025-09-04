//nolint:testpackage // Testing internal methods requires same package
package providers

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

// getFreePortForMDNS returns a free port for mDNS testing.
func getFreePortForMDNS() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port, nil
}

func TestMDNSProvider_Creation(t *testing.T) {
	logger := testutils.NewMockLogger()

	tests := []struct {
		name        string
		serviceName string
		domainName  string
		hostname    string
		port        int
		expectType  string
	}{
		{
			name:        "valid_creation_with_hostname",
			serviceName: "_test-service._tcp",
			domainName:  "local.",
			hostname:    "test-host",
			port:        8080,
			expectType:  "mdns",
		},
		{
			name:        "valid_creation_with_ip",
			serviceName: "_vm-proxy-auth._tcp",
			domainName:  "local.",
			hostname:    "127.0.0.1",
			port:        7946,
			expectType:  "mdns",
		},
		{
			name:        "zero_port",
			serviceName: "_test._tcp",
			domainName:  "local.",
			hostname:    "localhost",
			port:        0,
			expectType:  "mdns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewMDNSProvider(tt.serviceName, tt.domainName, tt.hostname, tt.port, nil, logger)
			require.NotNil(t, provider)
			assert.Equal(t, tt.expectType, provider.GetProviderType())
		})
	}
}

func TestMDNSProvider_StartStop(t *testing.T) {
	logger := testutils.NewMockLogger()

	port, err := getFreePortForMDNS()
	require.NoError(t, err)

	provider := NewMDNSProvider("_test-service._tcp", "local.", "127.0.0.1", port, nil, logger)
	require.NotNil(t, provider)

	ctx := context.Background()

	t.Run("start_success", func(t *testing.T) {
		startErr := provider.Start(ctx)
		require.NoError(t, startErr)

		// Should not allow double start
		doubleStartErr := provider.Start(ctx)
		require.Error(t, doubleStartErr)
		assert.Contains(t, doubleStartErr.Error(), "already running")

		// Clean stop
		err = provider.Stop()
		require.NoError(t, err)
	})

	t.Run("stop_before_start", func(t *testing.T) {
		// Should be safe to stop before start
		stopErr := provider.Stop()
		require.NoError(t, stopErr)
	})

	t.Run("multiple_stops", func(t *testing.T) {
		startErr := provider.Start(ctx)
		require.NoError(t, startErr)

		err = provider.Stop()
		require.NoError(t, err)

		// Multiple stops should be safe
		err = provider.Stop()
		require.NoError(t, err)
	})
}

func TestMDNSProvider_Discover(t *testing.T) {
	logger := testutils.NewMockLogger()

	port1, err := getFreePortForMDNS()
	require.NoError(t, err)
	port2, err := getFreePortForMDNS()
	require.NoError(t, err)

	t.Run("discover_self", func(t *testing.T) {
		// Test discovering self through mDNS
		provider := NewMDNSProvider("_mdns-unit-test._tcp", "local.", "127.0.0.1", port1, nil, logger)
		require.NotNil(t, provider)

		ctx := context.Background()

		// Start the provider to announce itself
		startErr := provider.Start(ctx)
		require.NoError(t, startErr)
		defer func() {
			stopErr := provider.Stop()
			require.NoError(t, stopErr)
		}()

		// Give mDNS time to start announcing
		time.Sleep(1 * time.Second)

		// Discover peers
		peers, discoverErr := provider.Discover(ctx)
		require.NoError(t, discoverErr)

		// Should find exactly itself since we're the only one with this service name
		assert.Len(t, peers, 1, "Should discover exactly self")

		// Log discovered peers for debugging
		t.Logf("Discovered %d peers via mDNS", len(peers))
		for i, peer := range peers {
			t.Logf("  Peer %d: %s", i, peer)
		}
	})

	t.Run("discover_with_timeout", func(t *testing.T) {
		provider := NewMDNSProvider("_timeout-test._tcp", "local.", "127.0.0.1", port2, nil, logger)
		require.NotNil(t, provider)

		// Create context with short timeout
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Start provider
		startErr := provider.Start(ctx)
		require.NoError(t, startErr)
		defer func() {
			stopErr := provider.Stop()
			require.NoError(t, stopErr)
		}()

		// Discover should complete within timeout
		start := time.Now()
		peers, discoverErr := provider.Discover(ctx)
		duration := time.Since(start)

		require.NoError(t, discoverErr)
		assert.LessOrEqual(t, duration.Seconds(), 3.0, "Discovery should complete within timeout")

		t.Logf("Discovery completed in %v with %d peers", duration, len(peers))
	})

	t.Run("discover_without_server", func(t *testing.T) {
		// Test discovery without starting server (client-only mode)
		provider := NewMDNSProvider("_client-only-test._tcp", "local.", "127.0.0.1", port1+100, nil, logger)
		require.NotNil(t, provider)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// Discover without starting server
		peers, discoverErr := provider.Discover(ctx)
		require.NoError(t, discoverErr)
		assert.Empty(t, peers, "Client-only mode without server should find nothing")

		t.Logf("Client-only discovery found %d peers", len(peers))
	})
}

func TestMDNSProvider_GetLocalIP(t *testing.T) {
	logger := testutils.NewMockLogger()
	provider := &MDNSProvider{logger: logger}

	t.Run("get_local_ip_success", func(t *testing.T) {
		ip, err := provider.getLocalIP()
		require.NoError(t, err, "getLocalIP must succeed in test environment")

		assert.NotNil(t, ip)
		assert.NotNil(t, ip.To4(), "Should return IPv4 address")
		assert.False(t, ip.IsLoopback(), "Should not return loopback address")

		t.Logf("Detected local IP: %s", ip.String())
	})
}

func TestMDNSProvider_Integration_TwoProviders(t *testing.T) {
	// Always run integration tests - they're critical for correctness

	logger := testutils.NewMockLogger()

	port1, err := getFreePortForMDNS()
	require.NoError(t, err)
	port2, err := getFreePortForMDNS()
	require.NoError(t, err)

	// Use same service name for inter-discovery
	serviceName := "_mdns-integration-test._tcp"

	provider1 := NewMDNSProvider(
		serviceName,
		"local.",
		fmt.Sprintf("test-node-1-%d", port1),
		port1,
		nil,
		logger.With(domain.Field{Key: "provider", Value: "1"}),
	)
	provider2 := NewMDNSProvider(
		serviceName,
		"local.",
		fmt.Sprintf("test-node-2-%d", port2),
		port2,
		nil,
		logger.With(domain.Field{Key: "provider", Value: "2"}),
	)

	ctx := context.Background()

	t.Run("two_providers_announce_and_discover", func(t *testing.T) {
		// Start both providers
		startErr1 := provider1.Start(ctx)
		require.NoError(t, startErr1)
		defer func() {
			stopErr := provider1.Stop()
			require.NoError(t, stopErr)
		}()

		startErr2 := provider2.Start(ctx)
		require.NoError(t, startErr2)
		defer func() {
			stopErr := provider2.Stop()
			require.NoError(t, stopErr)
		}()

		// Give time for mDNS registration and propagation
		t.Log("Waiting for mDNS service registration...")
		time.Sleep(5 * time.Second)

		// Both providers should discover peers
		t.Log("Starting discovery from provider 1...")
		peers1, discoverErr1 := provider1.Discover(ctx)
		require.NoError(t, discoverErr1)

		t.Log("Starting discovery from provider 2...")
		peers2, discoverErr2 := provider2.Discover(ctx)
		require.NoError(t, discoverErr2)

		t.Logf("Provider 1 discovered %d peers", len(peers1))
		for i, peer := range peers1 {
			t.Logf("  Provider 1 Peer %d: %s", i, peer)
		}

		t.Logf("Provider 2 discovered %d peers", len(peers2))
		for i, peer := range peers2 {
			t.Logf("  Provider 2 Peer %d: %s", i, peer)
		}

		// Each provider should discover both services (including itself)
		// since they share the same service name

		// Provider 1 should find both services
		assert.Len(t, peers1, 2, "Provider 1 should find both providers")
		foundPorts1 := make(map[int]bool)
		for _, peer := range peers1 {
			// Verify proper formatting and extract port
			_, portStr, splitErr := net.SplitHostPort(peer)
			require.NoError(t, splitErr, "Peer address should be properly formatted: %s", peer)
			port, _ := strconv.Atoi(portStr)
			foundPorts1[port] = true
		}

		// Provider 2 should find both services
		assert.Len(t, peers2, 2, "Provider 2 should find both providers")
		foundPorts2 := make(map[int]bool)
		for _, peer := range peers2 {
			// Verify proper formatting and extract port
			_, portStr, splitErr := net.SplitHostPort(peer)
			require.NoError(t, splitErr, "Peer address should be properly formatted: %s", peer)
			port, _ := strconv.Atoi(portStr)
			foundPorts2[port] = true
		}

		// Verify that each provider finds both services (by port since IP may vary)
		// mDNS cross-discovery MUST work after the port fix
		assert.Contains(t, foundPorts1, port1, "Provider 1 should find service on port1")
		assert.Contains(t, foundPorts1, port2, "Provider 1 should find service on port2")
		assert.Contains(t, foundPorts2, port1, "Provider 2 should find service on port1")
		assert.Contains(t, foundPorts2, port2, "Provider 2 should find service on port2")
	})

	t.Run("sequential_discovery", func(t *testing.T) {
		// Test sequential startup to see if timing affects discovery
		seqServiceName := "_mdns-sequential-test._tcp"

		seqProvider1 := NewMDNSProvider(
			seqServiceName,
			"local.",
			"sequential-discovery-1",
			port1+100,
			nil,
			logger.With(domain.Field{Key: "provider", Value: "seq1"}),
		)
		seqProvider2 := NewMDNSProvider(
			seqServiceName,
			"local.",
			"sequential-discovery-2",
			port2+100,
			nil,
			logger.With(domain.Field{Key: "provider", Value: "seq2"}),
		)

		// Start provider 1 first
		startErr1 := seqProvider1.Start(ctx)
		require.NoError(t, startErr1)
		defer func() {
			stopErr := seqProvider1.Stop()
			require.NoError(t, stopErr)
		}()

		t.Log("Provider 1 started, waiting before starting provider 2...")
		time.Sleep(3 * time.Second)

		// Start provider 2 after delay
		startErr2 := seqProvider2.Start(ctx)
		require.NoError(t, startErr2)
		defer func() {
			stopErr := seqProvider2.Stop()
			require.NoError(t, stopErr)
		}()

		t.Log("Provider 2 started, waiting for cross-discovery...")
		time.Sleep(5 * time.Second)

		// Test discovery from both
		peers1, discoverErr1 := seqProvider1.Discover(ctx)
		require.NoError(t, discoverErr1)

		peers2, discoverErr2 := seqProvider2.Discover(ctx)
		require.NoError(t, discoverErr2)

		t.Logf("Sequential test - Provider 1 found %d peers", len(peers1))
		for i, peer := range peers1 {
			t.Logf("  Sequential Provider 1 Peer %d: %s", i, peer)
		}

		t.Logf("Sequential test - Provider 2 found %d peers", len(peers2))
		for i, peer := range peers2 {
			t.Logf("  Sequential Provider 2 Peer %d: %s", i, peer)
		}

		// Both should find each other even with sequential startup
		assert.Len(t, peers1, 2, "Provider 1 should find both providers")
		assert.Len(t, peers2, 2, "Provider 2 should find both providers")

		t.Log("✅ Sequential mDNS cross-discovery successful")
	})
}

func TestMDNSProvider_ErrorCases(t *testing.T) {
	logger := testutils.NewMockLogger()

	t.Run("invalid_service_name", func(t *testing.T) {
		// mDNS library should handle invalid service names gracefully
		provider := NewMDNSProvider("invalid-service-name", "local.", "127.0.0.1", 8080, nil, logger)
		require.NotNil(t, provider)

		ctx := context.Background()
		err := provider.Start(ctx)
		// May fail due to invalid service name format
		if err != nil {
			t.Logf("Expected error for invalid service name: %v", err)
		}

		_ = provider.Stop() // Always safe to call
	})

	t.Run("invalid_port", func(t *testing.T) {
		provider := NewMDNSProvider("_test._tcp", "local.", "127.0.0.1", -1, nil, logger)
		require.NotNil(t, provider)

		ctx := context.Background()
		err := provider.Start(ctx)
		// May fail due to invalid port
		if err != nil {
			t.Logf("Expected error for invalid port: %v", err)
		}

		_ = provider.Stop()
	})
}

func TestMDNSProvider_ConcurrentOperations(t *testing.T) {
	logger := testutils.NewMockLogger()

	port, err := getFreePortForMDNS()
	require.NoError(t, err)

	provider := NewMDNSProvider("_concurrent-test._tcp", "local.", "concurrent-test", port, nil, logger)
	require.NotNil(t, provider)

	ctx := context.Background()

	t.Run("concurrent_start_stop", func(t *testing.T) {
		// Start provider
		startErr := provider.Start(ctx)
		require.NoError(t, startErr)

		// Run concurrent discoveries
		done := make(chan struct{})
		const numGoroutines = 5

		for i := range numGoroutines {
			go func(id int) {
				defer func() {
					done <- struct{}{}
				}()

				discoverCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()

				peers, discErr := provider.Discover(discoverCtx)
				if discErr != nil {
					t.Logf("Goroutine %d discovery error: %v", id, discErr)
					return
				}

				t.Logf("Goroutine %d found %d peers", id, len(peers))
			}(i)
		}

		// Wait for all goroutines
		for range numGoroutines {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("Timeout waiting for concurrent discovery operations")
			}
		}

		// Stop provider
		err = provider.Stop()
		require.NoError(t, err)
	})
}

func TestMDNSProvider_EdgeCases(t *testing.T) {
	logger := testutils.NewMockLogger()

	t.Run("discover_with_cancelled_context", func(t *testing.T) {
		provider := NewMDNSProvider("_cancel-test._tcp", "local.", "discover-with-cancel", 8080, nil, logger)
		require.NotNil(t, provider)

		// Create cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Discover should handle cancelled context gracefully
		peers, err := provider.Discover(ctx)
		require.NoError(t, err) // Should not error on cancelled context

		// With cancelled context, should return empty slice (not nil)
		assert.NotNil(t, peers, "Should return empty slice, not nil")
		assert.Empty(t, peers, "Cancelled context should return no peers")

		t.Logf("Discovery with cancelled context returned %d peers", len(peers))
	})

	t.Run("provider_type_consistency", func(t *testing.T) {
		provider := NewMDNSProvider("_type-test._tcp", "local.", "provider-type-consistency", 8080, nil, logger)
		require.NotNil(t, provider)

		// Provider type should be consistent
		providerType := provider.GetProviderType()
		assert.Equal(t, "mdns", providerType)
		assert.Equal(t, providerType, provider.GetProviderType(), "Provider type should be consistent")
	})
}

func TestMDNSProvider_ServiceNameFormats(t *testing.T) {
	logger := testutils.NewMockLogger()

	validServiceNames := []string{
		"_vm-proxy-auth._tcp",
		"_test-service._tcp",
		"_app._tcp",
		"_discovery._tcp",
		"_cluster._tcp",
	}

	for _, serviceName := range validServiceNames {
		t.Run(fmt.Sprintf("service_%s", serviceName), func(t *testing.T) {
			provider := NewMDNSProvider(serviceName, "local.", "valid-service-names", 8080, nil, logger)
			require.NotNil(t, provider)
			assert.Equal(t, "mdns", provider.GetProviderType())

			// Should be able to create without errors
			ctx := context.Background()
			err := provider.Start(ctx)
			if err != nil {
				t.Logf("Start failed for service %s: %v", serviceName, err)
			}

			_ = provider.Stop() // Always safe
		})
	}
}
