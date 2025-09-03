package memberlist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/hashicorp/memberlist"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// RaftManager defines the interface for managing Raft cluster membership.
type RaftManager interface {
	AddVoter(nodeID, address string) error
	RemoveServer(nodeID string) error
	GetLeader() (string, string)
	GetPeers() ([]string, error)
	IsLeader() bool
}

// Service implements cluster membership using HashiCorp's memberlist.
type Service struct {
	list     *memberlist.Memberlist
	config   config.MemberlistSettings
	delegate *RaftDelegate
	logger   domain.Logger

	raftMgr RaftManager
	stopCh  chan struct{}
	mu      sync.RWMutex
	running bool
}

// NewMemberlistService creates a new memberlist service instance.
func NewMemberlistService(config config.MemberlistSettings, logger domain.Logger) (*Service, error) {
	ms := &Service{
		config: config,
		logger: logger.With(domain.Field{Key: domain.LogFieldComponent, Value: "memberlist"}),
		stopCh: make(chan struct{}),
	}

	// Create delegate for Raft integration
	ms.delegate = NewRaftDelegate(ms, logger)

	// Create memberlist configuration
	mlConfig := memberlist.DefaultWANConfig()

	// Apply custom configuration
	if err := ms.applyConfig(mlConfig); err != nil {
		return nil, fmt.Errorf("failed to apply memberlist config: %w", err)
	}

	// Set delegate
	mlConfig.Delegate = ms.delegate
	mlConfig.Events = ms.delegate

	// Create memberlist instance
	list, err := memberlist.Create(mlConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create memberlist: %w", err)
	}

	ms.list = list

	ms.logger.Info("Memberlist service created",
		domain.Field{Key: "bind_address", Value: mlConfig.BindAddr},
		domain.Field{Key: "bind_port", Value: mlConfig.BindPort},
		domain.Field{Key: "node_name", Value: mlConfig.Name})

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
func (ms *Service) SetRaftManager(raftMgr RaftManager) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.raftMgr = raftMgr
	ms.delegate.SetRaftManager(raftMgr)
}

// Start starts the memberlist service.
func (ms *Service) Start(_ context.Context) error {
	ms.mu.Lock()
	if ms.running {
		ms.mu.Unlock()
		return errors.New("memberlist service already running")
	}
	ms.running = true
	ms.mu.Unlock()

	ms.logger.Info("Starting memberlist service")
	return nil
}

// Join joins the cluster using the configured join nodes.
func (ms *Service) Join(joinNodes []string) error {
	if len(joinNodes) == 0 {
		ms.logger.Info("No join nodes configured, starting as first node")
		return nil
	}

	ms.logger.Info("Joining memberlist cluster",
		domain.Field{Key: "join_nodes", Value: joinNodes})

	joined, err := ms.list.Join(joinNodes)
	if err != nil {
		return fmt.Errorf("failed to join cluster: %w", err)
	}

	ms.logger.Info("Successfully joined memberlist cluster",
		domain.Field{Key: "nodes_contacted", Value: joined})

	return nil
}

// Stop gracefully stops the memberlist service.
func (ms *Service) Stop() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if !ms.running {
		return nil
	}

	close(ms.stopCh)
	ms.running = false

	if ms.list != nil {
		if err := ms.list.Leave(domain.DefaultLeaveTimeout); err != nil {
			ms.logger.Warn("Failed to leave cluster gracefully",
				domain.Field{Key: "error", Value: err.Error()})
		}
		if err := ms.list.Shutdown(); err != nil {
			ms.logger.Error("Failed to shutdown memberlist",
				domain.Field{Key: "error", Value: err.Error()})
			return err
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
