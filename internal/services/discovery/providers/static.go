package providers

import (
	"context"
	"net"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// StaticProvider implements static peer list discovery.
type StaticProvider struct {
	logger domain.Logger
	peers  []string
}

// NewStaticProvider creates a new static discovery provider.
func NewStaticProvider(peers []string, logger domain.Logger) domain.DiscoveryProvider {
	return &StaticProvider{
		logger: logger.With(domain.Field{Key: "component", Value: "static"}),
		peers:  peers,
	}
}

// Discover implements DiscoveryProvider for static peer list.
func (s *StaticProvider) Discover(_ context.Context) ([]string, error) {
	// Validate all peer addresses
	var validPeers []string
	for _, peer := range s.peers {
		if _, _, err := net.SplitHostPort(peer); err != nil {
			s.logger.Warn("Invalid peer address format",
				domain.Field{Key: "address", Value: peer},
				domain.Field{Key: "error", Value: err.Error()})
			continue
		}
		validPeers = append(validPeers, peer)
	}

	if len(validPeers) > 0 {
		s.logger.Debug("Static discovery returning configured peers",
			domain.Field{Key: "peers_count", Value: len(validPeers)})
	}

	return validPeers, nil
}

// Start implements DiscoveryProvider - no-op for static provider.
func (s *StaticProvider) Start(_ context.Context) error {
	s.logger.Debug("Static provider started (no-op)")
	return nil
}

// Stop implements DiscoveryProvider - no-op for static provider.
func (s *StaticProvider) Stop() error {
	s.logger.Debug("Static provider stopped (no-op)")
	return nil
}

// GetProviderType returns the provider type name.
func (s *StaticProvider) GetProviderType() string {
	return "static"
}
