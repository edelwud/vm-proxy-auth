package cluster_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config/modules/cluster"
)

func TestClusterConfig_Validate_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config cluster.Config
	}{
		{
			name: "valid minimal config",
			config: cluster.Config{
				Memberlist: cluster.MemberlistConfig{
					BindAddress: "0.0.0.0:7946",
					Peers: cluster.PeersConfig{
						Join:       []string{},
						Encryption: "",
					},
					Gossip: cluster.GossipConfig{
						Interval: 200 * time.Millisecond,
						Nodes:    3,
					},
					Probe: cluster.ProbeConfig{
						Interval: 1 * time.Second,
						Timeout:  500 * time.Millisecond,
					},
					Metadata: map[string]string{},
				},
				Discovery: cluster.DiscoveryConfig{
					Enabled:   true,
					Interval:  30 * time.Second,
					Providers: []string{"mdns"},
					Static:    nil, // Optional
					MDNS: cluster.MDNSConfig{
						Enabled:     true,
						ServiceName: "vm-proxy-auth",
						Domain:      "local",
						Hostname:    "",
						Port:        7946,
						TXTRecords:  map[string]string{},
					},
				},
			},
		},
		{
			name: "valid with static discovery",
			config: cluster.Config{
				Memberlist: cluster.MemberlistConfig{
					BindAddress:      "10.0.1.5:7946",
					AdvertiseAddress: "10.0.1.5:7946",
					Peers: cluster.PeersConfig{
						Join:       []string{"10.0.1.6:7946", "10.0.1.7:7946"},
						Encryption: "base64-encoded-key",
					},
					Gossip: cluster.GossipConfig{
						Interval: 100 * time.Millisecond,
						Nodes:    5,
					},
					Probe: cluster.ProbeConfig{
						Interval: 2 * time.Second,
						Timeout:  1 * time.Second,
					},
					Metadata: map[string]string{
						"node_name": "vm-proxy-1",
						"region":    "us-east-1",
					},
				},
				Discovery: cluster.DiscoveryConfig{
					Enabled:   true,
					Interval:  15 * time.Second,
					Providers: []string{"static", "mdns"},
					Static: &cluster.StaticConfig{
						Peers: []string{"vm-proxy-seed-1:7946", "vm-proxy-seed-2:7946"},
					},
					MDNS: cluster.MDNSConfig{
						Enabled:     false,
						ServiceName: "vm-proxy-auth",
						Domain:      "local",
						Hostname:    "vm-proxy-1",
						Port:        7946,
						TXTRecords: map[string]string{
							"version": "1.0",
							"role":    "gateway",
						},
					},
				},
			},
		},
		{
			name: "valid with disabled discovery",
			config: cluster.Config{
				Memberlist: cluster.MemberlistConfig{
					BindAddress: "127.0.0.1:7946",
					Peers: cluster.PeersConfig{
						Join:       []string{"127.0.0.1:7947"},
						Encryption: "",
					},
					Gossip: cluster.GossipConfig{
						Interval: 500 * time.Millisecond,
						Nodes:    2,
					},
					Probe: cluster.ProbeConfig{
						Interval: 3 * time.Second,
						Timeout:  2 * time.Second,
					},
					Metadata: map[string]string{},
				},
				Discovery: cluster.DiscoveryConfig{
					Enabled:   false,
					Interval:  30 * time.Second, // Still needs to be positive
					Providers: []string{},
					Static:    nil,
					MDNS: cluster.MDNSConfig{
						Enabled: false,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestClusterConfig_Validate_Failure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      cluster.Config
		expectedErr string
	}{
		{
			name: "memberlist without bind address",
			config: cluster.Config{
				Memberlist: cluster.MemberlistConfig{
					BindAddress: "",
				},
			},
			expectedErr: "memberlist bind address is required",
		},
		{
			name: "memberlist with invalid bind address format",
			config: cluster.Config{
				Memberlist: cluster.MemberlistConfig{
					BindAddress: "invalid-address",
				},
			},
			expectedErr: "invalid memberlist bind address format",
		},
		{
			name: "memberlist with invalid advertise address format",
			config: cluster.Config{
				Memberlist: cluster.MemberlistConfig{
					BindAddress:      "0.0.0.0:7946",
					AdvertiseAddress: "invalid-advertise",
				},
			},
			expectedErr: "invalid memberlist advertise address format",
		},
		{
			name: "memberlist with zero gossip interval",
			config: cluster.Config{
				Memberlist: cluster.MemberlistConfig{
					BindAddress: "0.0.0.0:7946",
					Gossip: cluster.GossipConfig{
						Interval: 0,
						Nodes:    3,
					},
				},
			},
			expectedErr: "gossip interval must be positive",
		},
		{
			name: "memberlist with zero gossip nodes",
			config: cluster.Config{
				Memberlist: cluster.MemberlistConfig{
					BindAddress: "0.0.0.0:7946",
					Gossip: cluster.GossipConfig{
						Interval: 200 * time.Millisecond,
						Nodes:    0,
					},
				},
			},
			expectedErr: "gossip nodes must be positive",
		},
		{
			name: "memberlist with zero probe interval",
			config: cluster.Config{
				Memberlist: cluster.MemberlistConfig{
					BindAddress: "0.0.0.0:7946",
					Gossip: cluster.GossipConfig{
						Interval: 200 * time.Millisecond,
						Nodes:    3,
					},
					Probe: cluster.ProbeConfig{
						Interval: 0,
						Timeout:  500 * time.Millisecond,
					},
				},
			},
			expectedErr: "probe interval must be positive",
		},
		{
			name: "memberlist with zero probe timeout",
			config: cluster.Config{
				Memberlist: cluster.MemberlistConfig{
					BindAddress: "0.0.0.0:7946",
					Gossip: cluster.GossipConfig{
						Interval: 200 * time.Millisecond,
						Nodes:    3,
					},
					Probe: cluster.ProbeConfig{
						Interval: 1 * time.Second,
						Timeout:  0,
					},
				},
			},
			expectedErr: "probe timeout must be positive",
		},
		{
			name: "probe timeout >= probe interval",
			config: cluster.Config{
				Memberlist: cluster.MemberlistConfig{
					BindAddress: "0.0.0.0:7946",
					Gossip: cluster.GossipConfig{
						Interval: 200 * time.Millisecond,
						Nodes:    3,
					},
					Probe: cluster.ProbeConfig{
						Interval: 1 * time.Second,
						Timeout:  1 * time.Second, // Equal to interval
					},
				},
			},
			expectedErr: "probe timeout",
		},
		{
			name: "discovery enabled with zero interval",
			config: cluster.Config{
				Memberlist: cluster.MemberlistConfig{
					BindAddress: "0.0.0.0:7946",
					Gossip: cluster.GossipConfig{
						Interval: 200 * time.Millisecond,
						Nodes:    3,
					},
					Probe: cluster.ProbeConfig{
						Interval: 1 * time.Second,
						Timeout:  500 * time.Millisecond,
					},
				},
				Discovery: cluster.DiscoveryConfig{
					Enabled:  true,
					Interval: 0,
				},
			},
			expectedErr: "discovery interval must be positive",
		},
		{
			name: "mdns enabled without service name",
			config: cluster.Config{
				Memberlist: cluster.MemberlistConfig{
					BindAddress: "0.0.0.0:7946",
					Gossip: cluster.GossipConfig{
						Interval: 200 * time.Millisecond,
						Nodes:    3,
					},
					Probe: cluster.ProbeConfig{
						Interval: 1 * time.Second,
						Timeout:  500 * time.Millisecond,
					},
				},
				Discovery: cluster.DiscoveryConfig{
					Enabled:   true,
					Interval:  30 * time.Second,
					Providers: []string{"mdns"},
					MDNS: cluster.MDNSConfig{
						Enabled:     true,
						ServiceName: "",
					},
				},
			},
			expectedErr: "mDNS service name is required when mDNS is enabled",
		},
		{
			name: "mdns enabled without domain",
			config: cluster.Config{
				Memberlist: cluster.MemberlistConfig{
					BindAddress: "0.0.0.0:7946",
					Gossip: cluster.GossipConfig{
						Interval: 200 * time.Millisecond,
						Nodes:    3,
					},
					Probe: cluster.ProbeConfig{
						Interval: 1 * time.Second,
						Timeout:  500 * time.Millisecond,
					},
				},
				Discovery: cluster.DiscoveryConfig{
					Enabled:   true,
					Interval:  30 * time.Second,
					Providers: []string{"mdns"},
					MDNS: cluster.MDNSConfig{
						Enabled:     true,
						ServiceName: "vm-proxy-auth",
						Domain:      "",
					},
				},
			},
			expectedErr: "mDNS domain is required when mDNS is enabled",
		},
		{
			name: "mdns enabled with invalid port",
			config: cluster.Config{
				Memberlist: cluster.MemberlistConfig{
					BindAddress: "0.0.0.0:7946",
					Gossip: cluster.GossipConfig{
						Interval: 200 * time.Millisecond,
						Nodes:    3,
					},
					Probe: cluster.ProbeConfig{
						Interval: 1 * time.Second,
						Timeout:  500 * time.Millisecond,
					},
				},
				Discovery: cluster.DiscoveryConfig{
					Enabled:   true,
					Interval:  30 * time.Second,
					Providers: []string{"mdns"},
					MDNS: cluster.MDNSConfig{
						Enabled:     true,
						ServiceName: "vm-proxy-auth",
						Domain:      "local",
						Port:        0,
					},
				},
			},
			expectedErr: "mDNS port must be positive when mDNS is enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestClusterConfig_Defaults(t *testing.T) {
	t.Parallel()

	defaults := cluster.GetDefaults()

	// Memberlist defaults
	assert.Equal(t, "0.0.0.0:7946", defaults.Memberlist.BindAddress)
	assert.Empty(t, defaults.Memberlist.AdvertiseAddress)
	assert.Equal(t, []string{}, defaults.Memberlist.Peers.Join)
	assert.Empty(t, defaults.Memberlist.Peers.Encryption)
	assert.Equal(t, 200*time.Millisecond, defaults.Memberlist.Gossip.Interval)
	assert.Equal(t, 3, defaults.Memberlist.Gossip.Nodes)
	assert.Equal(t, 1*time.Second, defaults.Memberlist.Probe.Interval)
	assert.Equal(t, 500*time.Millisecond, defaults.Memberlist.Probe.Timeout)
	assert.Equal(t, map[string]string{}, defaults.Memberlist.Metadata)

	// Discovery defaults
	assert.Equal(t, 30*time.Second, defaults.Discovery.Interval)
	assert.Nil(t, defaults.Discovery.Static)

	// MDNS defaults
	assert.True(t, defaults.Discovery.MDNS.Enabled)
	assert.Equal(t, "_vm-proxy-auth._tcp", defaults.Discovery.MDNS.ServiceName)
	assert.Equal(t, "local.", defaults.Discovery.MDNS.Domain)
	assert.Empty(t, defaults.Discovery.MDNS.Hostname)
	assert.Equal(t, 7946, defaults.Discovery.MDNS.Port)
	assert.Equal(t, map[string]string{}, defaults.Discovery.MDNS.TXTRecords)
}

func TestDiscoveryConfig_ProviderValidation(t *testing.T) {
	t.Parallel()

	validProviders := []string{"static", "mdns"}

	for _, provider := range validProviders {
		t.Run("valid_provider_"+provider, func(t *testing.T) {
			t.Parallel()
			config := cluster.DiscoveryConfig{
				Enabled:   true,
				Interval:  30 * time.Second,
				Providers: []string{provider},
				MDNS: cluster.MDNSConfig{
					Enabled:     provider == "mdns",
					ServiceName: "vm-proxy-auth",
					Domain:      "local",
					Port:        7946,
				},
			}

			if provider == "static" {
				config.Static = &cluster.StaticConfig{
					Peers: []string{"peer1:7946"},
				}
			}

			err := config.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestMemberlistConfig_AddressValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		bindAddr       string
		advertiseAddr  string
		expectError    bool
		expectedErrMsg string
	}{
		{
			name:          "valid IPv4 addresses",
			bindAddr:      "192.168.1.100:7946",
			advertiseAddr: "192.168.1.100:7946",
			expectError:   false,
		},
		{
			name:          "valid IPv6 addresses",
			bindAddr:      "[::1]:7946",
			advertiseAddr: "[2001:db8::1]:7946",
			expectError:   false,
		},
		{
			name:          "valid hostname addresses",
			bindAddr:      "vm-proxy-1:7946",
			advertiseAddr: "vm-proxy-1.example.com:7946",
			expectError:   false,
		},
		{
			name:          "bind address without advertise",
			bindAddr:      "0.0.0.0:7946",
			advertiseAddr: "", // Empty is valid
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config := cluster.MemberlistConfig{
				BindAddress:      tt.bindAddr,
				AdvertiseAddress: tt.advertiseAddr,
				Gossip: cluster.GossipConfig{
					Interval: 200 * time.Millisecond,
					Nodes:    3,
				},
				Probe: cluster.ProbeConfig{
					Interval: 1 * time.Second,
					Timeout:  500 * time.Millisecond,
				},
			}

			err := config.Validate()
			if tt.expectError {
				require.Error(t, err)
				if tt.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tt.expectedErrMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
