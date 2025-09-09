package discovery

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/memberlist"

	"github.com/edelwud/vm-proxy-auth/internal/config/modules/cluster"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

const (
	// defaultStopTimeout is the maximum time to wait for graceful shutdown.
	defaultStopTimeout = 2 * time.Second
	// defaultDiscoveryTimeout is the maximum time for each discovery cycle.
	defaultDiscoveryTimeout = 5 * time.Second
)

// Service implements automatic peer discovery service.
type Service struct {
	logger     domain.Logger
	factory    *Factory
	providers  []domain.DiscoveryProvider
	peerJoiner interface {
		Join(peers []string) error
	}
	memberGetter interface {
		GetMembers() []*memberlist.Node
	}
	raftManager interface {
		TryDelayedBootstrap(discoveredPeers []string) error
	}

	config  cluster.DiscoveryConfig
	stopCh  chan struct{}
	wg      sync.WaitGroup
	running bool
	mu      sync.RWMutex
}

// NewService creates a new discovery service.
func NewService(cfg cluster.DiscoveryConfig, logger domain.Logger) *Service {
	return &Service{
		logger:  logger.With(domain.Field{Key: "component", Value: "discovery"}),
		factory: NewFactory(logger),
		config:  cfg,
		stopCh:  make(chan struct{}),
	}
}

// SetPeerJoiner sets the peer joiner (typically memberlist service).
func (s *Service) SetPeerJoiner(joiner interface{}) {
	if j, ok := joiner.(interface{ Join([]string) error }); ok {
		s.peerJoiner = j
	}
	if mg, ok := joiner.(interface{ GetMembers() []*memberlist.Node }); ok {
		s.memberGetter = mg
	}
}

// SetRaftManager sets the Raft manager for delayed bootstrap coordination.
func (s *Service) SetRaftManager(manager interface{}) {
	if rm, ok := manager.(interface{ TryDelayedBootstrap([]string) error }); ok {
		s.raftManager = rm
	}
}

// Start starts the discovery service.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return errors.New("discovery service already running")
	}

	if !s.config.Enabled {
		s.logger.Info("Discovery service disabled")
		return nil
	}

	// Create providers
	discoveryProviders, err := s.factory.CreateProviders(s.config)
	if err != nil {
		return fmt.Errorf("failed to create discovery providers: %w", err)
	}

	s.providers = discoveryProviders

	if len(s.providers) == 0 {
		s.logger.Info("No discovery providers available, discovery disabled")
		return nil
	}

	// Start all providers
	for _, provider := range s.providers {
		if startErr := provider.Start(ctx); startErr != nil {
			s.logger.Error("Failed to start discovery provider",
				domain.Field{Key: "provider", Value: provider.GetProviderType()},
				domain.Field{Key: "error", Value: startErr.Error()})
			continue
		}
	}

	s.running = true

	s.logger.Info("Discovery service started",
		domain.Field{Key: "providers_count", Value: len(s.providers)},
		domain.Field{Key: "interval", Value: s.config.Interval.String()})

	// Run initial discovery
	s.runDiscovery(ctx)

	// Start discovery loop - it will use stopCh for cancellation
	s.wg.Add(1)
	go s.discoveryLoop()

	return nil
}

// Stop stops the discovery service.
func (s *Service) Stop() error {
	s.mu.Lock()

	if !s.running {
		s.mu.Unlock()
		return nil
	}

	s.running = false

	// Close stop channel to signal loop to exit
	if s.stopCh != nil {
		close(s.stopCh)
		s.stopCh = nil // Prevent double close
	}

	// Stop all providers before unlocking to avoid race
	for _, provider := range s.providers {
		if err := provider.Stop(); err != nil {
			s.logger.Warn("Error stopping discovery provider",
				domain.Field{Key: "provider", Value: provider.GetProviderType()},
				domain.Field{Key: "error", Value: err.Error()})
		}
	}

	s.mu.Unlock() // Unlock before waiting to avoid deadlock

	// Wait for discovery loop to finish with timeout to avoid hanging
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("Discovery service stopped")
	case <-time.After(defaultStopTimeout):
		s.logger.Warn("Discovery service stop timeout - forcefully terminating")
	}

	return nil
}

// discoveryLoop periodically runs discovery across all providers.
func (s *Service) discoveryLoop() {
	defer s.wg.Done()

	s.logger.Debug("Starting discovery loop",
		domain.Field{Key: "interval", Value: s.config.Interval.String()})

	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()

	// Create a context for discovery operations
	ctx := context.Background()

	for {
		select {
		case <-ticker.C:
			s.logger.Debug("Ticker fired - starting discovery cycle")
			// Create a timeout context for each discovery cycle
			discoveryCtx, cancel := context.WithTimeout(ctx, defaultDiscoveryTimeout)
			s.runDiscovery(discoveryCtx)
			cancel()
		case <-func() <-chan struct{} {
			s.mu.RLock()
			defer s.mu.RUnlock()
			return s.stopCh
		}():
			s.logger.Debug("Discovery loop stopped via stop channel")
			return
		}
	}
}

// runDiscovery executes discovery across all providers.
func (s *Service) runDiscovery(ctx context.Context) {
	if s.peerJoiner == nil {
		s.logger.Debug("No peer joiner configured, skipping discovery")
		return
	}

	var allPeers []string

	for _, provider := range s.providers {
		peers, err := provider.Discover(ctx)
		if err != nil {
			s.logger.Warn("Discovery provider failed",
				domain.Field{Key: "provider", Value: provider.GetProviderType()},
				domain.Field{Key: "error", Value: err.Error()})
			continue
		}

		s.logger.Debug("Discovery provider found peers",
			domain.Field{Key: "provider", Value: provider.GetProviderType()},
			domain.Field{Key: "peers_count", Value: len(peers)})

		allPeers = append(allPeers, peers...)
	}

	if len(allPeers) == 0 {
		s.logger.Debug("No peers discovered")
		return
	}

	// Remove duplicates and filter out self
	uniquePeers := s.filterPeers(allPeers)
	if len(uniquePeers) == 0 {
		s.logger.Debug("No new peers after filtering")
		return
	}

	s.logger.Debug("Attempting to join discovered peers",
		domain.Field{Key: "peers_count", Value: len(uniquePeers)},
		domain.Field{Key: "peers", Value: uniquePeers})

	// Attempt to join discovered peers
	if err := s.peerJoiner.Join(uniquePeers); err != nil {
		s.logger.Warn("Failed to join discovered peers",
			domain.Field{Key: "error", Value: err.Error()})
		return
	}

	// Try delayed bootstrap if Raft manager is available
	if s.raftManager != nil {
		if err := s.raftManager.TryDelayedBootstrap(uniquePeers); err != nil {
			s.logger.Warn("Failed to try delayed bootstrap",
				domain.Field{Key: "error", Value: err.Error()})
		} else {
			s.logger.Debug("Raft delayed bootstrap attempted",
				domain.Field{Key: "bootstrap_peers", Value: len(uniquePeers)})
		}
	}

	s.logger.Debug("Successfully processed peer discovery",
		domain.Field{Key: "joined_peers", Value: len(uniquePeers)})
}

// filterPeers removes duplicates and filters out self.
func (s *Service) filterPeers(peers []string) []string {
	seen := make(map[string]bool)
	var unique []string

	for _, peer := range peers {
		// Skip if already seen
		if seen[peer] {
			continue
		}
		seen[peer] = true

		// Skip if it's our own address (basic check)
		if s.isSelfAddress(peer) {
			continue
		}

		// Skip if already connected
		if s.isAlreadyConnected(peer) {
			continue
		}

		unique = append(unique, peer)
	}

	return unique
}

// isSelfAddress checks if the address belongs to this instance.
func (s *Service) isSelfAddress(_ string) bool {
	// Simple self-detection - for now just return false
	// This can be improved with actual local address checking
	return false
}

// isAlreadyConnected checks if we're already connected to a peer address.
func (s *Service) isAlreadyConnected(peerAddr string) bool {
	if s.memberGetter == nil {
		return false
	}

	members := s.memberGetter.GetMembers()
	for _, member := range members {
		memberAddr := fmt.Sprintf("%s:%d", member.Addr.String(), member.Port)
		if memberAddr == peerAddr {
			s.logger.Debug("Peer already connected, skipping",
				domain.Field{Key: "peer_address", Value: peerAddr})
			return true
		}
	}
	return false
}
