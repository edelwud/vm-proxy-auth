package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// DragonflyDiscoveryConfig holds Dragonfly service discovery configuration.
type DragonflyDiscoveryConfig struct {
	Address            string            `json:"address"`
	Password           string            `json:"password"`
	Database           int               `json:"database"`
	KeyPrefix          string            `json:"key_prefix"`
	PeerRegistryKey    string            `json:"peer_registry_key"`
	BackendRegistryKey string            `json:"backend_registry_key"`
	UpdateInterval     time.Duration     `json:"update_interval"`
	TTL                time.Duration     `json:"ttl"`
	Metadata           map[string]string `json:"metadata"`
}

// DragonflyDiscovery implements ServiceDiscovery using Dragonfly database.
type DragonflyDiscovery struct {
	client  redis.UniversalClient
	config  DragonflyDiscoveryConfig
	logger  domain.Logger
	watchCh chan domain.ServiceDiscoveryEvent
	stopCh  chan struct{}
	nodeID  string
}

// NewDragonflyDiscovery creates a new Dragonfly service discovery client.
func NewDragonflyDiscovery(config DragonflyDiscoveryConfig, nodeID string, logger domain.Logger) (*DragonflyDiscovery, error) {
	client := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:    []string{config.Address},
		Password: config.Password,
		DB:       config.Database,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to connect to Dragonfly: %w", err)
	}

	// Set default values
	if config.KeyPrefix == "" {
		config.KeyPrefix = "vm-proxy-auth:"
	}
	if config.PeerRegistryKey == "" {
		config.PeerRegistryKey = "peers"
	}
	if config.BackendRegistryKey == "" {
		config.BackendRegistryKey = "backends"
	}
	if config.UpdateInterval == 0 {
		config.UpdateInterval = 30 * time.Second
	}
	if config.TTL == 0 {
		config.TTL = 2 * time.Minute
	}

	dd := &DragonflyDiscovery{
		client:  client,
		config:  config,
		logger:  logger.With(domain.Field{Key: "component", Value: "dragonfly.discovery"}),
		watchCh: make(chan domain.ServiceDiscoveryEvent, 100),
		stopCh:  make(chan struct{}),
		nodeID:  nodeID,
	}

	return dd, nil
}

// DiscoverPeers discovers peer nodes for Raft cluster formation.
func (dd *DragonflyDiscovery) DiscoverPeers(ctx context.Context) ([]domain.PeerInfo, error) {
	key := dd.config.KeyPrefix + dd.config.PeerRegistryKey

	// Get all peer entries from Dragonfly hash
	result, err := dd.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get peers from Dragonfly: %w", err)
	}

	var peers []domain.PeerInfo
	for nodeID, data := range result {
		var peerInfo domain.PeerInfo
		if err := json.Unmarshal([]byte(data), &peerInfo); err != nil {
			dd.logger.Warn("Failed to unmarshal peer data",
				domain.Field{Key: "node_id", Value: nodeID},
				domain.Field{Key: "error", Value: err.Error()})
			continue
		}

		// Check if peer data is recent (not stale)
		if time.Since(peerInfo.LastSeen) > dd.config.TTL {
			dd.logger.Debug("Peer data is stale, skipping",
				domain.Field{Key: "node_id", Value: nodeID},
				domain.Field{Key: "last_seen", Value: peerInfo.LastSeen})
			continue
		}

		peers = append(peers, peerInfo)
	}

	dd.logger.Debug("Discovered Raft peers from Dragonfly",
		domain.Field{Key: "peers_count", Value: len(peers)})

	return peers, nil
}

// DiscoverBackends discovers backend services (VictoriaMetrics instances).
func (dd *DragonflyDiscovery) DiscoverBackends(ctx context.Context) ([]domain.BackendInfo, error) {
	key := dd.config.KeyPrefix + dd.config.BackendRegistryKey

	// Get all backend entries from Dragonfly hash
	result, err := dd.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get backends from Dragonfly: %w", err)
	}

	var backends []domain.BackendInfo
	for backendID, data := range result {
		var backendInfo domain.BackendInfo
		if err := json.Unmarshal([]byte(data), &backendInfo); err != nil {
			dd.logger.Warn("Failed to unmarshal backend data",
				domain.Field{Key: "backend_id", Value: backendID},
				domain.Field{Key: "error", Value: err.Error()})
			continue
		}

		// Check if backend data is recent (not stale)
		if time.Since(backendInfo.LastSeen) > dd.config.TTL {
			dd.logger.Debug("Backend data is stale, skipping",
				domain.Field{Key: "backend_id", Value: backendID},
				domain.Field{Key: "last_seen", Value: backendInfo.LastSeen})
			continue
		}

		backends = append(backends, backendInfo)
	}

	dd.logger.Debug("Discovered backend services from Dragonfly",
		domain.Field{Key: "backends_count", Value: len(backends)})

	return backends, nil
}

// Watch monitors changes in service topology using Dragonfly keyspace notifications.
func (dd *DragonflyDiscovery) Watch(ctx context.Context) (<-chan domain.ServiceDiscoveryEvent, error) {
	// Subscribe to keyspace notifications for our registry keys
	peerPattern := fmt.Sprintf("__keyspace@%d__:%s%s", dd.config.Database, dd.config.KeyPrefix, dd.config.PeerRegistryKey)
	backendPattern := fmt.Sprintf("__keyspace@%d__:%s%s", dd.config.Database, dd.config.KeyPrefix, dd.config.BackendRegistryKey)

	pubsub := dd.client.PSubscribe(ctx, peerPattern, backendPattern)

	go dd.processKeyspaceNotifications(ctx, pubsub)

	dd.logger.Info("Started Dragonfly service discovery watch",
		domain.Field{Key: "peer_pattern", Value: peerPattern},
		domain.Field{Key: "backend_pattern", Value: backendPattern})

	return dd.watchCh, nil
}

// processKeyspaceNotifications processes keyspace notifications from Dragonfly.
func (dd *DragonflyDiscovery) processKeyspaceNotifications(ctx context.Context, pubsub *redis.PubSub) {
	defer pubsub.Close()

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case <-dd.stopCh:
			return
		case msg := <-ch:
			dd.handleKeyspaceNotification(ctx, msg)
		}
	}
}

// handleKeyspaceNotification handles individual keyspace notifications.
func (dd *DragonflyDiscovery) handleKeyspaceNotification(ctx context.Context, msg *redis.Message) {
	// Parse keyspace notification
	// Format: __keyspace@DB__:key operation
	parts := strings.Split(msg.Channel, ":")
	if len(parts) < 2 {
		return
	}

	key := strings.Join(parts[1:], ":")
	operation := msg.Payload

	dd.logger.Debug("Received keyspace notification",
		domain.Field{Key: "key", Value: key},
		domain.Field{Key: "operation", Value: operation})

	// Determine if this is a peer or backend change
	isPeerChange := strings.HasSuffix(key, dd.config.PeerRegistryKey)
	isBackendChange := strings.HasSuffix(key, dd.config.BackendRegistryKey)

	if !isPeerChange && !isBackendChange {
		return
	}

	// For hash operations (hset, hdel), we need to get the updated data
	if operation == "hset" || operation == "hdel" {
		if isPeerChange {
			dd.handlePeerChange(ctx, operation)
		} else if isBackendChange {
			dd.handleBackendChange(ctx, operation)
		}
	}
}

// handlePeerChange handles peer registry changes.
func (dd *DragonflyDiscovery) handlePeerChange(ctx context.Context, operation string) {
	peers, err := dd.DiscoverPeers(ctx)
	if err != nil {
		dd.logger.Error("Failed to discover peers after change",
			domain.Field{Key: "error", Value: err.Error()})
		return
	}

	eventType := domain.ServiceDiscoveryNodeUpdated
	if operation == "hdel" {
		eventType = domain.ServiceDiscoveryNodeLeft
	} else if operation == "hset" {
		eventType = domain.ServiceDiscoveryNodeJoined
	}

	// Send events for all current peers
	for _, peer := range peers {
		event := domain.ServiceDiscoveryEvent{
			Type:      eventType,
			PeerInfo:  &peer,
			Timestamp: time.Now(),
		}

		select {
		case dd.watchCh <- event:
		default:
			dd.logger.Warn("Service discovery event channel full",
				domain.Field{Key: "event_type", Value: string(eventType)},
				domain.Field{Key: "node_id", Value: peer.NodeID})
		}
	}
}

// handleBackendChange handles backend registry changes.
func (dd *DragonflyDiscovery) handleBackendChange(ctx context.Context, operation string) {
	backends, err := dd.DiscoverBackends(ctx)
	if err != nil {
		dd.logger.Error("Failed to discover backends after change",
			domain.Field{Key: "error", Value: err.Error()})
		return
	}

	eventType := domain.ServiceDiscoveryBackendUpdated
	if operation == "hdel" {
		eventType = domain.ServiceDiscoveryBackendRemoved
	} else if operation == "hset" {
		eventType = domain.ServiceDiscoveryBackendAdded
	}

	// Send events for all current backends
	for _, backend := range backends {
		event := domain.ServiceDiscoveryEvent{
			Type:      eventType,
			Backend:   &backend,
			Timestamp: time.Now(),
		}

		select {
		case dd.watchCh <- event:
		default:
			dd.logger.Warn("Service discovery event channel full",
				domain.Field{Key: "event_type", Value: string(eventType)},
				domain.Field{Key: "backend_url", Value: backend.URL})
		}
	}
}

// RegisterSelf registers this node in the service discovery system.
func (dd *DragonflyDiscovery) RegisterSelf(ctx context.Context, nodeInfo domain.NodeInfo) error {
	key := dd.config.KeyPrefix + dd.config.PeerRegistryKey

	// Prepare peer info for registration
	peerInfo := domain.PeerInfo{
		NodeID:      nodeInfo.NodeID,
		Address:     nodeInfo.Address,
		RaftAddress: nodeInfo.RaftAddress,
		Healthy:     true,
		LastSeen:    time.Now(),
		Metadata:    nodeInfo.Metadata,
	}

	// Add version and start time to metadata
	if peerInfo.Metadata == nil {
		peerInfo.Metadata = make(map[string]string)
	}
	peerInfo.Metadata["version"] = nodeInfo.Version
	peerInfo.Metadata["start_time"] = nodeInfo.StartTime.Format(time.RFC3339)

	data, err := json.Marshal(peerInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal peer info: %w", err)
	}

	// Register in Dragonfly hash with TTL
	pipe := dd.client.Pipeline()
	pipe.HSet(ctx, key, nodeInfo.NodeID, data)
	pipe.Expire(ctx, key, dd.config.TTL)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to register node in Dragonfly: %w", err)
	}

	dd.logger.Info("Registered node in Dragonfly",
		domain.Field{Key: "node_id", Value: nodeInfo.NodeID},
		domain.Field{Key: "address", Value: nodeInfo.Address},
		domain.Field{Key: "raft_address", Value: nodeInfo.RaftAddress})

	return nil
}

// RegisterBackend registers a backend service in Dragonfly.
func (dd *DragonflyDiscovery) RegisterBackend(ctx context.Context, backend domain.BackendInfo) error {
	key := dd.config.KeyPrefix + dd.config.BackendRegistryKey

	// Update last seen time
	backend.LastSeen = time.Now()

	// Generate backend ID from URL
	backendID := dd.generateBackendID(backend.URL)

	data, err := json.Marshal(backend)
	if err != nil {
		return fmt.Errorf("failed to marshal backend info: %w", err)
	}

	// Register in Dragonfly hash with TTL
	pipe := dd.client.Pipeline()
	pipe.HSet(ctx, key, backendID, data)
	pipe.Expire(ctx, key, dd.config.TTL)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to register backend in Dragonfly: %w", err)
	}

	dd.logger.Debug("Registered backend in Dragonfly",
		domain.Field{Key: "backend_id", Value: backendID},
		domain.Field{Key: "url", Value: backend.URL},
		domain.Field{Key: "weight", Value: backend.Weight})

	return nil
}

// UnregisterSelf removes this node from the service discovery system.
func (dd *DragonflyDiscovery) UnregisterSelf(ctx context.Context) error {
	key := dd.config.KeyPrefix + dd.config.PeerRegistryKey

	_, err := dd.client.HDel(ctx, key, dd.nodeID).Result()
	if err != nil {
		return fmt.Errorf("failed to unregister node from Dragonfly: %w", err)
	}

	dd.logger.Info("Unregistered node from Dragonfly",
		domain.Field{Key: "node_id", Value: dd.nodeID})

	return nil
}

// UnregisterBackend removes a backend service from Dragonfly.
func (dd *DragonflyDiscovery) UnregisterBackend(ctx context.Context, backendURL string) error {
	key := dd.config.KeyPrefix + dd.config.BackendRegistryKey
	backendID := dd.generateBackendID(backendURL)

	_, err := dd.client.HDel(ctx, key, backendID).Result()
	if err != nil {
		return fmt.Errorf("failed to unregister backend from Dragonfly: %w", err)
	}

	dd.logger.Debug("Unregistered backend from Dragonfly",
		domain.Field{Key: "backend_id", Value: backendID},
		domain.Field{Key: "url", Value: backendURL})

	return nil
}

// StartPeriodicRegistration starts periodic self-registration to maintain presence.
func (dd *DragonflyDiscovery) StartPeriodicRegistration(ctx context.Context, nodeInfo domain.NodeInfo) {
	ticker := time.NewTicker(dd.config.UpdateInterval)
	defer ticker.Stop()

	// Initial registration
	if err := dd.RegisterSelf(ctx, nodeInfo); err != nil {
		dd.logger.Error("Failed initial self-registration",
			domain.Field{Key: "error", Value: err.Error()})
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-dd.stopCh:
			return
		case <-ticker.C:
			// Update last seen time
			nodeInfo.Metadata["last_heartbeat"] = time.Now().Format(time.RFC3339)

			if err := dd.RegisterSelf(ctx, nodeInfo); err != nil {
				dd.logger.Error("Failed periodic self-registration",
					domain.Field{Key: "error", Value: err.Error()})
			}
		}
	}
}

// CleanupStaleEntries removes stale entries from the registry.
func (dd *DragonflyDiscovery) CleanupStaleEntries(ctx context.Context) error {
	// Clean up stale peers
	if err := dd.cleanupStalePeers(ctx); err != nil {
		dd.logger.Error("Failed to cleanup stale peers",
			domain.Field{Key: "error", Value: err.Error()})
	}

	// Clean up stale backends
	if err := dd.cleanupStaleBackends(ctx); err != nil {
		dd.logger.Error("Failed to cleanup stale backends",
			domain.Field{Key: "error", Value: err.Error()})
	}

	return nil
}

// cleanupStalePeers removes stale peer entries.
func (dd *DragonflyDiscovery) cleanupStalePeers(ctx context.Context) error {
	key := dd.config.KeyPrefix + dd.config.PeerRegistryKey

	result, err := dd.client.HGetAll(ctx, key).Result()
	if err != nil {
		return err
	}

	var staleNodes []string
	for nodeID, data := range result {
		var peerInfo domain.PeerInfo
		if err := json.Unmarshal([]byte(data), &peerInfo); err != nil {
			staleNodes = append(staleNodes, nodeID)
			continue
		}

		if time.Since(peerInfo.LastSeen) > dd.config.TTL {
			staleNodes = append(staleNodes, nodeID)
		}
	}

	if len(staleNodes) > 0 {
		_, err := dd.client.HDel(ctx, key, staleNodes...).Result()
		if err != nil {
			return err
		}

		dd.logger.Info("Cleaned up stale peer entries",
			domain.Field{Key: "stale_count", Value: len(staleNodes)},
			domain.Field{Key: "stale_nodes", Value: fmt.Sprintf("%v", staleNodes)})
	}

	return nil
}

// cleanupStaleBackends removes stale backend entries.
func (dd *DragonflyDiscovery) cleanupStaleBackends(ctx context.Context) error {
	key := dd.config.KeyPrefix + dd.config.BackendRegistryKey

	result, err := dd.client.HGetAll(ctx, key).Result()
	if err != nil {
		return err
	}

	var staleBackends []string
	for backendID, data := range result {
		var backendInfo domain.BackendInfo
		if err := json.Unmarshal([]byte(data), &backendInfo); err != nil {
			staleBackends = append(staleBackends, backendID)
			continue
		}

		if time.Since(backendInfo.LastSeen) > dd.config.TTL {
			staleBackends = append(staleBackends, backendID)
		}
	}

	if len(staleBackends) > 0 {
		_, err := dd.client.HDel(ctx, key, staleBackends...).Result()
		if err != nil {
			return err
		}

		dd.logger.Info("Cleaned up stale backend entries",
			domain.Field{Key: "stale_count", Value: len(staleBackends)})
	}

	return nil
}

// Close performs cleanup and graceful shutdown.
func (dd *DragonflyDiscovery) Close() error {
	dd.logger.Info("Shutting down Dragonfly service discovery")

	// Unregister self before closing
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := dd.UnregisterSelf(ctx); err != nil {
		dd.logger.Warn("Failed to unregister self during shutdown",
			domain.Field{Key: "error", Value: err.Error()})
	}

	close(dd.stopCh)
	close(dd.watchCh)

	if err := dd.client.Close(); err != nil {
		return fmt.Errorf("failed to close Dragonfly client: %w", err)
	}

	return nil
}

// generateBackendID creates a consistent ID for a backend URL.
func (dd *DragonflyDiscovery) generateBackendID(url string) string {
	// Simple approach: use the URL as ID after cleaning
	id := strings.ReplaceAll(url, "://", "_")
	id = strings.ReplaceAll(id, ":", "_")
	id = strings.ReplaceAll(id, "/", "_")
	return id
}

// GetRegistryStats returns statistics about the service registry.
func (dd *DragonflyDiscovery) GetRegistryStats(ctx context.Context) (map[string]interface{}, error) {
	peerKey := dd.config.KeyPrefix + dd.config.PeerRegistryKey
	backendKey := dd.config.KeyPrefix + dd.config.BackendRegistryKey

	// Get counts
	peerCount, err := dd.client.HLen(ctx, peerKey).Result()
	if err != nil {
		peerCount = 0
	}

	backendCount, err := dd.client.HLen(ctx, backendKey).Result()
	if err != nil {
		backendCount = 0
	}

	// Get Dragonfly info
	info, err := dd.client.Info(ctx).Result()
	if err != nil {
		info = "unavailable"
	}

	return map[string]interface{}{
		"peer_count":     peerCount,
		"backend_count":  backendCount,
		"dragonfly_info": info,
		"registry_keys": map[string]string{
			"peers":    peerKey,
			"backends": backendKey,
		},
	}, nil
}
