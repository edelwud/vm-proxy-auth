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

// mockPeerJoiner implements the PeerJoiner interface for testing.
type mockPeerJoiner struct {
	joinedPeers []string
}

func (m *mockPeerJoiner) Join(peers []string) error {
	m.joinedPeers = append(m.joinedPeers, peers...)
	return nil
}

func TestDiscoveryService_StaticProvider(t *testing.T) {
	logger := testutils.NewMockLogger()

	cfg := config.DiscoverySettings{
		Enabled:   true,
		Providers: []string{"static"},
		Interval:  1 * time.Second,
		Static: config.StaticDiscoveryConfig{
			Peers: []string{"127.0.0.1:7946", "127.0.0.1:7947"},
		},
	}

	service := discovery.NewService(cfg, logger)

	// Setup mock peer joiner
	mockJoiner := &mockPeerJoiner{}
	service.SetPeerJoiner(mockJoiner)

	ctx := context.Background()

	// Start service
	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() {
		stopErr := service.Stop()
		require.NoError(t, stopErr)
	}()

	// Wait a bit for discovery to run
	time.Sleep(100 * time.Millisecond)

	// Verify peers were discovered
	require.NotEmpty(t, mockJoiner.joinedPeers)
	require.Contains(t, mockJoiner.joinedPeers, "127.0.0.1:7946")
	require.Contains(t, mockJoiner.joinedPeers, "127.0.0.1:7947")
}

func TestDiscoveryService_Disabled(t *testing.T) {
	logger := testutils.NewMockLogger()

	cfg := config.DiscoverySettings{
		Enabled: false,
	}

	service := discovery.NewService(cfg, logger)

	ctx := context.Background()

	// Start service
	err := service.Start(ctx)
	require.NoError(t, err)

	// Stop service
	err = service.Stop()
	require.NoError(t, err)
}

func TestDiscoveryService_NoProviders(t *testing.T) {
	logger := testutils.NewMockLogger()

	cfg := config.DiscoverySettings{
		Enabled:   true,
		Providers: []string{}, // No providers
		Interval:  1 * time.Second,
	}

	service := discovery.NewService(cfg, logger)

	ctx := context.Background()

	// Start service
	err := service.Start(ctx)
	require.NoError(t, err)

	// Stop service
	err = service.Stop()
	require.NoError(t, err)
}
