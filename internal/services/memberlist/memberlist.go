package memberlist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/memberlist"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	infralogger "github.com/edelwud/vm-proxy-auth/internal/infrastructure/logger"
)

const (
	// defaultLeaveTimeout is the maximum time to wait for graceful cluster leave.
	defaultLeaveTimeout = 2 * time.Second
)

// Service implements cluster membership using HashiCorp's memberlist.
type Service struct {
	list      *memberlist.Memberlist
	config    config.MemberlistSettings
	delegate  *RaftDelegate
	logger    domain.Logger
	discovery domain.MemberlistDiscoveryService

	raftMgr   domain.RaftManager
	stopCh    chan struct{}
	mu        sync.RWMutex
	running   bool
	wasLeader bool // Track previous leadership state
}

// NewMemberlistService creates a new memberlist service instance.
func NewMemberlistService(config config.MemberlistSettings, logger domain.Logger) (*Service, error) {
	return NewMemberlistServiceWithMetadata(config, logger, nil, nil)
}

// NewMemberlistServiceWithMetadata creates a new memberlist service instance with optional node metadata and Raft manager.
func NewMemberlistServiceWithMetadata(
	config config.MemberlistSettings,
	logger domain.Logger,
	nodeMetadata *NodeMetadata,
	raftManager domain.RaftManager,
) (*Service, error) {
	ms := &Service{
		config: config,
		logger: logger.With(domain.Field{Key: domain.LogFieldComponent, Value: "memberlist"}),
		stopCh: make(chan struct{}),
	}

	// Create delegate for Raft integration
	ms.delegate = NewRaftDelegate(ms, logger)

	// Set metadata if provided
	if nodeMetadata != nil {
		ms.delegate.SetNodeMetadata(nodeMetadata)
		logger.Info("Pre-set node metadata on delegate",
			domain.Field{Key: "node_id", Value: nodeMetadata.NodeID},
			domain.Field{Key: "role", Value: nodeMetadata.Role})
	}

	// Set Raft manager if provided
	if raftManager != nil {
		ms.raftMgr = raftManager
		ms.delegate.SetRaftManager(raftManager)
		logger.Info("Pre-set Raft manager on delegate")
	}

	// Create memberlist configuration
	mlConfig := memberlist.DefaultWANConfig()

	// Apply custom configuration
	if err := ms.applyConfig(mlConfig); err != nil {
		return nil, fmt.Errorf("failed to apply memberlist config: %w", err)
	}

	// Set delegate
	mlConfig.Delegate = ms.delegate
	mlConfig.Events = ms.delegate

	// Set logger adapter for HashiCorp memberlist
	hclogAdapter := infralogger.NewHCLogAdapter(ms.logger)
	mlConfig.Logger = hclogAdapter.StandardLogger(nil)

	// Create memberlist instance
	list, err := memberlist.Create(mlConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create memberlist: %w", err)
	}

	ms.list = list

	// Cache local node name in delegate to avoid deadlock
	ms.delegate.SetLocalNodeName(list.LocalNode().Name)

	ms.logger.Debug("Memberlist service created",
		domain.Field{Key: "bind_address", Value: mlConfig.BindAddr},
		domain.Field{Key: "bind_port", Value: mlConfig.BindPort},
		domain.Field{Key: "node_name", Value: list.LocalNode().Name})

	return ms, nil
}

// applyConfig applies custom configuration to memberlist config.
func (ms *Service) applyConfig(mlConfig *memberlist.Config) error {
	// Basic network settings
	mlConfig.BindAddr = ms.config.BindAddress
	mlConfig.BindPort = ms.config.BindPort

	if ms.config.AdvertiseAddress != "" {
		mlConfig.AdvertiseAddr = ms.config.AdvertiseAddress
	}
	if ms.config.AdvertisePort > 0 {
		mlConfig.AdvertisePort = ms.config.AdvertisePort
	}

	// Gossip settings
	if ms.config.GossipInterval > 0 {
		mlConfig.GossipInterval = ms.config.GossipInterval
	}
	if ms.config.GossipNodes > 0 {
		mlConfig.GossipNodes = ms.config.GossipNodes
	}
	if ms.config.ProbeInterval > 0 {
		mlConfig.ProbeInterval = ms.config.ProbeInterval
	}
	if ms.config.ProbeTimeout > 0 {
		mlConfig.ProbeTimeout = ms.config.ProbeTimeout
	}

	// Encryption if configured
	if ms.config.EncryptionKey != "" {
		key, err := DecodeEncryptionKey(ms.config.EncryptionKey)
		if err != nil {
			return fmt.Errorf("invalid encryption key: %w", err)
		}
		mlConfig.SecretKey = key
	}

	// Node name based on metadata or default
	if name, exists := ms.config.Metadata["node_name"]; exists {
		mlConfig.Name = name
	}

	return nil
}

// SetRaftManager sets the Raft manager for cluster coordination.
func (ms *Service) SetRaftManager(raftMgr domain.RaftManager) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.raftMgr = raftMgr
	ms.delegate.SetRaftManager(raftMgr)
}

// SetDiscoveryService sets the discovery service for auto peer discovery.
func (ms *Service) SetDiscoveryService(discovery domain.MemberlistDiscoveryService) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.discovery = discovery
	if discovery != nil {
		discovery.SetPeerJoiner(ms)
	}
}

// Start starts the memberlist service.
func (ms *Service) Start(ctx context.Context) error {
	ms.mu.Lock()
	if ms.running {
		ms.mu.Unlock()
		return errors.New("memberlist service already running")
	}
	ms.running = true
	ms.mu.Unlock()

	ms.logger.Debug("Starting memberlist service")

	// Start discovery service if configured
	if ms.discovery != nil {
		if err := ms.discovery.Start(ctx); err != nil {
			ms.logger.Warn("Failed to start discovery service",
				domain.Field{Key: "error", Value: err.Error()})
		}
	}

	// Start leadership monitoring if Raft manager is available
	if ms.raftMgr != nil {
		go ms.monitorLeadership(ctx)
	}

	return nil
}

// Join joins the cluster using the configured join nodes.
func (ms *Service) Join(joinNodes []string) error {
	if len(joinNodes) == 0 {
		ms.logger.Debug("No join nodes configured, starting as first node")
		return nil
	}

	ms.logger.Debug("Joining memberlist cluster",
		domain.Field{Key: "join_nodes_count", Value: len(joinNodes)},
		domain.Field{Key: "cluster_size", Value: ms.list.NumMembers()})

	joined, err := ms.list.Join(joinNodes)
	if err != nil {
		return fmt.Errorf("failed to join cluster: %w", err)
	}

	ms.logger.Info("Joined memberlist cluster",
		domain.Field{Key: "nodes_contacted", Value: joined},
		domain.Field{Key: "cluster_size", Value: ms.list.NumMembers()})

	// Log cluster state after join
	members := ms.list.Members()
	ms.logger.Debug("Cluster state after join",
		domain.Field{Key: "members_count", Value: len(members)})

	// Process existing cluster members for Raft integration
	// Add slight delay to allow memberlist gossip to propagate
	go func() {
		time.Sleep(domain.DefaultMemberlistProcessDelay)
		ms.processExistingMembers()
	}()

	return nil
}

// Stop gracefully stops the memberlist service.
func (ms *Service) Stop() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if !ms.running {
		return nil
	}

	// Stop discovery service first
	if ms.discovery != nil {
		if err := ms.discovery.Stop(); err != nil {
			ms.logger.Warn("Failed to stop discovery service",
				domain.Field{Key: "error", Value: err.Error()})
		}
	}

	close(ms.stopCh)
	ms.running = false

	// Signal delegate to stop processing notifications to avoid deadlock
	if ms.delegate != nil {
		atomic.StoreInt64(&ms.delegate.shutdownFlag, 1)
	}

	if ms.list != nil {
		// Try to leave gracefully with a shorter timeout to avoid hanging
		leaveCtx, cancel := context.WithTimeout(context.Background(), defaultLeaveTimeout)
		leaveDone := make(chan error, 1)

		go func() {
			leaveDone <- ms.list.Leave(defaultLeaveTimeout)
		}()

		select {
		case err := <-leaveDone:
			if err != nil {
				ms.logger.Warn("Failed to leave cluster gracefully",
					domain.Field{Key: "error", Value: err.Error()})
			}
		case <-leaveCtx.Done():
			ms.logger.Warn("Timeout leaving cluster - forcing shutdown")
		}
		cancel()

		// Always attempt shutdown
		if err := ms.list.Shutdown(); err != nil {
			ms.logger.Error("Failed to shutdown memberlist",
				domain.Field{Key: "error", Value: err.Error()})
			// Don't return error - we want to continue cleanup
		}
	}

	ms.logger.Info("Memberlist service stopped")
	return nil
}

// GetMembers returns current cluster members.
func (ms *Service) GetMembers() []*memberlist.Node {
	if ms.list == nil {
		return nil
	}
	return ms.list.Members()
}

// GetLocalNode returns the local node information.
func (ms *Service) GetLocalNode() *memberlist.Node {
	if ms.list == nil {
		return nil
	}
	return ms.list.LocalNode()
}

// GetMember returns a specific member by name.
func (ms *Service) GetMember(name string) *memberlist.Node {
	for _, member := range ms.GetMembers() {
		if member.Name == name {
			return member
		}
	}
	return nil
}

// IsHealthy checks if the memberlist service is healthy.
func (ms *Service) IsHealthy() bool {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.running && ms.list != nil
}

// GetClusterSize returns the current cluster size.
func (ms *Service) GetClusterSize() int {
	return ms.list.NumMembers()
}

// GetDelegate returns the Raft delegate for configuration.
func (ms *Service) GetDelegate() *RaftDelegate {
	return ms.delegate
}

// processExistingMembers processes all current members for Raft integration.
func (ms *Service) processExistingMembers() {
	if ms.list == nil {
		ms.logger.Debug("Memberlist not available for processing members")
		return
	}

	members := ms.list.Members()
	localNode := ms.list.LocalNode()

	ms.logger.Info("Processing existing cluster members for Raft integration",
		domain.Field{Key: "total_members", Value: len(members)},
		domain.Field{Key: "local_node", Value: localNode.Name})

	// Debug: Print all member details
	for i, member := range members {
		ms.logger.Debug("Member details",
			domain.Field{Key: "index", Value: i},
			domain.Field{Key: "name", Value: member.Name},
			domain.Field{Key: "addr", Value: member.Addr.String()},
			domain.Field{Key: "port", Value: member.Port},
			domain.Field{Key: "meta_size", Value: len(member.Meta)})
	}

	processed := 0
	for _, member := range members {
		// Skip self
		if member.Name == localNode.Name {
			ms.logger.Debug("Skipping local node",
				domain.Field{Key: "local_node", Value: member.Name})
			continue
		}

		ms.logger.Debug("Processing cluster member",
			domain.Field{Key: "member_name", Value: member.Name},
			domain.Field{Key: "address", Value: fmt.Sprintf("%s:%d", member.Addr.String(), member.Port)})

		// Process this member as if it just joined
		ms.delegate.NotifyJoin(member)
		processed++
	}

	ms.logger.Debug("Processed existing cluster members",
		domain.Field{Key: "members_processed", Value: processed})
}

// monitorLeadership monitors Raft leadership changes and processes pending peers.
func (ms *Service) monitorLeadership(ctx context.Context) {
	ticker := time.NewTicker(domain.DefaultLeadershipCheckInterval)
	defer ticker.Stop()

	ms.logger.Debug("Started leadership monitoring")

	for {
		select {
		case <-ctx.Done():
			ms.logger.Debug("Leadership monitoring stopped due to context cancellation")
			return
		case <-ms.stopCh:
			ms.logger.Debug("Leadership monitoring stopped due to service shutdown")
			return
		case <-ticker.C:
			ms.checkLeadershipChange()
		}
	}
}

// checkLeadershipChange checks for leadership state changes and processes pending peers.
func (ms *Service) checkLeadershipChange() {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if ms.raftMgr == nil {
		return
	}

	isLeader := ms.raftMgr.IsLeader()

	// Check if leadership state changed from follower to leader
	if isLeader && !ms.wasLeader {
		ms.logger.Info("Leadership state changed: became leader")
		ms.wasLeader = true

		// Process pending peers when becoming leader
		if ms.delegate != nil {
			go ms.delegate.ProcessPendingPeers()
		}
	} else if !isLeader && ms.wasLeader {
		ms.logger.Info("Leadership state changed: became follower")
		ms.wasLeader = false
	}
}

// NodeMetadata represents node metadata stored in memberlist.
type NodeMetadata struct {
	NodeID      string            `json:"node_id"`
	HTTPAddress string            `json:"http_address"`
	RaftAddress string            `json:"raft_address"`
	Role        string            `json:"role"`
	Region      string            `json:"region,omitempty"`
	Zone        string            `json:"zone,omitempty"`
	Version     string            `json:"version,omitempty"`
	ExtraData   map[string]string `json:"extra_data,omitempty"`
}

// CreateNodeMetadata creates node metadata from configuration.
func CreateNodeMetadata(nodeID, httpAddr, raftAddr string, metadata map[string]string) (*NodeMetadata, error) {
	nodeMeta := &NodeMetadata{
		NodeID:      nodeID,
		HTTPAddress: httpAddr,
		RaftAddress: raftAddr,
		Role:        "peer",
		ExtraData:   make(map[string]string),
	}

	// Apply metadata from configuration
	for key, value := range metadata {
		switch key {
		case "region":
			nodeMeta.Region = value
		case "zone":
			nodeMeta.Zone = value
		case "version":
			nodeMeta.Version = value
		case "role":
			nodeMeta.Role = value
		default:
			nodeMeta.ExtraData[key] = value
		}
	}

	return nodeMeta, nil
}

// Marshal converts node metadata to bytes for memberlist.
func (m *NodeMetadata) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

// UnmarshalNodeMetadata unmarshals node metadata from bytes.
func UnmarshalNodeMetadata(data []byte) (*NodeMetadata, error) {
	if len(data) == 0 {
		return nil, errors.New("empty metadata")
	}

	var meta NodeMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal node metadata: %w", err)
	}

	return &meta, nil
}

// DecodeEncryptionKey decodes base64 encryption key.
func DecodeEncryptionKey(key string) ([]byte, error) {
	if len(key) == 0 {
		return nil, nil
	}

	// For now, expect the key to be provided as base64
	// In production, you might want to use more sophisticated key management
	decoded := make([]byte, domain.AESKeySize)
	copy(decoded, []byte(key))

	if len(key) < domain.AESKeySize {
		return nil, errors.New("encryption key must be at least 32 bytes")
	}

	return decoded[:domain.AESKeySize], nil
}
