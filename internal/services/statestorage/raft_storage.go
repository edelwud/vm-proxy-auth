package statestorage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"

	"github.com/edelwud/vm-proxy-auth/internal/config/modules/storage"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	infralogger "github.com/edelwud/vm-proxy-auth/internal/infrastructure/logger"
)

// RaftStorageConfig holds Raft storage configuration.
type RaftStorageConfig struct {
	NodeID             string        `json:"node_id"`
	BindAddress        string        `json:"bind_address"`
	DataDir            string        `json:"data_dir"`
	Peers              []string      `json:"peers"`
	HeartbeatTimeout   time.Duration `json:"heartbeat_timeout"`
	ElectionTimeout    time.Duration `json:"election_timeout"`
	LeaderLeaseTimeout time.Duration `json:"leader_lease_timeout"`
	CommitTimeout      time.Duration `json:"commit_timeout"`
	SnapshotRetention  int           `json:"snapshot_retention"`
	SnapshotThreshold  uint64        `json:"snapshot_threshold"`
	TrailingLogs       uint64        `json:"trailing_logs"`
	BootstrapExpected  int           `json:"bootstrap_expected"` // Number of expected servers for bootstrap
}

// RaftStorage implements StateStorage using HashiCorp Raft consensus.
type RaftStorage struct {
	raft        *raft.Raft
	fsm         *raftFSM
	transport   *raft.NetworkTransport
	logStore    raft.LogStore
	stableStore raft.StableStore
	snapStore   raft.SnapshotStore
	config      RaftStorageConfig
	nodeID      string
	watchChans  map[string][]chan domain.StateEvent
	mu          sync.RWMutex
	stopCh      chan struct{}
	wg          sync.WaitGroup
	logger      domain.Logger
	closed      bool

	// Delayed bootstrap support for auto-discovery (Consul pattern)
	bootstrapDeferred bool
}

// NewRaftStorage creates a new Raft-based state storage.
func NewRaftStorage(config RaftStorageConfig, nodeID string, logger domain.Logger) (*RaftStorage, error) {
	logger.Info("Creating Raft storage",
		domain.Field{Key: "node_id", Value: config.NodeID},
		domain.Field{Key: "bind_address", Value: config.BindAddress})

	if err := os.MkdirAll(config.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	logger.Debug("Creating Raft configuration")
	raftConfig := createRaftConfig(config, logger)

	logger.Debug("Creating Raft transport")
	transport, err := createRaftTransport(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	logger.Debug("Creating Raft stores")
	logStore, stableStore, snapStore, err := createRaftStores(config, transport)
	if err != nil {
		_ = transport.Close()
		return nil, err
	}

	logger.Debug("Creating Raft FSM")
	fsm := createRaftFSM(logger)

	logger.Debug("Creating Raft instance")
	// Create Raft instance
	r, err := raft.NewRaft(raftConfig, fsm, logStore, stableStore, snapStore, transport)
	if err != nil {
		cleanupStores(transport, logStore, stableStore)
		return nil, fmt.Errorf("failed to create raft instance: %w", err)
	}

	rs := &RaftStorage{
		raft:        r,
		fsm:         fsm,
		transport:   transport,
		logStore:    logStore,
		stableStore: stableStore,
		snapStore:   snapStore,
		config:      config,
		nodeID:      config.NodeID,
		watchChans:  make(map[string][]chan domain.StateEvent),
		stopCh:      make(chan struct{}),
		logger:      logger.With(domain.Field{Key: domain.LogFieldComponent, Value: "raft.storage"}),
	}

	// Peer discovery is now handled by memberlist integration

	// Bootstrap cluster if needed
	if bootstrapErr := rs.bootstrapCluster(); bootstrapErr != nil {
		_ = rs.Close()
		return nil, fmt.Errorf("failed to bootstrap cluster: %w", bootstrapErr)
	}

	rs.logger.Info("Raft storage initialized",
		domain.Field{Key: "node_id", Value: config.NodeID},
		domain.Field{Key: "bind_address", Value: config.BindAddress},
		domain.Field{Key: "data_dir", Value: config.DataDir},
		domain.Field{Key: "peers", Value: fmt.Sprintf("%v", config.Peers)})

	return rs, nil
}

// createRaftConfig creates and configures a Raft configuration.
func createRaftConfig(config RaftStorageConfig, logger domain.Logger) *raft.Config {
	raftConfig := raft.DefaultConfig()
	raftConfig.LocalID = raft.ServerID(config.NodeID)
	raftConfig.HeartbeatTimeout = config.HeartbeatTimeout
	raftConfig.ElectionTimeout = config.ElectionTimeout
	raftConfig.LeaderLeaseTimeout = config.LeaderLeaseTimeout
	raftConfig.CommitTimeout = config.CommitTimeout
	raftConfig.SnapshotThreshold = config.SnapshotThreshold
	raftConfig.TrailingLogs = config.TrailingLogs
	raftConfig.Logger = infralogger.NewHCLogAdapter(logger)
	return raftConfig
}

// createRaftTransport creates a Raft network transport.
func createRaftTransport(config RaftStorageConfig) (*raft.NetworkTransport, error) {
	addr, err := net.ResolveTCPAddr("tcp", config.BindAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve bind address: %w", err)
	}

	transport, err := raft.NewTCPTransport(
		config.BindAddress,
		addr,
		storage.DefaultRaftMaxConnections,
		storage.DefaultRaftApplyTimeout,
		os.Stderr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	return transport, nil
}

// createRaftStores creates BoltDB stores for Raft.
func createRaftStores(config RaftStorageConfig, _ *raft.NetworkTransport) (
	raft.LogStore, raft.StableStore, raft.SnapshotStore, error,
) {
	logStore, err := raftboltdb.NewBoltStore(filepath.Join(config.DataDir, "raft-log.db"))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create log store: %w", err)
	}

	stableStore, err := raftboltdb.NewBoltStore(filepath.Join(config.DataDir, "raft-stable.db"))
	if err != nil {
		_ = logStore.Close()
		return nil, nil, nil, fmt.Errorf("failed to create stable store: %w", err)
	}

	snapStore, err := raft.NewFileSnapshotStore(config.DataDir, config.SnapshotRetention, os.Stderr)
	if err != nil {
		_ = logStore.Close()
		_ = stableStore.Close()
		return nil, nil, nil, fmt.Errorf("failed to create snapshot store: %w", err)
	}

	return logStore, stableStore, snapStore, nil
}

// createRaftFSM creates the Finite State Machine for Raft.
func createRaftFSM(logger domain.Logger) *raftFSM {
	return &raftFSM{
		data:       make(map[string]*storageItem),
		watchChans: make(map[string][]chan domain.StateEvent),
		logger:     logger.With(domain.Field{Key: domain.LogFieldComponent, Value: "raft.fsm"}),
	}
}

// cleanupStores closes transport and stores on error.
func cleanupStores(transport *raft.NetworkTransport, logStore raft.LogStore, stableStore raft.StableStore) {
	_ = transport.Close()
	if closer, ok := logStore.(io.Closer); ok {
		_ = closer.Close()
	}
	if closer, ok := stableStore.(io.Closer); ok {
		_ = closer.Close()
	}
}

// bootstrapCluster handles cluster initialization and joining using Consul-style logic.
func (rs *RaftStorage) bootstrapCluster() error {
	// Check if we have existing state (Consul pattern)
	hasState, err := raft.HasExistingState(rs.logStore, rs.stableStore, rs.snapStore)
	if err != nil {
		return fmt.Errorf("failed to check existing state: %w", err)
	}

	if hasState {
		rs.logger.Info("Found existing Raft state, joining existing cluster")
		return nil
	}

	// Determine bootstrap strategy
	hasPeers := len(rs.config.Peers) > 0

	// Case 1: Static peers configuration
	if hasPeers {
		return rs.bootstrapWithStaticPeers()
	}

	// Case 2: Dynamic discovery - use maybeBootstrap pattern from Consul
	if rs.config.BootstrapExpected > 0 {
		return rs.maybeBootstrap()
	}

	// Case 3: Single node deployment (immediate bootstrap)
	rs.logger.Info("No bootstrap-expect configured, bootstrapping single-node cluster")
	return rs.bootstrapSingleNode()
}

// maybeBootstrap implements Consul-style conditional bootstrap logic.
// Only bootstrap if we have enough peers and no existing Raft state in the cluster.
func (rs *RaftStorage) maybeBootstrap() error {
	rs.logger.Info("Using maybeBootstrap pattern for cluster formation",
		domain.Field{Key: "bootstrap_expected", Value: rs.config.BootstrapExpected})

	// Defer bootstrap until memberlist discovery completes
	rs.bootstrapDeferred = true

	rs.logger.Info("Bootstrap deferred - waiting for memberlist peer discovery",
		domain.Field{Key: "bootstrap_expected", Value: rs.config.BootstrapExpected})

	// Return without bootstrapping - memberlist will call TryDelayedBootstrap later
	return nil
}

// TryDelayedBootstrap attempts bootstrap after peer discovery (Consul pattern).
func (rs *RaftStorage) TryDelayedBootstrap(discoveredPeers []string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if !rs.bootstrapDeferred {
		rs.logger.Debug("Bootstrap not deferred, skipping delayed bootstrap")
		return nil
	}

	rs.logger.Info("Attempting delayed bootstrap after peer discovery",
		domain.Field{Key: "discovered_peers", Value: len(discoveredPeers)},
		domain.Field{Key: "bootstrap_expected", Value: rs.config.BootstrapExpected})

	// Check if we have enough peers (including self)
	totalPeers := len(discoveredPeers) + 1 // +1 for self
	if totalPeers < rs.config.BootstrapExpected {
		rs.logger.Info("Not enough peers for bootstrap, continuing to wait",
			domain.Field{Key: "current_peers", Value: totalPeers},
			domain.Field{Key: "bootstrap_expected", Value: rs.config.BootstrapExpected})
		return nil
	}

	// TODO: Query discovered peers for existing Raft state (Consul pattern)
	// For now, proceed with bootstrap
	rs.logger.Info("Enough peers discovered, proceeding with delayed bootstrap")

	rs.bootstrapDeferred = false

	// Bootstrap the cluster with all discovered peers
	return rs.bootstrapWithDiscoveredPeers(discoveredPeers)
}

// bootstrapWithStaticPeers bootstraps cluster with statically configured peers.
func (rs *RaftStorage) bootstrapWithStaticPeers() error {
	servers := make([]raft.Server, 0, len(rs.config.Peers)+1)

	// Add self as server
	servers = append(servers, raft.Server{
		ID:      raft.ServerID(rs.config.NodeID),
		Address: raft.ServerAddress(rs.config.BindAddress),
	})

	// Add peers
	for _, peer := range rs.config.Peers {
		if peer != rs.config.NodeID+":"+rs.config.BindAddress {
			// Parse peer format: "nodeID:address"
			parts := strings.Split(peer, ":")
			if len(parts) >= storage.DefaultRaftMinPeerParts {
				nodeID := parts[0]
				address := strings.Join(parts[1:], ":")
				servers = append(servers, raft.Server{
					ID:      raft.ServerID(nodeID),
					Address: raft.ServerAddress(address),
				})
			}
		}
	}

	if len(servers) == 0 {
		return errors.New("no valid servers to bootstrap cluster")
	}

	configuration := raft.Configuration{Servers: servers}
	future := rs.raft.BootstrapCluster(configuration)
	if bootstrapErr := future.Error(); bootstrapErr != nil {
		rs.logger.Error("Failed to bootstrap cluster with static peers",
			domain.Field{Key: "error", Value: bootstrapErr.Error()},
			domain.Field{Key: "servers", Value: fmt.Sprintf("%+v", servers)})
		return fmt.Errorf("failed to bootstrap cluster: %w", bootstrapErr)
	}

	rs.logger.Info("Successfully bootstrapped Raft cluster with static peers",
		domain.Field{Key: "servers_count", Value: len(servers)},
		domain.Field{Key: "node_id", Value: rs.config.NodeID})

	return nil
}

// bootstrapWithDiscoveredPeers bootstraps cluster with peers discovered via memberlist.
func (rs *RaftStorage) bootstrapWithDiscoveredPeers(discoveredPeers []string) error {
	servers := make([]raft.Server, 0, len(discoveredPeers)+1)

	// Add self as server
	servers = append(servers, raft.Server{
		ID:      raft.ServerID(rs.config.NodeID),
		Address: raft.ServerAddress(rs.config.BindAddress),
	})

	rs.logger.Info("Bootstrapping cluster with discovered peers",
		domain.Field{Key: "total_servers_expected", Value: len(servers) + len(discoveredPeers)},
		domain.Field{Key: "self_node", Value: rs.config.NodeID},
		domain.Field{Key: "self_address", Value: rs.config.BindAddress})

	// Note: discoveredPeers are memberlist addresses, not Raft addresses
	// For proper Raft integration, we need to get Raft addresses from node metadata
	// For this initial implementation, we'll defer to memberlist to handle peer addition
	// The actual Raft peer addition will happen through the memberlist delegate

	// Bootstrap with just self for now - peers will be added via memberlist
	configuration := raft.Configuration{Servers: servers}
	future := rs.raft.BootstrapCluster(configuration)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to bootstrap cluster: %w", err)
	}

	rs.logger.Info("Successfully bootstrapped Raft cluster - peers will join via memberlist",
		domain.Field{Key: "discovered_peers_count", Value: len(discoveredPeers)})
	return nil
}

// bootstrapSingleNode bootstraps a single-node cluster.
func (rs *RaftStorage) bootstrapSingleNode() error {
	servers := []raft.Server{{
		ID:      raft.ServerID(rs.config.NodeID),
		Address: raft.ServerAddress(rs.config.BindAddress),
	}}

	configuration := raft.Configuration{Servers: servers}
	future := rs.raft.BootstrapCluster(configuration)
	if bootstrapErr := future.Error(); bootstrapErr != nil {
		rs.logger.Error("Failed to bootstrap single-node cluster",
			domain.Field{Key: "error", Value: bootstrapErr.Error()},
			domain.Field{Key: "node_id", Value: rs.config.NodeID})
		return fmt.Errorf("failed to bootstrap single-node cluster: %w", bootstrapErr)
	}

	rs.logger.Info("Successfully bootstrapped single-node Raft cluster",
		domain.Field{Key: "node_id", Value: rs.config.NodeID},
		domain.Field{Key: "bind_address", Value: rs.config.BindAddress})

	return nil
}

// Get retrieves a value by key.
func (rs *RaftStorage) Get(_ context.Context, key string) ([]byte, error) {
	if rs.isClosed() {
		return nil, domain.ErrStorageClosed
	}

	value, exists := rs.fsm.get(key)
	if !exists {
		return nil, domain.ErrKeyNotFound
	}

	rs.logger.Debug("Retrieved value from Raft storage",
		domain.Field{Key: "key", Value: key},
		domain.Field{Key: "value_size", Value: len(value)})

	return value, nil
}

// Set stores a value with optional TTL.
func (rs *RaftStorage) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if rs.isClosed() {
		return domain.ErrStorageClosed
	}

	if rs.raft.State() != raft.Leader {
		leader := string(rs.raft.Leader())
		if leader == "" {
			return errors.New("no leader available for write operation")
		}
		return fmt.Errorf("not leader, current leader: %s", leader)
	}

	cmd := raftCommand{
		Type:      raftCommandSet,
		Key:       key,
		Value:     value,
		TTL:       ttl,
		NodeID:    rs.nodeID,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	future := rs.raft.Apply(data, storage.DefaultRaftApplyTimeout)
	if applyErr := future.Error(); applyErr != nil {
		return fmt.Errorf("failed to apply raft command: %w", applyErr)
	}

	rs.logger.Debug("Set value in Raft storage",
		domain.Field{Key: "key", Value: key},
		domain.Field{Key: "value_size", Value: len(value)},
		domain.Field{Key: "ttl", Value: ttl})

	return nil
}

// Delete removes a key.
func (rs *RaftStorage) Delete(_ context.Context, key string) error {
	if rs.isClosed() {
		return domain.ErrStorageClosed
	}

	if rs.raft.State() != raft.Leader {
		leader := string(rs.raft.Leader())
		if leader == "" {
			return errors.New("no leader available for delete operation")
		}
		return fmt.Errorf("not leader, current leader: %s", leader)
	}

	cmd := raftCommand{
		Type:      raftCommandDelete,
		Key:       key,
		NodeID:    rs.nodeID,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	future := rs.raft.Apply(data, storage.DefaultRaftApplyTimeout)
	if applyErr := future.Error(); applyErr != nil {
		return fmt.Errorf("failed to apply raft command: %w", applyErr)
	}

	rs.logger.Debug("Deleted key from Raft storage",
		domain.Field{Key: "key", Value: key})

	return nil
}

// GetMultiple retrieves multiple values efficiently.
func (rs *RaftStorage) GetMultiple(_ context.Context, keys []string) (map[string][]byte, error) {
	if rs.isClosed() {
		return nil, domain.ErrStorageClosed
	}

	result := make(map[string][]byte)
	for _, key := range keys {
		if value, exists := rs.fsm.get(key); exists {
			result[key] = value
		}
	}

	rs.logger.Debug("Retrieved multiple values from Raft storage",
		domain.Field{Key: "requested_keys", Value: len(keys)},
		domain.Field{Key: "found_keys", Value: len(result)})

	return result, nil
}

// SetMultiple stores multiple values efficiently.
func (rs *RaftStorage) SetMultiple(_ context.Context, items map[string][]byte, ttl time.Duration) error {
	if rs.isClosed() {
		return domain.ErrStorageClosed
	}

	if rs.raft.State() != raft.Leader {
		leader := string(rs.raft.Leader())
		if leader == "" {
			return errors.New("no leader available for bulk write operation")
		}
		return fmt.Errorf("not leader, current leader: %s", leader)
	}

	cmd := raftCommand{
		Type:      raftCommandSetMultiple,
		Items:     items,
		TTL:       ttl,
		NodeID:    rs.nodeID,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal bulk command: %w", err)
	}

	future := rs.raft.Apply(data, storage.DefaultRaftApplyTimeoutBulk)
	if applyErr := future.Error(); applyErr != nil {
		return fmt.Errorf("failed to apply bulk raft command: %w", applyErr)
	}

	rs.logger.Debug("Set multiple values in Raft storage",
		domain.Field{Key: "items_count", Value: len(items)},
		domain.Field{Key: "ttl", Value: ttl})

	return nil
}

// Watch observes changes to keys matching the prefix.
func (rs *RaftStorage) Watch(ctx context.Context, keyPrefix string) (<-chan domain.StateEvent, error) {
	if rs.isClosed() {
		return nil, domain.ErrStorageClosed
	}

	ch := make(chan domain.StateEvent, storage.DefaultRaftWatchChannelSize)

	rs.mu.Lock()
	rs.watchChans[keyPrefix] = append(rs.watchChans[keyPrefix], ch)
	rs.mu.Unlock()

	// Also register with FSM for direct notifications
	rs.fsm.addWatcher(keyPrefix, ch)

	// Cleanup on context cancellation
	go func() {
		<-ctx.Done()
		rs.removeWatcherAndClose(keyPrefix, ch)
	}()

	rs.logger.Debug("Created Raft storage watcher",
		domain.Field{Key: "key_prefix", Value: keyPrefix})

	return ch, nil
}

// Close performs cleanup and graceful shutdown.
func (rs *RaftStorage) Close() error {
	rs.mu.Lock()
	if rs.closed {
		rs.mu.Unlock()
		return nil
	}
	rs.closed = true
	rs.mu.Unlock()

	rs.logger.Info("Shutting down Raft storage")

	// Signal shutdown
	close(rs.stopCh)

	// Close all watchers
	rs.closeAllWatchers()

	// Wait for goroutines
	rs.wg.Wait()

	// Shutdown Raft
	if rs.raft != nil {
		future := rs.raft.Shutdown()
		if applyErr := future.Error(); applyErr != nil {
			rs.logger.Error("Error during Raft shutdown",
				domain.Field{Key: "error", Value: applyErr.Error()})
		}
	}

	// Close stores
	var errs []error
	if rs.transport != nil {
		if err := rs.transport.Close(); err != nil {
			errs = append(errs, fmt.Errorf("transport close error: %w", err))
		}
	}
	// BoltDB stores will be closed by Raft shutdown
	// No explicit close needed for logStore and stableStore

	if len(errs) > 0 {
		return fmt.Errorf("errors during close: %v", errs)
	}

	rs.logger.Info("Raft storage shutdown completed")
	return nil
}

// Ping checks the health of the storage system.
func (rs *RaftStorage) Ping(_ context.Context) error {
	if rs.isClosed() {
		return domain.ErrStorageClosed
	}

	state := rs.raft.State()
	leader := rs.raft.Leader()

	if state == raft.Shutdown {
		return errors.New("raft is shutdown")
	}

	rs.logger.Debug("Raft storage ping",
		domain.Field{Key: "state", Value: state.String()},
		domain.Field{Key: "leader", Value: string(leader)})

	return nil
}

// GetStats returns Raft cluster statistics.
func (rs *RaftStorage) GetStats() map[string]interface{} {
	if rs.raft == nil {
		return nil
	}

	stats := rs.raft.Stats()
	leader := rs.raft.Leader()
	state := rs.raft.State()

	return map[string]interface{}{
		"state":  state.String(),
		"leader": string(leader),
		"stats":  stats,
	}
}

// removeWatcherAndClose removes a watcher and closes its channel.
func (rs *RaftStorage) removeWatcherAndClose(keyPrefix string, ch chan domain.StateEvent) {
	rs.mu.Lock()
	watchers := rs.watchChans[keyPrefix]
	for i, watcher := range watchers {
		if watcher == ch {
			rs.watchChans[keyPrefix] = append(watchers[:i], watchers[i+1:]...)
			break
		}
	}
	if len(rs.watchChans[keyPrefix]) == 0 {
		delete(rs.watchChans, keyPrefix)
	}
	rs.mu.Unlock()

	// Also remove from FSM
	rs.fsm.removeWatcher(keyPrefix, ch)

	// Close channel safely
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// closeAllWatchers closes all active watchers.
func (rs *RaftStorage) closeAllWatchers() {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	for keyPrefix, watchers := range rs.watchChans {
		for _, ch := range watchers {
			select {
			case <-ch:
			default:
				close(ch)
			}
		}
		delete(rs.watchChans, keyPrefix)
	}

	// Also close FSM watchers
	rs.fsm.closeAllWatchers()
}

// isClosed checks if the storage is closed.
func (rs *RaftStorage) isClosed() bool {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return rs.closed
}

// IsLeader checks if this node is the Raft leader.
func (rs *RaftStorage) IsLeader() bool {
	if rs.raft == nil {
		return false
	}
	return rs.raft.State() == raft.Leader
}

// --- RaftManager interface implementation for memberlist integration ---

// AddVoter adds a voting member to the Raft cluster.
func (rs *RaftStorage) AddVoter(nodeID, address string) error {
	if !rs.IsLeader() {
		return errors.New("only leader can add voters")
	}

	serverID := raft.ServerID(nodeID)
	serverAddress := raft.ServerAddress(address)

	rs.logger.Info("Attempting to add Raft voter",
		domain.Field{Key: "node_id", Value: nodeID},
		domain.Field{Key: "address", Value: address},
		domain.Field{Key: "leader_id", Value: string(rs.raft.Leader())})

	// Check current cluster configuration before adding
	configFuture := rs.raft.GetConfiguration()
	if configFuture.Error() != nil {
		rs.logger.Error("Failed to get current configuration",
			domain.Field{Key: "error", Value: configFuture.Error().Error()})
	} else {
		servers := configFuture.Configuration().Servers
		rs.logger.Debug("Current Raft configuration before AddVoter",
			domain.Field{Key: "servers_count", Value: len(servers)})
		for i, server := range servers {
			rs.logger.Debug("Existing server",
				domain.Field{Key: "index", Value: i},
				domain.Field{Key: "id", Value: string(server.ID)},
				domain.Field{Key: "address", Value: string(server.Address)})
		}
	}

	future := rs.raft.AddVoter(serverID, serverAddress, 0, 0)
	if err := future.Error(); err != nil {
		rs.logger.Error("AddVoter failed",
			domain.Field{Key: "node_id", Value: nodeID},
			domain.Field{Key: "address", Value: address},
			domain.Field{Key: "error", Value: err.Error()})
		return fmt.Errorf("failed to add voter: %w", err)
	}

	rs.logger.Info("Added Raft voter successfully",
		domain.Field{Key: "node_id", Value: nodeID},
		domain.Field{Key: "address", Value: address})

	// Check configuration after adding
	configFuture = rs.raft.GetConfiguration()
	if configFuture.Error() != nil {
		rs.logger.Error("Failed to get configuration after AddVoter",
			domain.Field{Key: "error", Value: configFuture.Error().Error()})
	} else {
		servers := configFuture.Configuration().Servers
		rs.logger.Info("Raft configuration after AddVoter",
			domain.Field{Key: "servers_count", Value: len(servers)})
		for i, server := range servers {
			rs.logger.Info("Server in cluster",
				domain.Field{Key: "index", Value: i},
				domain.Field{Key: "id", Value: string(server.ID)},
				domain.Field{Key: "address", Value: string(server.Address)})
		}
	}

	return nil
}

// RemoveServer removes a server from the Raft cluster.
func (rs *RaftStorage) RemoveServer(nodeID string) error {
	if !rs.IsLeader() {
		return errors.New("only leader can remove servers")
	}

	serverID := raft.ServerID(nodeID)

	future := rs.raft.RemoveServer(serverID, 0, 0)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to remove server: %w", err)
	}

	rs.logger.Info("Removed Raft server",
		domain.Field{Key: "node_id", Value: nodeID})

	return nil
}

// GetLeader returns the current leader's node ID and address.
func (rs *RaftStorage) GetLeader() (string, string) {
	leaderAddr, leaderID := rs.raft.LeaderWithID()
	return string(leaderID), string(leaderAddr)
}

// GetPeers returns the list of peer node IDs in the Raft cluster.
func (rs *RaftStorage) GetPeers() ([]string, error) {
	future := rs.raft.GetConfiguration()
	if err := future.Error(); err != nil {
		return nil, fmt.Errorf("failed to get raft configuration: %w", err)
	}

	var peers []string
	for _, server := range future.Configuration().Servers {
		peers = append(peers, string(server.ID))
	}

	return peers, nil
}

// ForceRecoverCluster performs emergency cluster recovery for split-brain scenarios.
// WARNING: This should only be used when quorum is lost and manual intervention is required.
func (rs *RaftStorage) ForceRecoverCluster(nodeID, address string) error {
	rs.logger.Warn("Attempting emergency cluster recovery",
		domain.Field{Key: "node_id", Value: nodeID},
		domain.Field{Key: "address", Value: address})

	// Create new configuration with only the surviving node
	newConfig := raft.Configuration{
		Servers: []raft.Server{
			{
				ID:       raft.ServerID(nodeID),
				Address:  raft.ServerAddress(address),
				Suffrage: raft.Voter,
			},
		},
	}

	// Convert RaftStorageConfig to raft.Config for RecoverCluster
	raftConfig := createRaftConfig(rs.config, rs.logger)

	// Use RecoverCluster to force the new configuration
	if err := raft.RecoverCluster(raftConfig, rs.fsm, rs.logStore, rs.stableStore, rs.snapStore, rs.transport, newConfig); err != nil {
		return fmt.Errorf("emergency cluster recovery failed: %w", err)
	}

	rs.logger.Info("Emergency cluster recovery completed successfully",
		domain.Field{Key: "node_id", Value: nodeID},
		domain.Field{Key: "servers_count", Value: len(newConfig.Servers)})

	return nil
}

// raftFSM implements the Raft Finite State Machine.
type raftFSM struct {
	data       map[string]*storageItem
	watchChans map[string][]chan domain.StateEvent
	mu         sync.RWMutex
	logger     domain.Logger
}

// Apply applies a Raft log entry to the FSM.
func (f *raftFSM) Apply(log *raft.Log) interface{} {
	var cmd raftCommand
	if err := json.Unmarshal(log.Data, &cmd); err != nil {
		f.logger.Error("Failed to unmarshal raft command",
			domain.Field{Key: "error", Value: err.Error()})
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	switch cmd.Type {
	case raftCommandSet:
		item := &storageItem{
			value:     make([]byte, len(cmd.Value)),
			createdAt: cmd.Timestamp,
			ttl:       cmd.TTL,
		}
		copy(item.value, cmd.Value)
		f.data[cmd.Key] = item

		// Notify watchers
		event := domain.StateEvent{
			Type:      domain.StateEventSet,
			Key:       cmd.Key,
			Value:     cmd.Value,
			Timestamp: cmd.Timestamp,
			NodeID:    cmd.NodeID,
		}
		f.notifyWatchers(cmd.Key, event)

		f.logger.Debug("Applied SET command",
			domain.Field{Key: "key", Value: cmd.Key},
			domain.Field{Key: "node_id", Value: cmd.NodeID})

	case raftCommandDelete:
		delete(f.data, cmd.Key)

		// Notify watchers
		event := domain.StateEvent{
			Type:      domain.StateEventDelete,
			Key:       cmd.Key,
			Timestamp: cmd.Timestamp,
			NodeID:    cmd.NodeID,
		}
		f.notifyWatchers(cmd.Key, event)

		f.logger.Debug("Applied DELETE command",
			domain.Field{Key: "key", Value: cmd.Key},
			domain.Field{Key: "node_id", Value: cmd.NodeID})

	case raftCommandSetMultiple:
		for key, value := range cmd.Items {
			item := &storageItem{
				value:     make([]byte, len(value)),
				createdAt: cmd.Timestamp,
				ttl:       cmd.TTL,
			}
			copy(item.value, value)
			f.data[key] = item

			// Notify watchers for each key
			event := domain.StateEvent{
				Type:      domain.StateEventSet,
				Key:       key,
				Value:     value,
				Timestamp: cmd.Timestamp,
				NodeID:    cmd.NodeID,
			}
			f.notifyWatchers(key, event)
		}

		f.logger.Debug("Applied SET_MULTIPLE command",
			domain.Field{Key: "items_count", Value: len(cmd.Items)},
			domain.Field{Key: "node_id", Value: cmd.NodeID})

	default:
		err := fmt.Errorf("unknown command type: %d", cmd.Type)
		f.logger.Error("Unknown raft command type",
			domain.Field{Key: "command_type", Value: cmd.Type})
		return err
	}

	return nil
}

// Snapshot creates a snapshot of the current state.
func (f *raftFSM) Snapshot() (raft.FSMSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Create a deep copy of the data
	data := make(map[string]*storageItem)
	for k, v := range f.data {
		if !v.isExpired() {
			data[k] = &storageItem{
				value:     make([]byte, len(v.value)),
				createdAt: v.createdAt,
				ttl:       v.ttl,
			}
			copy(data[k].value, v.value)
		}
	}

	f.logger.Info("Created Raft snapshot",
		domain.Field{Key: "items_count", Value: len(data)})

	return &raftSnapshot{data: data}, nil
}

// Restore restores the FSM from a snapshot.
func (f *raftFSM) Restore(snapshot io.ReadCloser) error {
	defer snapshot.Close()

	var data map[string]*storageItem
	decoder := json.NewDecoder(snapshot)
	if err := decoder.Decode(&data); err != nil {
		return fmt.Errorf("failed to decode snapshot: %w", err)
	}

	f.mu.Lock()
	f.data = data
	f.mu.Unlock()

	f.logger.Info("Restored Raft state from snapshot",
		domain.Field{Key: "items_count", Value: len(data)})

	return nil
}

// get retrieves a value by key (internal method).
func (f *raftFSM) get(key string) ([]byte, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	item, exists := f.data[key]
	if !exists || item.isExpired() {
		if exists && item.isExpired() {
			// Clean up expired item asynchronously
			go func() {
				f.mu.Lock()
				delete(f.data, key)
				f.mu.Unlock()
			}()
		}
		return nil, false
	}

	// Return copy of value
	value := make([]byte, len(item.value))
	copy(value, item.value)
	return value, true
}

// addWatcher adds a watcher for a key prefix.
func (f *raftFSM) addWatcher(keyPrefix string, ch chan domain.StateEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watchChans[keyPrefix] = append(f.watchChans[keyPrefix], ch)
}

// removeWatcher removes a watcher for a key prefix.
func (f *raftFSM) removeWatcher(keyPrefix string, ch chan domain.StateEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()

	watchers := f.watchChans[keyPrefix]
	for i, watcher := range watchers {
		if watcher == ch {
			f.watchChans[keyPrefix] = append(watchers[:i], watchers[i+1:]...)
			break
		}
	}
	if len(f.watchChans[keyPrefix]) == 0 {
		delete(f.watchChans, keyPrefix)
	}
}

// closeAllWatchers closes all FSM watchers.
func (f *raftFSM) closeAllWatchers() {
	f.mu.Lock()
	defer f.mu.Unlock()

	for keyPrefix, watchers := range f.watchChans {
		for _, ch := range watchers {
			select {
			case <-ch:
			default:
				close(ch)
			}
		}
		delete(f.watchChans, keyPrefix)
	}
}

// notifyWatchers notifies all relevant watchers of a state change.
func (f *raftFSM) notifyWatchers(key string, event domain.StateEvent) {
	// Run notification asynchronously to avoid deadlocks
	go func() {
		f.mu.RLock()
		defer f.mu.RUnlock()

		for keyPrefix, watchers := range f.watchChans {
			if strings.HasPrefix(key, keyPrefix) {
				for _, ch := range watchers {
					select {
					case ch <- event:
					default:
						// Channel is full, skip
						f.logger.Warn("Watcher channel full, dropping event",
							domain.Field{Key: "key_prefix", Value: keyPrefix},
							domain.Field{Key: "event_key", Value: key})
					}
				}
			}
		}
	}()
}

// raftSnapshot implements the snapshot interface.
type raftSnapshot struct {
	data map[string]*storageItem
}

// Persist saves the snapshot to the given sink.
func (s *raftSnapshot) Persist(sink raft.SnapshotSink) error {
	encoder := json.NewEncoder(sink)
	if err := encoder.Encode(s.data); err != nil {
		_ = sink.Cancel()
		return fmt.Errorf("failed to encode snapshot: %w", err)
	}
	return sink.Close()
}

// Release is called when we are finished with the snapshot.
func (s *raftSnapshot) Release() {
	// No cleanup needed for our in-memory snapshot
}

// raftCommand represents a command to be applied to the FSM.
type raftCommand struct {
	Type      raftCommandType   `json:"type"`
	Key       string            `json:"key,omitempty"`
	Value     []byte            `json:"value,omitempty"`
	Items     map[string][]byte `json:"items,omitempty"`
	TTL       time.Duration     `json:"ttl"`
	NodeID    string            `json:"node_id"`
	Timestamp time.Time         `json:"timestamp"`
}

// raftCommandType represents the type of command.
type raftCommandType int

const (
	raftCommandSet raftCommandType = iota
	raftCommandDelete
	raftCommandSetMultiple
)
