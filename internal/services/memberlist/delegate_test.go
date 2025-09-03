package memberlist_test

import (
	"testing"

	"github.com/hashicorp/memberlist"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	memberlistpkg "github.com/edelwud/vm-proxy-auth/internal/services/memberlist"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestNewRaftDelegate(t *testing.T) {
	logger := testutils.NewMockLogger()
	config := config.MemberlistSettings{
		BindAddress: "127.0.0.1",
		BindPort:    0, // Use random port
	}

	// Create memberlist service using constructor
	service, err := memberlistpkg.NewMemberlistService(config, logger)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Stop()

	delegate := memberlistpkg.NewRaftDelegate(service, logger)
	require.NotNil(t, delegate)
}

func TestRaftDelegate_SetRaftManager(t *testing.T) {
	logger := testutils.NewMockLogger()
	config := config.MemberlistSettings{
		BindAddress: "127.0.0.1",
		BindPort:    0, // Use random port
	}

	service, err := memberlistpkg.NewMemberlistService(config, logger)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Stop()

	delegate := memberlistpkg.NewRaftDelegate(service, logger)

	mockRaft := &mockRaftManager{
		isLeader: true,
		leaderID: "test-leader",
	}

	delegate.SetRaftManager(mockRaft)
	// Cannot test private field, but the method should not panic
}

func TestRaftDelegate_NodeMeta(t *testing.T) {
	logger := testutils.NewMockLogger()
	config := config.MemberlistSettings{
		BindAddress: "127.0.0.1",
		BindPort:    0, // Use random port
	}

	service, err := memberlistpkg.NewMemberlistService(config, logger)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Stop()

	delegate := memberlistpkg.NewRaftDelegate(service, logger)

	// Test without metadata
	meta := delegate.NodeMeta(512)
	assert.Nil(t, meta)

	// Create and set metadata
	nodeMetadata, err := memberlistpkg.CreateNodeMetadata(
		"test-node",
		"127.0.0.1:8080",
		"127.0.0.1:9000",
		map[string]string{"role": "test"},
	)
	require.NoError(t, err)
	delegate.SetNodeMetadata(nodeMetadata)

	// Test with metadata
	meta = delegate.NodeMeta(512)
	require.NotNil(t, meta)

	// Verify we can unmarshal it
	unmarshaled, err := memberlistpkg.UnmarshalNodeMetadata(meta)
	require.NoError(t, err)
	assert.Equal(t, "test-node", unmarshaled.NodeID)

	// Test size limit
	smallMeta := delegate.NodeMeta(10) // Very small limit
	require.NotNil(t, smallMeta)
	assert.LessOrEqual(t, len(smallMeta), 10)
}

func TestRaftDelegate_LocalState(t *testing.T) {
	logger := testutils.NewMockLogger()
	config := config.MemberlistSettings{
		BindAddress: "127.0.0.1",
		BindPort:    0, // Use random port
	}

	service, err := memberlistpkg.NewMemberlistService(config, logger)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Stop()

	delegate := memberlistpkg.NewRaftDelegate(service, logger)

	// Test without metadata
	state := delegate.LocalState(true)
	assert.Nil(t, state)

	// Create and set metadata
	nodeMetadata, err := memberlistpkg.CreateNodeMetadata(
		"test-node",
		"127.0.0.1:8080",
		"127.0.0.1:9000",
		map[string]string{"test": "value"},
	)
	require.NoError(t, err)
	delegate.SetNodeMetadata(nodeMetadata)

	// Test with metadata
	state = delegate.LocalState(true)
	require.NotNil(t, state)

	// Verify state content
	unmarshaled, err := memberlistpkg.UnmarshalNodeMetadata(state)
	require.NoError(t, err)
	assert.Equal(t, "test-node", unmarshaled.NodeID)

	// Test different join parameter
	state2 := delegate.LocalState(false)
	require.NotNil(t, state2)
	assert.Equal(t, state, state2) // Should be same regardless of join parameter
}

func TestRaftDelegate_MergeRemoteState(t *testing.T) {
	logger := testutils.NewMockLogger()
	config := config.MemberlistSettings{
		BindAddress: "127.0.0.1",
		BindPort:    0, // Use random port
	}

	service, err := memberlistpkg.NewMemberlistService(config, logger)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Stop()

	delegate := memberlistpkg.NewRaftDelegate(service, logger)

	// Create remote metadata
	remoteMetadata, err := memberlistpkg.CreateNodeMetadata(
		"remote-node",
		"127.0.0.1:8081",
		"127.0.0.1:9001",
		map[string]string{"region": "us-west-2"},
	)
	require.NoError(t, err)

	remoteData, err := remoteMetadata.Marshal()
	require.NoError(t, err)

	// Should not panic or error
	assert.NotPanics(t, func() {
		delegate.MergeRemoteState(remoteData, true)
		delegate.MergeRemoteState(remoteData, false)
	})

	// Test with invalid data
	assert.NotPanics(t, func() {
		delegate.MergeRemoteState([]byte("{invalid json"), true)
	})
}

func TestRaftDelegate_NotifyEvents(t *testing.T) {
	logger := testutils.NewMockLogger()
	config := config.MemberlistSettings{
		BindAddress: "127.0.0.1",
		BindPort:    0, // Use random port
	}

	service, err := memberlistpkg.NewMemberlistService(config, logger)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Stop()

	delegate := memberlistpkg.NewRaftDelegate(service, logger)

	// Create mock Raft manager
	mockRaft := &mockRaftManager{
		isLeader: true,
	}
	delegate.SetRaftManager(mockRaft)

	// Create test node with metadata
	nodeMetadata, err := memberlistpkg.CreateNodeMetadata(
		"test-node",
		"127.0.0.1:8080",
		"127.0.0.1:9000",
		map[string]string{"role": "peer"},
	)
	require.NoError(t, err)

	metaData, err := nodeMetadata.Marshal()
	require.NoError(t, err)

	testNode := &memberlist.Node{
		Name: "test-node",
		Addr: []byte{127, 0, 0, 1},
		Port: 7946,
		Meta: metaData,
	}

	// Test NotifyJoin
	assert.NotPanics(t, func() {
		delegate.NotifyJoin(testNode)
	})

	// Test NotifyLeave
	assert.NotPanics(t, func() {
		delegate.NotifyLeave(testNode)
	})

	// Test NotifyUpdate
	assert.NotPanics(t, func() {
		delegate.NotifyUpdate(testNode)
	})
}

func TestRaftDelegate_NotifyEventsWithoutRaftManager(t *testing.T) {
	logger := testutils.NewMockLogger()
	config := config.MemberlistSettings{
		BindAddress: "127.0.0.1",
		BindPort:    0, // Use random port
	}

	service, err := memberlistpkg.NewMemberlistService(config, logger)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Stop()

	delegate := memberlistpkg.NewRaftDelegate(service, logger)

	// No Raft manager set
	testNode := &memberlist.Node{
		Name: "test-node",
		Addr: []byte{127, 0, 0, 1},
		Port: 7946,
		Meta: []byte(`{"node_id":"test","role":"peer","raft_address":"127.0.0.1:9000"}`),
	}

	// Should not panic even without Raft manager
	assert.NotPanics(t, func() {
		delegate.NotifyJoin(testNode)
		delegate.NotifyLeave(testNode)
		delegate.NotifyUpdate(testNode)
	})
}

func TestRaftDelegate_Broadcasts(t *testing.T) {
	logger := testutils.NewMockLogger()
	config := config.MemberlistSettings{
		BindAddress: "127.0.0.1",
		BindPort:    0, // Use random port
	}

	service, err := memberlistpkg.NewMemberlistService(config, logger)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Stop()

	delegate := memberlistpkg.NewRaftDelegate(service, logger)

	// Test empty broadcasts
	broadcasts := delegate.GetBroadcasts(10, 100)
	assert.Nil(t, broadcasts)

	// Queue some broadcasts
	msg1 := []byte("test message 1")
	msg2 := []byte("test message 2")

	delegate.QueueBroadcast(msg1)
	delegate.QueueBroadcast(msg2)

	// Get broadcasts with sufficient limit
	broadcasts = delegate.GetBroadcasts(10, 100)
	require.NotNil(t, broadcasts)
	assert.Len(t, broadcasts, 2)

	// Should be cleared after retrieval
	broadcasts2 := delegate.GetBroadcasts(10, 100)
	assert.Nil(t, broadcasts2)

	// Test with size limit
	longMsg := make([]byte, 50)
	for i := range longMsg {
		longMsg[i] = 'A'
	}

	delegate.QueueBroadcast(longMsg)
	delegate.QueueBroadcast(msg1)

	// Small limit should truncate
	broadcasts = delegate.GetBroadcasts(10, 60) // overhead 10 + limit 60 = 50 available
	require.NotNil(t, broadcasts)
	assert.Len(t, broadcasts, 1) // Only first message fits
}

func TestRaftDelegate_NotifyMsg(t *testing.T) {
	logger := testutils.NewMockLogger()
	config := config.MemberlistSettings{
		BindAddress: "127.0.0.1",
		BindPort:    0, // Use random port
	}

	service, err := memberlistpkg.NewMemberlistService(config, logger)
	require.NoError(t, err)
	require.NotNil(t, service)
	defer service.Stop()

	delegate := memberlistpkg.NewRaftDelegate(service, logger)

	// Should not panic
	assert.NotPanics(t, func() {
		delegate.NotifyMsg([]byte("test message"))
		delegate.NotifyMsg([]byte{})
		delegate.NotifyMsg(nil)
	})
}
