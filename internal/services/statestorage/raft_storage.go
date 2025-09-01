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

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

const (
	raftMaxConnections    = 3
	raftApplyTimeout      = 10 * time.Second
	raftApplyTimeoutBulk  = 30 * time.Second
	raftWatchChannelSize  = 100
	raftMinPeerParts      = 2
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
}

// NewRaftStorage creates a new Raft-based state storage.
func NewRaftStorage(config RaftStorageConfig, nodeID string, logger domain.Logger) (*RaftStorage, error) {
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	raftConfig := raft.DefaultConfig()
	raftConfig.LocalID = raft.ServerID(config.NodeID)
	raftConfig.HeartbeatTimeout = config.HeartbeatTimeout
	raftConfig.ElectionTimeout = config.ElectionTimeout
	raftConfig.LeaderLeaseTimeout = config.LeaderLeaseTimeout
	raftConfig.CommitTimeout = config.CommitTimeout
	raftConfig.SnapshotThreshold = config.SnapshotThreshold
	raftConfig.TrailingLogs = config.TrailingLogs

	// Use built-in logger for Raft (we'll handle our own logging)
	// raftConfig.Logger will use default hclog

	// Create transport
	addr, err := net.ResolveTCPAddr("tcp", config.BindAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve bind address: %w", err)
	}

	transport, err := raft.NewTCPTransport(config.BindAddress, addr, raftMaxConnections, raftApplyTimeout, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	// Create BoltDB stores
	logStore, err := raftboltdb.NewBoltStore(filepath.Join(config.DataDir, "raft-log.db"))
	if err != nil {
		transport.Close()
		return nil, fmt.Errorf("failed to create log store: %w", err)
	}

	stableStore, err := raftboltdb.NewBoltStore(filepath.Join(config.DataDir, "raft-stable.db"))
	if err != nil {
		transport.Close()
		logStore.Close()
		return nil, fmt.Errorf("failed to create stable store: %w", err)
	}

	snapStore, err := raft.NewFileSnapshotStore(config.DataDir, config.SnapshotRetention, os.Stderr)
	if err != nil {
		transport.Close()
		logStore.Close()
		stableStore.Close()
		return nil, fmt.Errorf("failed to create snapshot store: %w", err)
	}

	// Create FSM
	fsm := &raftFSM{
		data:       make(map[string]*storageItem),
		watchChans: make(map[string][]chan domain.StateEvent),
		logger:     logger.With(domain.Field{Key: "component", Value: "raft.fsm"}),
	}

	// Create Raft instance
	r, err := raft.NewRaft(raftConfig, fsm, logStore, stableStore, snapStore, transport)
	if err != nil {
		transport.Close()
		logStore.Close()
		stableStore.Close()
		// snapStore doesn't need explicit close
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
		logger:      logger.With(domain.Field{Key: "component", Value: "raft.storage"}),
	}

	// Bootstrap cluster if needed
	if err := rs.bootstrapCluster(); err != nil {
		rs.Close()
		return nil, fmt.Errorf("failed to bootstrap cluster: %w", err)
	}

	rs.logger.Info("Raft storage initialized",
		domain.Field{Key: "node_id", Value: config.NodeID},
		domain.Field{Key: "bind_address", Value: config.BindAddress},
		domain.Field{Key: "data_dir", Value: config.DataDir},
		domain.Field{Key: "peers", Value: fmt.Sprintf("%v", config.Peers)})

	return rs, nil
}

// bootstrapCluster handles cluster initialization and joining.
func (rs *RaftStorage) bootstrapCluster() error {
	// Check if we have existing state
	hasState, err := raft.HasExistingState(rs.logStore, rs.stableStore, rs.snapStore)
	if err != nil {
		return fmt.Errorf("failed to check existing state: %w", err)
	}

	if hasState {
		rs.logger.Info("Found existing Raft state, joining existing cluster")
		return nil
	}

	// Bootstrap new cluster
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
			if len(parts) >= raftMinPeerParts {
				nodeID := parts[0]
				address := strings.Join(parts[1:], ":")
				servers = append(servers, raft.Server{
					ID:      raft.ServerID(nodeID),
					Address: raft.ServerAddress(address),
				})
			}
		}
	}

	// Bootstrap cluster with all known servers
	if len(servers) > 0 {
		configuration := raft.Configuration{Servers: servers}
		future := rs.raft.BootstrapCluster(configuration)
		if err := future.Error(); err != nil {
			rs.logger.Error("Failed to bootstrap cluster",
				domain.Field{Key: "error", Value: err.Error()},
				domain.Field{Key: "servers", Value: fmt.Sprintf("%+v", servers)})
			return fmt.Errorf("failed to bootstrap cluster: %w", err)
		}

		rs.logger.Info("Successfully bootstrapped Raft cluster",
			domain.Field{Key: "servers_count", Value: len(servers)},
			domain.Field{Key: "node_id", Value: rs.config.NodeID})
	}

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
			return fmt.Errorf("no leader available for write operation")
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

	future := rs.raft.Apply(data, raftApplyTimeout)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to apply raft command: %w", err)
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
			return fmt.Errorf("no leader available for delete operation")
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

	future := rs.raft.Apply(data, raftApplyTimeout)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to apply raft command: %w", err)
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

	future := rs.raft.Apply(data, raftApplyTimeoutBulk)
	if err := future.Error(); err != nil {
		return fmt.Errorf("failed to apply bulk raft command: %w", err)
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

	ch := make(chan domain.StateEvent, raftWatchChannelSize)

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
		if err := future.Error(); err != nil {
			rs.logger.Error("Error during Raft shutdown",
				domain.Field{Key: "error", Value: err.Error()})
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
