package cluster

import (
	"errors"
	"fmt"
	"net"
	"time"
)

// Config represents cluster configuration.
type Config struct {
	Memberlist MemberlistConfig `mapstructure:"memberlist"`
	Discovery  DiscoveryConfig  `mapstructure:"discovery"`
}

// MemberlistConfig represents memberlist configuration.
type MemberlistConfig struct {
	BindAddress      string            `mapstructure:"bindAddress"`
	AdvertiseAddress string            `mapstructure:"advertiseAddress"`
	Peers            PeersConfig       `mapstructure:"peers"`
	Gossip           GossipConfig      `mapstructure:"gossip"`
	Probe            ProbeConfig       `mapstructure:"probe"`
	Metadata         map[string]string `mapstructure:"metadata,omitempty"`
}

// PeersConfig represents peer configuration.
type PeersConfig struct {
	Join       []string `mapstructure:"join"`
	Encryption string   `mapstructure:"encryption"`
}

// GossipConfig represents gossip configuration.
type GossipConfig struct {
	Interval time.Duration `mapstructure:"interval"`
	Nodes    int           `mapstructure:"nodes"`
}

// ProbeConfig represents probe configuration.
type ProbeConfig struct {
	Interval time.Duration `mapstructure:"interval"`
	Timeout  time.Duration `mapstructure:"timeout"`
}

// DiscoveryConfig represents discovery configuration.
type DiscoveryConfig struct {
	Enabled   bool          `mapstructure:"enabled"`
	Interval  time.Duration `mapstructure:"interval"`
	Providers []string      `mapstructure:"providers"`
	Static    *StaticConfig `mapstructure:"static,omitempty"` // Optional
	MDNS      MDNSConfig    `mapstructure:"mdns"`
}

// StaticConfig represents static peer discovery configuration.
type StaticConfig struct {
	Peers []string `mapstructure:"peers"`
}

// MDNSConfig represents mDNS discovery configuration.
type MDNSConfig struct {
	Enabled     bool              `mapstructure:"enabled"`
	ServiceName string            `mapstructure:"serviceName"`
	Domain      string            `mapstructure:"domain"`
	Hostname    string            `mapstructure:"hostname"`
	Port        int               `mapstructure:"port"`
	TXTRecords  map[string]string `mapstructure:"txtRecords,omitempty"`
}

// Validate validates cluster configuration.
func (c *Config) Validate() error {
	// Validate memberlist
	if err := c.Memberlist.Validate(); err != nil {
		return fmt.Errorf("memberlist validation failed: %w", err)
	}

	// Validate discovery
	if err := c.Discovery.Validate(); err != nil {
		return fmt.Errorf("discovery validation failed: %w", err)
	}

	return nil
}

// Validate validates memberlist configuration.
func (m *MemberlistConfig) Validate() error {
	if m.BindAddress == "" {
		return errors.New("memberlist bind address is required")
	}

	// Validate bind address format (should be host:port)
	if _, _, err := net.SplitHostPort(m.BindAddress); err != nil {
		return fmt.Errorf("invalid memberlist bind address format: %w", err)
	}

	// Validate advertise address if provided
	if m.AdvertiseAddress != "" {
		if _, _, err := net.SplitHostPort(m.AdvertiseAddress); err != nil {
			return fmt.Errorf("invalid memberlist advertise address format: %w", err)
		}
	}

	// Validate gossip configuration
	if err := m.Gossip.Validate(); err != nil {
		return fmt.Errorf("gossip validation failed: %w", err)
	}

	// Validate probe configuration
	if err := m.Probe.Validate(); err != nil {
		return fmt.Errorf("probe validation failed: %w", err)
	}

	return nil
}

// Validate validates gossip configuration.
func (g *GossipConfig) Validate() error {
	if g.Interval <= 0 {
		return fmt.Errorf("gossip interval must be positive, got %v", g.Interval)
	}

	if g.Nodes <= 0 {
		return fmt.Errorf("gossip nodes must be positive, got %d", g.Nodes)
	}

	return nil
}

// Validate validates probe configuration.
func (p *ProbeConfig) Validate() error {
	if p.Interval <= 0 {
		return fmt.Errorf("probe interval must be positive, got %v", p.Interval)
	}

	if p.Timeout <= 0 {
		return fmt.Errorf("probe timeout must be positive, got %v", p.Timeout)
	}

	if p.Timeout >= p.Interval {
		return fmt.Errorf("probe timeout (%v) must be less than interval (%v)", p.Timeout, p.Interval)
	}

	return nil
}

// Validate validates discovery configuration.
func (d *DiscoveryConfig) Validate() error {
	if d.Interval <= 0 {
		return fmt.Errorf("discovery interval must be positive, got %v", d.Interval)
	}

	// Validate static configuration if present
	if d.Static != nil {
		if err := d.Static.Validate(); err != nil {
			return fmt.Errorf("static discovery validation failed: %w", err)
		}
	}

	// Validate mDNS configuration
	if err := d.MDNS.Validate(); err != nil {
		return fmt.Errorf("mDNS discovery validation failed: %w", err)
	}

	return nil
}

// Validate validates static discovery configuration.
func (s *StaticConfig) Validate() error {
	// Validate each peer address
	for i, peer := range s.Peers {
		if peer == "" {
			return fmt.Errorf("static peer %d cannot be empty", i)
		}

		// Validate peer address format
		if _, _, err := net.SplitHostPort(peer); err != nil {
			return fmt.Errorf("invalid static peer %d address format (%s): %w", i, peer, err)
		}
	}

	return nil
}

// Validate validates mDNS discovery configuration.
func (m *MDNSConfig) Validate() error {
	if m.Enabled {
		if m.ServiceName == "" {
			return errors.New("mDNS service name is required when mDNS is enabled")
		}

		if m.Domain == "" {
			return errors.New("mDNS domain is required when mDNS is enabled")
		}

		if m.Port <= 0 {
			return fmt.Errorf("mDNS port must be positive when mDNS is enabled, got %d", m.Port)
		}
	}

	return nil
}
