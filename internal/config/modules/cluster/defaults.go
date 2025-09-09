package cluster

import (
	"time"
)

// Default cluster configuration values.
const (
	DefaultMemberlistBindAddress    = "0.0.0.0:7946"
	DefaultGossipInterval           = 200 * time.Millisecond
	DefaultGossipNodes              = 3
	DefaultProbeInterval            = 1 * time.Second
	DefaultProbeTimeout             = 500 * time.Millisecond
	DefaultDiscoveryInterval        = 30 * time.Second
	DefaultMDNSServiceName          = "_vm-proxy-auth._tcp"
	DefaultMDNSDomain               = "local."
	DefaultMDNSPort                 = 7946
	DefaultMDNSEnabled              = true
	DefaultMemberlistBindPort       = 7946
	DefaultMemberlistGossipNodes    = 3
	DefaultMemberlistProbeTimeout   = 500 * time.Millisecond
	DefaultMemberlistProbeInterval  = 1 * time.Second
	DefaultMemberlistGossipInterval = 200 * time.Millisecond
	DefaultLeaveTimeout             = 5 * time.Second
	DefaultMemberlistProcessDelay   = 2 * time.Second
	DefaultLeadershipCheckInterval  = 5 * time.Second
	DefaultMDNSServiceType          = "_vm-proxy-auth._tcp"
	DefaultMDNSServiceDomain        = "local."
	DefaultMDNSUpdateInterval       = 15 * time.Second
	DefaultMDNSEventChannelSize     = 100
	DefaultMDNSQueryTimeout         = 5 * time.Second
	DefaultMDNSEntryChannelSize     = 32
	DefaultMDNSHostname             = "vm-proxy-auth"
	DefaultMDNSLookupTimeout        = 5 * time.Second
	DefaultDiscoveryBufferSize      = 10
	DefaultDNSUpdateInterval        = 30 * time.Second
	DefaultDNSPort                  = 8080
	DefaultDNSRaftPort              = 9000
	DefaultDNSTTL                   = 2 * time.Minute
	DefaultDNSEventChannelSize      = 100
	DefaultDNSSRVService            = "vm-proxy-auth"
	DefaultDNSSRVProtocol           = "tcp"
	DefaultDNSBackendService        = "vm-backend"
	DefaultK8sWatchTimeout          = 10 * time.Minute
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
