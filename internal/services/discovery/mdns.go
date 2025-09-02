package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/mdns"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/infrastructure/logger"
)

// MDNSDiscovery implements service discovery using mDNS (Multicast DNS).
// This is ideal for local development and small-scale deployments where
// services can auto-discover each other on the local network.
type MDNSDiscovery struct {
	config       config.MDNSDiscoveryConfig
	logger       domain.Logger
	eventCh      chan domain.ServiceDiscoveryEvent
	stopCh       chan struct{}
	running      bool
	runningMutex sync.RWMutex
}

// NewMDNSDiscovery creates a new mDNS-based service discovery instance.
func NewMDNSDiscovery(
	config config.MDNSDiscoveryConfig,
	logger domain.Logger,
) domain.ServiceDiscovery {
	return &MDNSDiscovery{
		config:  config,
		logger:  logger.With(domain.Field{Key: "component", Value: "mdns_discovery"}),
		eventCh: make(chan domain.ServiceDiscoveryEvent, domain.DefaultMDNSEventChannelSize),
		stopCh:  make(chan struct{}),
	}
}

// Start begins the mDNS discovery process.
func (m *MDNSDiscovery) Start(ctx context.Context) error {
	m.runningMutex.Lock()
	defer m.runningMutex.Unlock()

	if m.running {
		return errors.New("mDNS discovery already running")
	}

	m.running = true
	m.logger.Info("Starting mDNS service discovery",
		domain.Field{Key: "service_type", Value: m.config.ServiceType},
		domain.Field{Key: "service_domain", Value: m.config.ServiceDomain},
		domain.Field{Key: "update_interval", Value: m.config.UpdateInterval})

	go m.discoveryLoop(ctx)
	return nil
}

// Stop stops the mDNS discovery process.
func (m *MDNSDiscovery) Stop() error {
	m.runningMutex.Lock()
	defer m.runningMutex.Unlock()

	if !m.running {
		return nil
	}

	close(m.stopCh)
	m.running = false

	// Drain event channel
	for len(m.eventCh) > 0 {
		<-m.eventCh
	}

	m.logger.Info("mDNS service discovery stopped")
	return nil
}

// DiscoverPeers discovers Raft peers using mDNS.
func (m *MDNSDiscovery) DiscoverPeers(_ context.Context) ([]*domain.PeerInfo, error) {
	entries := make(chan *mdns.ServiceEntry, domain.DefaultMDNSEntryChannelSize)
	defer close(entries)

	var peers []*domain.PeerInfo
	var wg sync.WaitGroup

	// Collect entries in background
	wg.Add(1)
	go func() {
		defer wg.Done()
		for entry := range entries {
			peer := m.convertServiceEntryToPeer(entry)
			if peer != nil {
				peers = append(peers, peer)
			}
		}
	}()

	// Query for services with structured logging
	hclogAdapter := logger.NewHCLogAdapter(m.logger)
	stdLogger := hclogAdapter.StandardLogger(nil)

	queryParams := &mdns.QueryParam{
		Service:             m.config.ServiceType,
		Domain:              m.config.ServiceDomain,
		Timeout:             domain.DefaultMDNSQueryTimeout,
		Entries:             entries,
		WantUnicastResponse: false,
		Logger:              stdLogger,
		DisableIPv6:         m.config.DisableIPv6, // Configurable IPv6 disable
	}

	if err := mdns.Query(queryParams); err != nil {
		return nil, fmt.Errorf("mDNS peer discovery failed: %w", err)
	}

	wg.Wait()

	m.logger.Info("mDNS peer discovery completed",
		domain.Field{Key: "peers_found", Value: len(peers)},
		domain.Field{Key: "service_type", Value: m.config.ServiceType})

	return peers, nil
}

// DiscoverBackends discovers backend services using mDNS.
func (m *MDNSDiscovery) DiscoverBackends(_ context.Context) ([]*domain.BackendInfo, error) {
	// For now, use the same service type for backends
	// In production, you might want separate service types
	entries := make(chan *mdns.ServiceEntry, domain.DefaultMDNSEntryChannelSize)
	defer close(entries)

	var backends []*domain.BackendInfo
	var wg sync.WaitGroup

	// Collect entries in background
	wg.Add(1)
	go func() {
		defer wg.Done()
		for entry := range entries {
			backend := m.convertServiceEntryToBackend(entry)
			if backend != nil {
				backends = append(backends, backend)
			}
		}
	}()

	// Query for backend services (could be different service type)
	backendServiceType := strings.Replace(m.config.ServiceType, "vm-proxy-auth", "vm-backend", 1)
	hclogAdapter := logger.NewHCLogAdapter(m.logger)
	stdLogger := hclogAdapter.StandardLogger(nil)

	queryParams := &mdns.QueryParam{
		Service:             backendServiceType,
		Domain:              m.config.ServiceDomain,
		Timeout:             domain.DefaultMDNSQueryTimeout,
		Entries:             entries,
		WantUnicastResponse: false,
		Logger:              stdLogger,
		DisableIPv6:         m.config.DisableIPv6, // Configurable IPv6 disable
	}

	if err := mdns.Query(queryParams); err != nil {
		return nil, fmt.Errorf("mDNS backend discovery failed: %w", err)
	}

	wg.Wait()

	m.logger.Info("mDNS backend discovery completed",
		domain.Field{Key: "backends_found", Value: len(backends)},
		domain.Field{Key: "service_type", Value: backendServiceType})

	return backends, nil
}

// Events returns the event channel for discovery updates.
func (m *MDNSDiscovery) Events() <-chan domain.ServiceDiscoveryEvent {
	return m.eventCh
}

// RegisterSelf registers this node in mDNS.
func (m *MDNSDiscovery) RegisterSelf(_ context.Context, info *domain.PeerInfo) error {
	// Create mDNS service entry
	service, err := mdns.NewMDNSService(
		info.NodeID,
		m.config.ServiceType,
		m.config.ServiceDomain,
		"", // hostname will be auto-detected
		m.config.Port,
		[]net.IP{}, // IPs will be auto-detected
		[]string{
			fmt.Sprintf("raft_port=%d", m.config.RaftPort),
			fmt.Sprintf("node_id=%s", info.NodeID),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create mDNS service: %w", err)
	}

	// Start mDNS server for this service with structured logging
	hclogAdapter := logger.NewHCLogAdapter(m.logger)
	stdLogger := hclogAdapter.StandardLogger(nil)

	server, err := mdns.NewServer(&mdns.Config{
		Zone:   service,
		Logger: stdLogger,
	})
	if err != nil {
		return fmt.Errorf("failed to start mDNS server: %w", err)
	}

	m.logger.Info("mDNS self-registration completed",
		domain.Field{Key: "node_id", Value: info.NodeID},
		domain.Field{Key: "service_type", Value: m.config.ServiceType},
		domain.Field{Key: "port", Value: m.config.Port})

	// In a real implementation, you'd want to keep the server running
	// and shut it down when the discovery service stops
	defer func() {
		if shutdownErr := server.Shutdown(); shutdownErr != nil {
			m.logger.Error("Failed to shutdown mDNS server", domain.Field{Key: "error", Value: shutdownErr})
		}
	}()

	return nil
}

// discoveryLoop runs periodic mDNS discovery.
func (m *MDNSDiscovery) discoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(m.config.UpdateInterval)
	defer ticker.Stop()

	// Initial discovery
	m.performDiscovery(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.performDiscovery(ctx)
		}
	}
}

// performDiscovery performs a single mDNS discovery cycle.
func (m *MDNSDiscovery) performDiscovery(ctx context.Context) {
	// Discover peers
	peers, err := m.DiscoverPeers(ctx)
	if err != nil {
		m.logger.Debug("mDNS peer discovery failed (expected in some environments)",
			domain.Field{Key: "error", Value: err.Error()},
			domain.Field{Key: "service_type", Value: m.config.ServiceType})
	} else {
		m.processPeerChanges(peers)
	}

	// Discover backends
	backends, err := m.DiscoverBackends(ctx)
	if err != nil {
		m.logger.Debug("mDNS backend discovery failed (expected in some environments)",
			domain.Field{Key: "error", Value: err.Error()})
	} else {
		m.processBackendChanges(backends)
	}
}

// convertServiceEntryToPeer converts mDNS service entry to PeerInfo.
func (m *MDNSDiscovery) convertServiceEntryToPeer(entry *mdns.ServiceEntry) *domain.PeerInfo {
	if entry == nil {
		return nil
	}

	// Extract Raft port from TXT records
	raftPort := m.config.RaftPort
	nodeID := entry.Name

	for _, txt := range entry.InfoFields {
		if strings.HasPrefix(txt, "raft_port=") {
			if port, err := strconv.Atoi(strings.TrimPrefix(txt, "raft_port=")); err == nil {
				raftPort = port
			}
		}
		if strings.HasPrefix(txt, "node_id=") {
			nodeID = strings.TrimPrefix(txt, "node_id=")
		}
	}

	// Use first available IP
	ip := selectIPAddress(entry)
	if ip == nil {
		return nil
	}

	httpAddress := fmt.Sprintf("%s:%d", ip.String(), entry.Port)
	raftAddress := fmt.Sprintf("%s:%d", ip.String(), raftPort)

	return &domain.PeerInfo{
		NodeID:      nodeID,
		HTTPAddress: httpAddress,
		RaftAddress: raftAddress,
		Healthy:     true,
		LastSeen:    time.Now(),
		Metadata: map[string]string{
			"source":      "mdns",
			"host":        entry.Host,
			"service":     entry.Name,
			"txt_records": strings.Join(entry.InfoFields, ","),
		},
	}
}

// convertServiceEntryToBackend converts mDNS service entry to BackendInfo.
func (m *MDNSDiscovery) convertServiceEntryToBackend(entry *mdns.ServiceEntry) *domain.BackendInfo {
	if entry == nil {
		return nil
	}

	// Use first available IP
	ip := selectIPAddress(entry)
	if ip == nil {
		return nil
	}

	url := fmt.Sprintf("http://%s", net.JoinHostPort(ip.String(), strconv.Itoa(entry.Port)))

	return &domain.BackendInfo{
		URL:      url,
		Weight:   domain.DefaultBackendWeight,
		Healthy:  true,
		LastSeen: time.Now(),
		Metadata: map[string]string{
			"source":  "mdns",
			"host":    entry.Host,
			"service": entry.Name,
		},
	}
}

// processPeerChanges processes discovered peer changes and sends events.
func (m *MDNSDiscovery) processPeerChanges(peers []*domain.PeerInfo) {
	for _, peer := range peers {
		event := domain.ServiceDiscoveryEvent{
			Type:      domain.ServiceDiscoveryEventTypePeerJoined,
			Peer:      peer,
			Timestamp: time.Now(),
		}

		select {
		case m.eventCh <- event:
		default:
			m.logger.Warn("mDNS discovery event channel full, dropping peer event",
				domain.Field{Key: "peer_id", Value: peer.NodeID})
		}
	}
}

// processBackendChanges processes discovered backend changes and sends events.
func (m *MDNSDiscovery) processBackendChanges(backends []*domain.BackendInfo) {
	for _, backend := range backends {
		event := domain.ServiceDiscoveryEvent{
			Type:      domain.ServiceDiscoveryEventTypeBackendAdded,
			Backend:   backend,
			Timestamp: time.Now(),
		}

		select {
		case m.eventCh <- event:
		default:
			m.logger.Warn("mDNS discovery event channel full, dropping backend event",
				domain.Field{Key: "backend_url", Value: backend.URL})
		}
	}
}

// selectIPAddress selects the first available IP address from a service entry.
// Prefers IPv4 over IPv6 for compatibility.
func selectIPAddress(entry *mdns.ServiceEntry) net.IP {
	switch {
	case entry.AddrV4 != nil:
		return entry.AddrV4
	case entry.AddrV6 != nil:
		return entry.AddrV6
	default:
		return nil
	}
}
