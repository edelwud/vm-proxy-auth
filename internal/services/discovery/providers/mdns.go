package providers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/hashicorp/mdns"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/infrastructure/logger"
)

// safeServiceEntry is a race-safe copy of mdns.ServiceEntry with string representations.
type safeServiceEntry struct {
	Name      string
	Host      string
	Port      int
	Info      string
	AddrV4Str string
	AddrV6Str string
	HasAddrV4 bool
	HasAddrV6 bool
}

// MDNSProvider implements mDNS-based discovery with both server and client.
type MDNSProvider struct {
	logger domain.Logger

	serviceName string
	domainName  string
	port        int
	hostname    string
	txtRecords  map[string]string

	// mDNS server for announcing ourselves
	server   *mdns.Server
	serverMu sync.RWMutex

	running bool
	stopCh  chan struct{}
}

// NewMDNSProvider creates a new mDNS discovery provider.
func NewMDNSProvider(
	serviceName, domainName, hostname string,
	port int,
	txtRecords map[string]string,
	logger domain.Logger,
) domain.DiscoveryProvider {
	return &MDNSProvider{
		logger:      logger.With(domain.Field{Key: "component", Value: "mdns"}),
		serviceName: serviceName,
		domainName:  domainName,
		hostname:    hostname,
		port:        port,
		txtRecords:  txtRecords,
		stopCh:      make(chan struct{}),
	}
}

// Start starts the mDNS server to announce ourselves.
func (m *MDNSProvider) Start(_ context.Context) error {
	m.serverMu.Lock()
	defer m.serverMu.Unlock()

	if m.running {
		return errors.New("mDNS provider already running")
	}

	// Use configured hostname if it's an IP address, otherwise auto-detect
	// Special case: "0.0.0.0" means auto-detect (broadcast mode)
	var localIP net.IP
	if ip := net.ParseIP(m.hostname); ip != nil && !ip.IsUnspecified() {
		localIP = ip
		m.logger.Debug("Using configured IP address",
			domain.Field{Key: "hostname", Value: m.hostname})
	} else {
		var err error
		localIP, err = m.getLocalIP()
		if err != nil {
			return fmt.Errorf("failed to get local IP: %w", err)
		}
		m.logger.Debug("Auto-detected IP address",
			domain.Field{Key: "detected_ip", Value: localIP.String()},
			domain.Field{Key: "hostname", Value: m.hostname})
	}

	// Create mDNS service record
	service, err := mdns.NewMDNSService(
		m.hostname,        // Instance name
		m.serviceName,     // Service name (e.g., "_vm-proxy-auth._tcp")
		m.domainName,      // Domain (e.g., "local.")
		"",                // Host name (empty = use hostname)
		m.port,            // Port
		[]net.IP{localIP}, // IP addresses
		m.convertTXTRecords(),
	)
	if err != nil {
		return fmt.Errorf("failed to create mDNS service: %w", err)
	}

	// Start mDNS server with HCLog adapter
	hclogAdapter := logger.NewHCLogAdapter(m.logger.With(domain.Field{Key: "mdns_component", Value: "server"}))
	server, err := mdns.NewServer(&mdns.Config{
		Zone:   service,
		Logger: hclogAdapter.StandardLogger(nil),
	})
	if err != nil {
		return fmt.Errorf("failed to start mDNS server: %w", err)
	}

	m.server = server
	m.running = true

	m.logger.Info("mDNS provider started",
		domain.Field{Key: "service_name", Value: m.serviceName},
		domain.Field{Key: "hostname", Value: m.hostname},
		domain.Field{Key: "port", Value: m.port},
		domain.Field{Key: "local_ip", Value: localIP.String()})

	return nil
}

// Stop stops the mDNS server.
func (m *MDNSProvider) Stop() error {
	m.serverMu.Lock()
	defer m.serverMu.Unlock()

	if !m.running {
		return nil
	}

	// Close stop channel only if it's not already closed
	select {
	case <-m.stopCh:
		// Already closed
	default:
		close(m.stopCh)
	}

	if m.server != nil {
		if err := m.server.Shutdown(); err != nil {
			m.logger.Warn("Error shutting down mDNS server",
				domain.Field{Key: "error", Value: err.Error()})
		}
	}

	m.running = false
	m.logger.Info("mDNS provider stopped")
	return nil
}

// Discover implements DiscoveryProvider for mDNS.
func (m *MDNSProvider) Discover(ctx context.Context) ([]string, error) {
	// Check if provider is running with read lock
	m.serverMu.RLock()
	isRunning := m.running
	m.serverMu.RUnlock()

	if !isRunning {
		return []string{}, nil
	}

	// Create entries channel with buffer
	entriesCh := make(chan *mdns.ServiceEntry, domain.DefaultDiscoveryBufferSize)

	// Create lookup context with timeout
	lookupCtx, cancel := context.WithTimeout(ctx, domain.DefaultMDNSLookupTimeout)
	defer cancel()

	// Start mDNS lookup in goroutine with IPv6 disabled and custom logger
	go func() {
		defer close(entriesCh)

		// Create HCLog adapter for mDNS client
		hclogAdapter := logger.NewHCLogAdapter(m.logger.With(domain.Field{Key: "mdns_component", Value: "client"}))

		params := &mdns.QueryParam{
			Service:     m.serviceName,
			Domain:      m.domainName,
			Timeout:     domain.DefaultMDNSLookupTimeout,
			Entries:     entriesCh,
			DisableIPv6: true, // Disable IPv6 to prevent binding errors
			Logger:      hclogAdapter.StandardLogger(nil),
		}
		if err := mdns.Query(params); err != nil {
			m.logger.Debug("mDNS lookup error",
				domain.Field{Key: "service", Value: m.serviceName},
				domain.Field{Key: "error", Value: err.Error()})
		}
	}()

	var peers []string

	// Collect entries with timeout
	m.logger.Debug("Starting mDNS lookup",
		domain.Field{Key: "service", Value: m.serviceName},
		domain.Field{Key: "domain", Value: m.domainName})

	for {
		select {
		case entry := <-entriesCh:
			if entry == nil {
				// Channel closed, done collecting
				m.logger.Debug("mDNS entries channel closed")
				goto done
			}

			// Create a complete safe copy of the service entry immediately to avoid race conditions
			entryCopy := m.copyServiceEntry(entry)

			m.logger.Debug("Received mDNS entry",
				domain.Field{Key: "name", Value: entryCopy.Name},
				domain.Field{Key: "host", Value: entryCopy.Host},
				domain.Field{Key: "port", Value: entryCopy.Port},
				domain.Field{Key: "addrV4", Value: entryCopy.AddrV4Str},
				domain.Field{Key: "addrV6", Value: entryCopy.AddrV6Str},
				domain.Field{Key: "info", Value: entryCopy.Info})

			// Skip if no IPv4 address
			if !entryCopy.HasAddrV4 {
				m.logger.Debug("Skipping entry without IPv4 address",
					domain.Field{Key: "name", Value: entryCopy.Name})
				continue
			}

			peerAddr := fmt.Sprintf("%s:%d", entryCopy.AddrV4Str, entryCopy.Port)
			peers = append(peers, peerAddr)

			m.logger.Info("Discovered mDNS peer",
				domain.Field{Key: "address", Value: peerAddr},
				domain.Field{Key: "hostname", Value: entryCopy.Host},
				domain.Field{Key: "info", Value: entryCopy.Info})

		case <-lookupCtx.Done():
			// Timeout or cancellation
			m.logger.Debug("mDNS lookup timeout or cancellation")
			goto done
		}
	}

done:
	if len(peers) > 0 {
		m.logger.Info("Discovered peers via mDNS",
			domain.Field{Key: "service", Value: m.serviceName},
			domain.Field{Key: "peers_count", Value: len(peers)})
	}

	return peers, nil
}

// GetProviderType returns the provider type name.
func (m *MDNSProvider) GetProviderType() string {
	return "mdns"
}

// convertTXTRecords converts the map of TXT records to the string slice format required by mdns.
func (m *MDNSProvider) convertTXTRecords() []string {
	if len(m.txtRecords) == 0 {
		// Default TXT records if none configured
		return []string{
			"version=1.0",
			"role=gateway",
		}
	}

	txtSlice := make([]string, 0, len(m.txtRecords))
	for key, value := range m.txtRecords {
		txtSlice = append(txtSlice, fmt.Sprintf("%s=%s", key, value))
	}
	return txtSlice
}

// getLocalIP returns the local non-loopback IP address.
func (m *MDNSProvider) getLocalIP() (net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP, nil
			}
		}
	}

	return nil, errors.New("no non-loopback IP address found")
}

// copyServiceEntry creates a race-safe copy of an mdns.ServiceEntry.
// This function uses defensive programming to handle potential race conditions
// in the external hashicorp/mdns library.
func (m *MDNSProvider) copyServiceEntry(entry *mdns.ServiceEntry) *safeServiceEntry {
	if entry == nil {
		return &safeServiceEntry{}
	}

	// Create default safe entry
	safe := &safeServiceEntry{}

	// Use defer + recover to handle potential race conditions during field access
	defer func() {
		if r := recover(); r != nil {
			m.logger.Debug("Recovered from panic during service entry copy",
				domain.Field{Key: "error", Value: fmt.Sprintf("%v", r)})
		}
	}()

	// Safely copy string fields with nil checks
	if entry.Name != "" {
		safe.Name = entry.Name
	}
	if entry.Host != "" {
		safe.Host = entry.Host
	}
	if entry.Info != "" {
		safe.Info = entry.Info
	}
	safe.Port = entry.Port

	// Safely copy IPv4 address with extensive checks
	if len(entry.AddrV4) > 0 {
		safe.HasAddrV4 = true
		// Create copy in separate operation
		addrV4Copy := make(net.IP, len(entry.AddrV4))
		n := copy(addrV4Copy, entry.AddrV4)
		if n > 0 {
			safe.AddrV4Str = addrV4Copy.String()
		}
	}

	// Safely copy IPv6 address with extensive checks
	if len(entry.AddrV6) > 0 {
		safe.HasAddrV6 = true
		// Create copy in separate operation
		addrV6Copy := make(net.IP, len(entry.AddrV6))
		n := copy(addrV6Copy, entry.AddrV6)
		if n > 0 {
			safe.AddrV6Str = addrV6Copy.String()
		}
	}

	return safe
}
