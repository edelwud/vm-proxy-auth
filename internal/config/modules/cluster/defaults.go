package cluster

import (
	"time"
)

// Default cluster configuration values.
const (
	DefaultMemberlistBindAddress = "0.0.0.0:7946"
	DefaultGossipInterval        = 200 * time.Millisecond
	DefaultGossipNodes           = 3
	DefaultProbeInterval         = 1 * time.Second
	DefaultProbeTimeout          = 500 * time.Millisecond
	DefaultDiscoveryInterval     = 30 * time.Second
	DefaultMDNSServiceName       = "_vm-proxy-auth._tcp"
	DefaultMDNSDomain            = "local."
	DefaultMDNSPort              = 7946
	DefaultMDNSEnabled           = true
)

// GetDefaults returns default cluster configuration.
func GetDefaults() Config {
	return Config{
		Memberlist: MemberlistConfig{
			BindAddress: DefaultMemberlistBindAddress,
			Peers: PeersConfig{
				Join:       []string{},
				Encryption: "",
			},
			Gossip: GossipConfig{
				Interval: DefaultGossipInterval,
				Nodes:    DefaultGossipNodes,
			},
			Probe: ProbeConfig{
				Interval: DefaultProbeInterval,
				Timeout:  DefaultProbeTimeout,
			},
			Metadata: map[string]string{},
		},
		Discovery: DiscoveryConfig{
			Interval: DefaultDiscoveryInterval,
			Static:   nil, // Optional - defaults to nil
			MDNS: MDNSConfig{
				Enabled:     DefaultMDNSEnabled,
				ServiceName: DefaultMDNSServiceName,
				Domain:      DefaultMDNSDomain,
				Hostname:    "", // Will use nodeId from metadata
				Port:        DefaultMDNSPort,
				TXTRecords:  map[string]string{},
			},
		},
	}
}
