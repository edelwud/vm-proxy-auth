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

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

const (
	dnsDefaultUpdateInterval   = 30 * time.Second
	dnsDefaultPort             = 8080
	dnsDefaultRaftPort         = 9000
	dnsDefaultTTL              = 2 * time.Minute
	dnsDefaultEventChannelSize = 100
)

// DNSDiscovery implements service discovery using DNS lookups.
type DNSDiscovery struct {
	config   config.DNSDiscoveryConfig
	logger   domain.Logger
	watchCh  chan domain.ServiceDiscoveryEvent
	stopCh   chan struct{}
	mu       sync.RWMutex
	running  bool
	lastSeen map[string]time.Time
}

// NewDNSDiscovery creates a new DNS-based service discovery instance.
func NewDNSDiscovery(config config.DNSDiscoveryConfig, logger domain.Logger) *DNSDiscovery {
	// Set defaults
	if config.UpdateInterval == 0 {
		config.UpdateInterval = dnsDefaultUpdateInterval
	}
	if config.Port == 0 {
		config.Port = dnsDefaultPort
	}
	if config.RaftPort == 0 {
		config.RaftPort = dnsDefaultRaftPort
	}
	if config.SRVService == "" {
		config.SRVService = "vm-proxy-auth"
	}
	if config.SRVProtocol == "" {
		config.SRVProtocol = "tcp"
	}

	return &DNSDiscovery{
		config:   config,
		logger:   logger,
		watchCh:  make(chan domain.ServiceDiscoveryEvent, dnsDefaultEventChannelSize),
		stopCh:   make(chan struct{}),
		lastSeen: make(map[string]time.Time),
	}
}

// Start begins the DNS discovery process.
func (d *DNSDiscovery) Start(ctx context.Context) error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return errors.New("DNS discovery already running")
	}
	d.running = true
	d.mu.Unlock()

	d.logger.Info("Starting DNS service discovery",
		domain.Field{Key: "domain", Value: d.config.Domain},
		domain.Field{Key: "use_srv", Value: d.config.UseSRVRecords},
		domain.Field{Key: "update_interval", Value: d.config.UpdateInterval})

	go d.discoveryLoop(ctx)
	return nil
}

// Stop stops the DNS discovery process.
func (d *DNSDiscovery) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return nil
	}

	close(d.stopCh)
	d.running = false
	close(d.watchCh)

	d.logger.Info("DNS service discovery stopped")
	return nil
}

// DiscoverPeers discovers Raft peer nodes using DNS.
func (d *DNSDiscovery) DiscoverPeers(ctx context.Context) ([]*domain.PeerInfo, error) {
	d.logger.Debug("Discovering peers via DNS",
		domain.Field{Key: "domain", Value: d.config.Domain})

	var peers []*domain.PeerInfo
	var err error

	if d.config.UseSRVRecords {
		peers, err = d.discoverPeersViaSRV(ctx)
	} else {
		peers, err = d.discoverPeersViaA(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("DNS peer discovery failed: %w", err)
	}

	d.logger.Info("DNS peer discovery completed",
		domain.Field{Key: "peers_found", Value: len(peers)})

	return peers, nil
}

// DiscoverBackends discovers backend services using DNS.
func (d *DNSDiscovery) DiscoverBackends(ctx context.Context) ([]*domain.BackendInfo, error) {
	d.logger.Debug("Discovering backends via DNS",
		domain.Field{Key: "domain", Value: d.config.Domain})

	var backends []*domain.BackendInfo
	var err error

	if d.config.UseSRVRecords {
		backends, err = d.discoverBackendsViaSRV(ctx)
	} else {
		backends, err = d.discoverBackendsViaA(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("DNS backend discovery failed: %w", err)
	}

	d.logger.Info("DNS backend discovery completed",
		domain.Field{Key: "backends_found", Value: len(backends)})

	return backends, nil
}

// Events returns the discovery event channel.
func (d *DNSDiscovery) Events() <-chan domain.ServiceDiscoveryEvent {
	return d.watchCh
}

// RegisterSelf registers this node in DNS (no-op for DNS discovery).
func (d *DNSDiscovery) RegisterSelf(_ context.Context, nodeInfo domain.NodeInfo) error {
	d.logger.Info("DNS discovery does not support self-registration",
		domain.Field{Key: "node_id", Value: nodeInfo.NodeID})
	return nil
}

// discoveryLoop runs periodic DNS discovery.
func (d *DNSDiscovery) discoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(d.config.UpdateInterval)
	defer ticker.Stop()

	// Initial discovery
	d.performDiscovery(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.performDiscovery(ctx)
		}
	}
}

// performDiscovery performs a single discovery cycle.
func (d *DNSDiscovery) performDiscovery(ctx context.Context) {
	// Discover peers
	peers, err := d.DiscoverPeers(ctx)
	if err != nil {
		d.logger.Error("Failed to discover peers",
			domain.Field{Key: "error", Value: err.Error()})
	} else {
		d.processPeerChanges(peers)
	}

	// Discover backends
	backends, err := d.DiscoverBackends(ctx)
	if err != nil {
		d.logger.Error("Failed to discover backends",
			domain.Field{Key: "error", Value: err.Error()})
	} else {
		d.processBackendChanges(backends)
	}
}

// discoverPeersViaSRV discovers peers using SRV records.
func (d *DNSDiscovery) discoverPeersViaSRV(ctx context.Context) ([]*domain.PeerInfo, error) {
	srvName := fmt.Sprintf("_%s._%s.%s", d.config.SRVService, d.config.SRVProtocol, d.config.Domain)

	resolver := &net.Resolver{}
	_, srvs, err := resolver.LookupSRV(ctx, d.config.SRVService, d.config.SRVProtocol, d.config.Domain)
	if err != nil {
		return nil, fmt.Errorf("SRV lookup failed for %s: %w", srvName, err)
	}

	var peers []*domain.PeerInfo
	for _, srv := range srvs {
		target := strings.TrimSuffix(srv.Target, ".")
		nodeID := fmt.Sprintf("dns-%s", target)
		httpAddress := fmt.Sprintf("%s:%d", target, srv.Port)
		raftAddress := fmt.Sprintf("%s:%d", target, d.config.RaftPort)

		peer := &domain.PeerInfo{
			NodeID:      nodeID,
			HTTPAddress: httpAddress,
			RaftAddress: raftAddress,
			Healthy:     true,
			LastSeen:    time.Now(),
			Metadata: map[string]string{
				"source":       "dns",
				"record_type":  "srv",
				"srv_priority": strconv.Itoa(int(srv.Priority)),
				"srv_weight":   strconv.Itoa(int(srv.Weight)),
				"srv_port":     strconv.Itoa(int(srv.Port)),
			},
		}
		peers = append(peers, peer)
	}

	return peers, nil
}

// discoverPeersViaA discovers peers using A/AAAA records.
func (d *DNSDiscovery) discoverPeersViaA(ctx context.Context) ([]*domain.PeerInfo, error) {
	resolver := &net.Resolver{}
	ips, err := resolver.LookupHost(ctx, d.config.Domain)
	if err != nil {
		return nil, fmt.Errorf("A/AAAA lookup failed for %s: %w", d.config.Domain, err)
	}

	var peers []*domain.PeerInfo
	for _, ip := range ips {
		nodeID := fmt.Sprintf("dns-%s", ip)
		httpAddress := fmt.Sprintf("%s:%d", ip, d.config.Port)
		raftAddress := fmt.Sprintf("%s:%d", ip, d.config.RaftPort)

		peer := &domain.PeerInfo{
			NodeID:      nodeID,
			HTTPAddress: httpAddress,
			RaftAddress: raftAddress,
			Healthy:     true,
			LastSeen:    time.Now(),
			Metadata: map[string]string{
				"source": "dns",
				"domain": d.config.Domain,
			},
		}
		peers = append(peers, peer)
	}

	return peers, nil
}

// discoverBackendsViaSRV discovers backends using SRV records.
func (d *DNSDiscovery) discoverBackendsViaSRV(ctx context.Context) ([]*domain.BackendInfo, error) {
	backendService := "vm-backend"
	srvName := fmt.Sprintf("_%s._%s.%s", backendService, d.config.SRVProtocol, d.config.Domain)

	resolver := &net.Resolver{}
	_, srvs, err := resolver.LookupSRV(ctx, backendService, d.config.SRVProtocol, d.config.Domain)
	if err != nil {
		return nil, fmt.Errorf("SRV lookup failed for backends %s: %w", srvName, err)
	}

	var backends []*domain.BackendInfo
	for _, srv := range srvs {
		url := "http://" + net.JoinHostPort(srv.Target, strconv.Itoa(int(srv.Port)))

		backend := &domain.BackendInfo{
			URL:      url,
			Weight:   int(srv.Weight),
			Healthy:  true,
			LastSeen: time.Now(),
			Metadata: map[string]string{
				"discovery_type": "dns_srv",
				"srv_priority":   strconv.Itoa(int(srv.Priority)),
				"srv_weight":     strconv.Itoa(int(srv.Weight)),
				"srv_port":       strconv.Itoa(int(srv.Port)),
				"target":         srv.Target,
			},
		}
		backends = append(backends, backend)
	}

	return backends, nil
}

// discoverBackendsViaA discovers backends using A/AAAA records.
func (d *DNSDiscovery) discoverBackendsViaA(ctx context.Context) ([]*domain.BackendInfo, error) {
	resolver := &net.Resolver{}
	ips, err := resolver.LookupHost(ctx, d.config.Domain)
	if err != nil {
		return nil, fmt.Errorf("A/AAAA lookup failed for backends %s: %w", d.config.Domain, err)
	}

	var backends []*domain.BackendInfo
	for _, ip := range ips {
		url := "http://" + net.JoinHostPort(ip, strconv.Itoa(d.config.Port))

		backend := &domain.BackendInfo{
			URL:      url,
			Weight:   1, // Equal weight for A record discovery
			Healthy:  true,
			LastSeen: time.Now(),
			Metadata: map[string]string{
				"source": "dns",
				"domain": d.config.Domain,
			},
		}
		backends = append(backends, backend)
	}

	return backends, nil
}

// processPeerChanges processes peer changes and sends events.
func (d *DNSDiscovery) processPeerChanges(peers []*domain.PeerInfo) {
	d.mu.Lock()
	defer d.mu.Unlock()

	currentPeers := make(map[string]*domain.PeerInfo)
	for _, peer := range peers {
		currentPeers[peer.NodeID] = peer
		d.lastSeen[peer.NodeID] = time.Now()
	}

	// Check for new peers
	for nodeID, peer := range currentPeers {
		if _, exists := d.lastSeen[nodeID]; !exists {
			d.sendEvent(domain.ServiceDiscoveryEventTypePeerJoined, peer, nil)
		}
	}

	// Check for removed peers (not seen for TTL period)
	now := time.Now()
	for nodeID, lastSeen := range d.lastSeen {
		if _, exists := currentPeers[nodeID]; !exists && now.Sub(lastSeen) > dnsDefaultTTL {
			d.sendEvent(
				domain.ServiceDiscoveryEventTypePeerLeft,
				&domain.PeerInfo{NodeID: nodeID},
				nil,
			)
			delete(d.lastSeen, nodeID)
		}
	}
}

// processBackendChanges processes backend changes and sends events.
func (d *DNSDiscovery) processBackendChanges(backends []*domain.BackendInfo) {
	for _, backend := range backends {
		d.sendEvent(domain.ServiceDiscoveryEventTypeBackendAdded, nil, backend)
	}
}

// sendEvent sends a discovery event through the watch channel.
func (d *DNSDiscovery) sendEvent(
	eventType domain.ServiceDiscoveryEventType,
	peer *domain.PeerInfo,
	backend *domain.BackendInfo,
) {
	event := domain.ServiceDiscoveryEvent{
		Type:      eventType,
		Peer:      peer,
		Backend:   backend,
		Timestamp: time.Now(),
	}

	select {
	case d.watchCh <- event:
	default:
		d.logger.Warn("DNS discovery event channel full, dropping event",
			domain.Field{Key: "event_type", Value: string(eventType)})
	}
}
