package providers_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/services/discovery/providers"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestStaticProvider_Discover(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		peers         []string
		expectedPeers []string
	}{
		{
			name:          "valid peers",
			peers:         []string{"127.0.0.1:7946", "127.0.0.1:7947"},
			expectedPeers: []string{"127.0.0.1:7946", "127.0.0.1:7947"},
		},
		{
			name:          "empty peers",
			peers:         []string{},
			expectedPeers: nil,
		},
		{
			name:          "invalid peer format",
			peers:         []string{"invalid-address", "127.0.0.1:7946"},
			expectedPeers: []string{"127.0.0.1:7946"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger := testutils.NewMockLogger()
			provider := providers.NewStaticProvider(tt.peers, logger)

			ctx := context.Background()

			// Start provider
			err := provider.Start(ctx)
			require.NoError(t, err)

			// Discover peers
			discoveredPeers, err := provider.Discover(ctx)
			require.NoError(t, err)
			require.Equal(t, tt.expectedPeers, discoveredPeers)

			// Stop provider
			err = provider.Stop()
			require.NoError(t, err)

			// Verify provider type
			require.Equal(t, "static", provider.GetProviderType())
		})
	}
}
