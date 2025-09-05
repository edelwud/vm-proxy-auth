package memberlist_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	memberlistpkg "github.com/edelwud/vm-proxy-auth/internal/services/memberlist"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestNewMemberlistService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		config    config.MemberlistSettings
		wantError bool
	}{
		{
			name: "valid_configuration",
			config: config.MemberlistSettings{
				BindAddress:    "127.0.0.1",
				BindPort:       0, // Use random port
				GossipInterval: 200 * time.Millisecond,
				GossipNodes:    3,
				ProbeInterval:  1 * time.Second,
				ProbeTimeout:   500 * time.Millisecond,
				Metadata:       map[string]string{"role": "test"},
			},
			wantError: false,
		},
		{
			name: "with_encryption",
			config: config.MemberlistSettings{
				BindAddress:   "127.0.0.1",
				BindPort:      0, // Use random port
				EncryptionKey: "test-key-32-bytes-long-1234567890",
				Metadata:      map[string]string{"role": "secure"},
			},
			wantError: false,
		},
		{
			name: "default_values",
			config: config.MemberlistSettings{
				BindAddress: "127.0.0.1",
				BindPort:    0, // Use random port
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger := testutils.NewMockLogger()

			service, err := memberlistpkg.NewMemberlistService(tt.config, logger)

			if tt.wantError {
				require.Error(t, err)
				assert.Nil(t, service)
			} else {
				require.NoError(t, err)
				require.NotNil(t, service)
				assert.NotNil(t, service.GetDelegate())

				// Clean up
				require.NoError(t, service.Stop())
			}
		})
	}
}

func TestMemberlistService_StartStop(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	config := config.MemberlistSettings{
		BindAddress: "127.0.0.1",
		BindPort:    0, // Use random port
		Metadata:    map[string]string{"role": "test"},
	}

	service, err := memberlistpkg.NewMemberlistService(config, logger)
	require.NoError(t, err)
	require.NotNil(t, service)

	// Test initial state
	assert.False(t, service.IsHealthy())

	// Start service
	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)
	assert.True(t, service.IsHealthy())

	// Test double start
	err = service.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Stop service
	err = service.Stop()
	require.NoError(t, err)
	assert.False(t, service.IsHealthy())

	// Test double stop
	err = service.Stop()
	require.NoError(t, err)
}

func TestMemberlistService_GetMethods(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	config := config.MemberlistSettings{
		BindAddress: "127.0.0.1",
		BindPort:    0, // Use random port
	}

	service, err := memberlistpkg.NewMemberlistService(config, logger)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Stop()

	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)

	// Test GetLocalNode
	localNode := service.GetLocalNode()
	require.NotNil(t, localNode)

	// Test GetMembers
	members := service.GetMembers()
	require.NotNil(t, members)
	assert.Len(t, members, 1) // Should have at least the local node

	// Test GetClusterSize
	clusterSize := service.GetClusterSize()
	assert.Equal(t, 1, clusterSize)

	// Test GetMember
	member := service.GetMember(localNode.Name)
	require.NotNil(t, member)
	assert.Equal(t, localNode.Name, member.Name)

	// Test GetMember with non-existent node
	nonExistent := service.GetMember("non-existent")
	assert.Nil(t, nonExistent)
}

func TestMemberlistService_ClusterFormation(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()

	// Create single service for basic cluster testing
	config := config.MemberlistSettings{
		BindAddress: "127.0.0.1",
		BindPort:    0, // Use random port
		Metadata:    map[string]string{"role": "leader"},
	}

	service, err := memberlistpkg.NewMemberlistService(config, logger)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Stop()

	// Start service
	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)

	// Verify single node cluster
	assert.Equal(t, 1, service.GetClusterSize())

	// Verify we can get local node
	localNode := service.GetLocalNode()
	require.NotNil(t, localNode)
	assert.NotEmpty(t, localNode.Name)

	// Verify members list contains local node
	members := service.GetMembers()
	require.Len(t, members, 1)
	assert.Equal(t, localNode.Name, members[0].Name)
}

func TestNodeMetadata_CreateAndMarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		nodeID      string
		httpAddr    string
		raftAddr    string
		metadata    map[string]string
		expectError bool
	}{
		{
			name:     "basic_metadata",
			nodeID:   "test-node-1",
			httpAddr: "127.0.0.1:8080",
			raftAddr: "127.0.0.1:9000",
			metadata: map[string]string{
				"region": "us-east-1",
				"zone":   "us-east-1a",
			},
			expectError: false,
		},
		{
			name:     "with_role_and_version",
			nodeID:   "test-node-2",
			httpAddr: "127.0.0.1:8081",
			raftAddr: "127.0.0.1:9001",
			metadata: map[string]string{
				"role":    "leader",
				"version": "1.0.0",
				"custom":  "value",
			},
			expectError: false,
		},
		{
			name:        "minimal_metadata",
			nodeID:      "test-node-3",
			httpAddr:    "127.0.0.1:8082",
			raftAddr:    "127.0.0.1:9002",
			metadata:    nil,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Create metadata
			meta, err := memberlistpkg.CreateNodeMetadata(tt.nodeID, tt.httpAddr, tt.raftAddr, tt.metadata)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, meta)

			// Verify basic fields
			assert.Equal(t, tt.nodeID, meta.NodeID)
			assert.Equal(t, tt.httpAddr, meta.HTTPAddress)
			assert.Equal(t, tt.raftAddr, meta.RaftAddress)

			// Test marshaling
			data, err := meta.Marshal()
			require.NoError(t, err)
			require.NotEmpty(t, data)

			// Test unmarshaling
			unmarshaledMeta, err := memberlistpkg.UnmarshalNodeMetadata(data)
			require.NoError(t, err)
			require.NotNil(t, unmarshaledMeta)

			// Verify unmarshaled data
			assert.Equal(t, meta.NodeID, unmarshaledMeta.NodeID)
			assert.Equal(t, meta.HTTPAddress, unmarshaledMeta.HTTPAddress)
			assert.Equal(t, meta.RaftAddress, unmarshaledMeta.RaftAddress)
			assert.Equal(t, meta.Role, unmarshaledMeta.Role)
			assert.Equal(t, meta.Region, unmarshaledMeta.Region)
			assert.Equal(t, meta.Zone, unmarshaledMeta.Zone)
		})
	}
}

func TestUnmarshalNodeMetadata_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		data      []byte
		wantError bool
	}{
		{
			name:      "empty_data",
			data:      []byte{},
			wantError: true,
		},
		{
			name:      "invalid_json",
			data:      []byte("{invalid json"),
			wantError: true,
		},
		{
			name:      "valid_empty_json",
			data:      []byte("{}"),
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			meta, err := memberlistpkg.UnmarshalNodeMetadata(tt.data)

			if tt.wantError {
				require.Error(t, err)
				assert.Nil(t, meta)
			} else {
				require.NoError(t, err)
				require.NotNil(t, meta)
			}
		})
	}
}

func TestDecodeEncryptionKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		key       string
		wantError bool
	}{
		{
			name:      "empty_key",
			key:       "",
			wantError: false, // Empty key is valid (no encryption)
		},
		{
			name:      "valid_32_byte_key",
			key:       "this-is-a-32-byte-key-for-testing",
			wantError: false,
		},
		{
			name:      "too_short_key",
			key:       "short",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			decoded, err := memberlistpkg.DecodeEncryptionKey(tt.key)

			if tt.wantError {
				require.Error(t, err)
				assert.Nil(t, decoded)
			} else {
				require.NoError(t, err)
				if tt.key == "" {
					assert.Nil(t, decoded)
				} else {
					require.NotNil(t, decoded)
					assert.Len(t, decoded, 32)
				}
			}
		})
	}
}

func TestMemberlistService_RaftIntegration(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	config := config.MemberlistSettings{
		BindAddress: "127.0.0.1",
		BindPort:    0, // Use random port
		Metadata:    map[string]string{"role": "test"},
	}

	service, err := memberlistpkg.NewMemberlistService(config, logger)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Stop()

	// Create mock Raft manager
	mockRaft := testutils.NewMockRaftManager()
	mockRaft.LeaderID = "test-leader"
	mockRaft.LeaderAddr = "127.0.0.1:9000"
	mockRaft.Peers = []string{"test-leader"}

	// Set Raft manager
	service.SetRaftManager(mockRaft)

	// Verify delegate can be retrieved
	assert.NotNil(t, service.GetDelegate())

	// Start service
	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)
}

func TestMemberlistService_NodeMetadataIntegration(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	config := config.MemberlistSettings{
		BindAddress: "127.0.0.1",
		BindPort:    0, // Use random port
		Metadata: map[string]string{
			"region":  "us-west-2",
			"zone":    "us-west-2a",
			"version": "1.2.3",
		},
	}

	service, err := memberlistpkg.NewMemberlistService(config, logger)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Stop()

	// Create node metadata
	nodeMetadata, err := memberlistpkg.CreateNodeMetadata(
		"test-node",
		"127.0.0.1:8080",
		"127.0.0.1:9000",
		config.Metadata,
	)
	require.NoError(t, err)
	require.NotNil(t, nodeMetadata)

	// Set metadata on delegate
	service.GetDelegate().SetNodeMetadata(nodeMetadata)

	// Verify metadata is set
	assert.Equal(t, "test-node", nodeMetadata.NodeID)
	assert.Equal(t, "us-west-2", nodeMetadata.Region)
	assert.Equal(t, "us-west-2a", nodeMetadata.Zone)
	assert.Equal(t, "1.2.3", nodeMetadata.Version)

	// Start service to test metadata transmission
	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)
}

func TestMemberlistService_JoinCluster(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	config := config.MemberlistSettings{
		BindAddress: "127.0.0.1",
		BindPort:    0, // Use random port
	}

	service, err := memberlistpkg.NewMemberlistService(config, logger)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Stop()

	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)

	// Test join with empty nodes (should not error)
	err = service.Join([]string{})
	require.NoError(t, err)

	// Test join with invalid nodes (should error but not crash)
	err = service.Join([]string{"invalid-host:7946"})
	require.Error(t, err) // Expected to fail since host doesn't exist
}
