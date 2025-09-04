package memberlist

import (
	"fmt"
	"sync"

	"github.com/hashicorp/memberlist"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// RaftDelegate implements memberlist.Delegate and memberlist.EventDelegate
// for integration with Raft consensus.
type RaftDelegate struct {
	memberlistSvc *Service
	logger        domain.Logger
	raftMgr       domain.RaftManager

	mu              sync.RWMutex
	nodeMetadata    *NodeMetadata
	broadcasts      [][]byte
	pendingPeers    []PendingPeer   // Peers discovered but not yet added to Raft
	pendingRemovals map[string]bool // Peers that need to be removed when we become leader
	localNodeName   string          // Cache to avoid deadlock in NotifyJoin
}

// PendingPeer represents a peer discovered via memberlist but not yet added to Raft.
type PendingPeer struct {
	NodeID      string
	RaftAddress string
}

// NewRaftDelegate creates a new delegate for Raft-memberlist integration.
func NewRaftDelegate(memberlistSvc *Service, logger domain.Logger) *RaftDelegate {
	return &RaftDelegate{
		memberlistSvc:   memberlistSvc,
		logger:          logger.With(domain.Field{Key: domain.LogFieldComponent, Value: "memberlist.delegate"}),
		broadcasts:      make([][]byte, 0),
		pendingPeers:    make([]PendingPeer, 0),
		pendingRemovals: make(map[string]bool),
	}
}

// SetRaftManager sets the Raft manager for the delegate.
func (d *RaftDelegate) SetRaftManager(raftMgr domain.RaftManager) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.raftMgr = raftMgr
}

// SetNodeMetadata sets the local node metadata.
func (d *RaftDelegate) SetNodeMetadata(metadata *NodeMetadata) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nodeMetadata = metadata
}

// SetLocalNodeName caches the local node name to avoid deadlock.
func (d *RaftDelegate) SetLocalNodeName(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.localNodeName = name
}

// --- memberlist.Delegate interface implementation ---

// NodeMeta returns metadata about the local node.
func (d *RaftDelegate) NodeMeta(limit int) []byte {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.nodeMetadata == nil {
		d.logger.Debug("NodeMeta called but no metadata available",
			domain.Field{Key: "limit", Value: limit})
		return nil
	}

	data, err := d.nodeMetadata.Marshal()
	if err != nil {
		d.logger.Error("Failed to marshal node metadata",
			domain.Field{Key: "error", Value: err.Error()})
		return nil
	}

	if len(data) > limit {
		d.logger.Warn("Node metadata too large, truncating",
			domain.Field{Key: "size", Value: len(data)},
			domain.Field{Key: "limit", Value: limit})
		return data[:limit]
	}

	d.logger.Debug("NodeMeta returning metadata",
		domain.Field{Key: "limit", Value: limit},
		domain.Field{Key: "size", Value: len(data)},
		domain.Field{Key: "node_id", Value: d.nodeMetadata.NodeID},
		domain.Field{Key: "role", Value: d.nodeMetadata.Role})

	return data
}

// NotifyMsg handles incoming user messages.
func (d *RaftDelegate) NotifyMsg(msg []byte) {
	d.logger.Debug("Received memberlist message",
		domain.Field{Key: "size", Value: len(msg)})

	// Handle custom messages here if needed
	// For now, we don't use custom messages
}

// GetBroadcasts returns pending broadcasts.
func (d *RaftDelegate) GetBroadcasts(overhead, limit int) [][]byte {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.broadcasts) == 0 {
		return nil
	}

	// Calculate how many broadcasts we can send
	available := limit - overhead
	var result [][]byte
	var used int

	for i, broadcast := range d.broadcasts {
		if used+len(broadcast) > available {
			// Store remaining broadcasts for next call
			d.broadcasts = d.broadcasts[i:]
			break
		}
		result = append(result, broadcast)
		used += len(broadcast)
	}

	if len(result) == len(d.broadcasts) {
		// All broadcasts sent, clear the list
		d.broadcasts = d.broadcasts[:0]
	}

	return result
}

// LocalState returns the local node state.
func (d *RaftDelegate) LocalState(join bool) []byte {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.nodeMetadata == nil {
		d.logger.Debug("Returning local state: no metadata available",
			domain.Field{Key: "join", Value: join})
		return nil
	}

	data, err := d.nodeMetadata.Marshal()
	if err != nil {
		d.logger.Error("Failed to marshal local state",
			domain.Field{Key: "error", Value: err.Error()})
		return nil
	}

	d.logger.Debug("Returning local state with metadata",
		domain.Field{Key: "join", Value: join},
		domain.Field{Key: "size", Value: len(data)},
		domain.Field{Key: "node_id", Value: d.nodeMetadata.NodeID},
		domain.Field{Key: "role", Value: d.nodeMetadata.Role})

	return data
}

// MergeRemoteState merges remote node state.
func (d *RaftDelegate) MergeRemoteState(buf []byte, join bool) {
	d.logger.Debug("Merging remote state",
		domain.Field{Key: "join", Value: join},
		domain.Field{Key: "size", Value: len(buf)})

	// Parse remote metadata
	remoteMeta, err := UnmarshalNodeMetadata(buf)
	if err != nil {
		d.logger.Error("Failed to unmarshal remote state",
			domain.Field{Key: "error", Value: err.Error()})
		return
	}

	d.logger.Info("Merged remote node state",
		domain.Field{Key: "remote_node_id", Value: remoteMeta.NodeID},
		domain.Field{Key: "remote_role", Value: remoteMeta.Role})
}

// --- memberlist.EventDelegate interface implementation ---

// NotifyJoin handles node join events.
func (d *RaftDelegate) NotifyJoin(node *memberlist.Node) {
	d.logger.Debug("Node joined cluster",
		domain.Field{Key: "node_name", Value: node.Name},
		domain.Field{Key: "address", Value: fmt.Sprintf("%s:%d", node.Addr.String(), node.Port)})

	// Skip self-join events using cached local node name to avoid deadlock
	d.mu.RLock()
	localName := d.localNodeName
	d.mu.RUnlock()

	if localName != "" && localName == node.Name {
		d.logger.Debug("Skipping self-join event for Raft integration",
			domain.Field{Key: "local_node_name", Value: localName},
			domain.Field{Key: "joining_node_name", Value: node.Name})
		return
	}

	// Parse node metadata to get Raft information
	d.logger.Debug("Processing node metadata",
		domain.Field{Key: "node_name", Value: node.Name})

	if len(node.Meta) > 0 {
		metadata, err := UnmarshalNodeMetadata(node.Meta)
		if err != nil {
			d.logger.Error("Failed to parse node metadata",
				domain.Field{Key: "node_name", Value: node.Name},
				domain.Field{Key: "error", Value: err.Error()})
			return
		}

		d.logger.Debug("Parsed node metadata",
			domain.Field{Key: "node_id", Value: metadata.NodeID},
			domain.Field{Key: "role", Value: metadata.Role},
			domain.Field{Key: "raft_address", Value: metadata.RaftAddress})

		// Add to Raft cluster if it's a peer role
		if metadata.Role == domain.PeerRole && metadata.RaftAddress != "" {
			d.addRaftPeer(metadata.NodeID, metadata.RaftAddress)
		} else {
			d.logger.Debug("Node not eligible for Raft peer addition",
				domain.Field{Key: "role", Value: metadata.Role},
				domain.Field{Key: "raft_address", Value: metadata.RaftAddress},
				domain.Field{Key: "role_check", Value: metadata.Role == domain.PeerRole},
				domain.Field{Key: "address_check", Value: metadata.RaftAddress != ""})
		}
	} else {
		d.logger.Warn("Node has no metadata",
			domain.Field{Key: "node_name", Value: node.Name})
	}
}

// NotifyLeave handles node leave events.
func (d *RaftDelegate) NotifyLeave(node *memberlist.Node) {
	d.logger.Info("Node left cluster",
		domain.Field{Key: "node_name", Value: node.Name},
		domain.Field{Key: "node_address", Value: fmt.Sprintf("%s:%d", node.Addr.String(), node.Port)})

	// Parse node metadata to get Raft information
	if len(node.Meta) > 0 {
		metadata, err := UnmarshalNodeMetadata(node.Meta)
		if err != nil {
			d.logger.Error("Failed to parse leaving node metadata",
				domain.Field{Key: "node_name", Value: node.Name},
				domain.Field{Key: "error", Value: err.Error()})
			return
		}

		// Remove from Raft cluster if it's a peer role
		if metadata.Role == domain.PeerRole {
			d.removeRaftPeer(metadata.NodeID)
		}
	}
}

// NotifyUpdate handles node update events.
func (d *RaftDelegate) NotifyUpdate(node *memberlist.Node) {
	d.logger.Debug("Node updated",
		domain.Field{Key: "node_name", Value: node.Name},
		domain.Field{Key: "node_address", Value: fmt.Sprintf("%s:%d", node.Addr.String(), node.Port)})

	// Handle metadata updates if needed
	// This could be used for role changes, health status updates, etc.
}

// addRaftPeer adds a peer to the Raft cluster or caches it for later processing.
func (d *RaftDelegate) addRaftPeer(nodeID, raftAddress string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.raftMgr == nil {
		d.logger.Debug("Raft manager not set, cannot add peer",
			domain.Field{Key: "node_id", Value: nodeID})
		return
	}

	// If we're leader, add peer immediately
	if d.raftMgr.IsLeader() {
		d.logger.Info("Adding Raft peer (leader)",
			domain.Field{Key: "node_id", Value: nodeID},
			domain.Field{Key: "raft_address", Value: raftAddress})

		if err := d.raftMgr.AddVoter(nodeID, raftAddress); err != nil {
			d.logger.Error("Failed to add Raft peer",
				domain.Field{Key: "node_id", Value: nodeID},
				domain.Field{Key: "raft_address", Value: raftAddress},
				domain.Field{Key: "error", Value: err.Error()})
		} else {
			d.logger.Info("Successfully added Raft peer",
				domain.Field{Key: "node_id", Value: nodeID})
		}
		return
	}

	// Not leader, cache peer for later processing
	pending := PendingPeer{
		NodeID:      nodeID,
		RaftAddress: raftAddress,
	}

	// Check if peer already pending
	for _, p := range d.pendingPeers {
		if p.NodeID == nodeID {
			d.logger.Debug("Peer already in pending list",
				domain.Field{Key: "node_id", Value: nodeID})
			return
		}
	}

	d.pendingPeers = append(d.pendingPeers, pending)
	d.logger.Info("Caching peer for later addition (not leader)",
		domain.Field{Key: "node_id", Value: nodeID},
		domain.Field{Key: "raft_address", Value: raftAddress},
		domain.Field{Key: "pending_peers_count", Value: len(d.pendingPeers)})
}

// removeRaftPeer removes a peer from the Raft cluster.
func (d *RaftDelegate) removeRaftPeer(nodeID string) {
	d.mu.RLock()
	raftMgr := d.raftMgr
	d.mu.RUnlock()

	if raftMgr == nil {
		d.logger.Debug("Raft manager not set, cannot remove peer",
			domain.Field{Key: "node_id", Value: nodeID})
		return
	}

	// Handle 2-node cluster emergency recovery
	if d.handleEmergencyRecovery(raftMgr, nodeID) {
		return
	}

	// Handle non-leader scenario
	if d.handleNonLeaderRemoval(raftMgr, nodeID) {
		return
	}

	// Perform the actual removal as leader
	d.performPeerRemoval(raftMgr, nodeID)
}

// handleEmergencyRecovery handles 2-node cluster emergency recovery.
func (d *RaftDelegate) handleEmergencyRecovery(raftMgr domain.RaftManager, nodeID string) bool {
	peers, err := raftMgr.GetPeers()
	if err != nil || len(peers) != 2 {
		return false
	}

	d.logger.Info("2-node cluster detected, attempting emergency recovery",
		domain.Field{Key: "failed_node_id", Value: nodeID},
		domain.Field{Key: "current_peers", Value: len(peers)})

	survivingNodeID, survivingAddress := d.findSurvivingNode(peers, nodeID)
	if survivingNodeID == "" || survivingAddress == "" {
		return false
	}

	d.logger.Info("Performing emergency cluster recovery",
		domain.Field{Key: "surviving_node", Value: survivingNodeID},
		domain.Field{Key: "surviving_address", Value: survivingAddress})

	if recErr := raftMgr.ForceRecoverCluster(survivingNodeID, survivingAddress); recErr != nil {
		d.logger.Error("Failed emergency cluster recovery",
			domain.Field{Key: "error", Value: recErr.Error()})
		return false
	}

	d.logger.Info("Successfully recovered cluster",
		domain.Field{Key: "surviving_node", Value: survivingNodeID})
	return true
}

// findSurvivingNode determines the surviving node in a 2-node cluster.
func (d *RaftDelegate) findSurvivingNode(peers []string, failedNodeID string) (string, string) {
	for _, peerID := range peers {
		if peerID == failedNodeID {
			continue
		}

		address := d.getSurvivingNodeAddress(peerID)
		if address != "" {
			return peerID, address
		}
	}
	return "", ""
}

// getSurvivingNodeAddress returns the address for a given node ID by looking up member metadata.
func (d *RaftDelegate) getSurvivingNodeAddress(nodeID string) string {
	if d.memberlistSvc == nil || d.memberlistSvc.list == nil {
		d.logger.Debug("Cannot look up node address: memberlist service unavailable",
			domain.Field{Key: "node_id", Value: nodeID})
		return ""
	}

	// Search through current members for the node ID
	members := d.memberlistSvc.list.Members()
	for _, member := range members {
		if len(member.Meta) == 0 {
			continue
		}

		// Parse member metadata
		metadata, err := UnmarshalNodeMetadata(member.Meta)
		if err != nil {
			d.logger.Debug("Failed to parse member metadata for address lookup",
				domain.Field{Key: "member_name", Value: member.Name},
				domain.Field{Key: "error", Value: err.Error()})
			continue
		}

		// Check if this is the node we're looking for
		if metadata.NodeID == nodeID && metadata.RaftAddress != "" {
			d.logger.Debug("Found node address",
				domain.Field{Key: "node_id", Value: nodeID},
				domain.Field{Key: "raft_address", Value: metadata.RaftAddress})
			return metadata.RaftAddress
		}
	}

	d.logger.Debug("Node address not found in current members",
		domain.Field{Key: "node_id", Value: nodeID},
		domain.Field{Key: "members_count", Value: len(members)})
	return ""
}

// handleNonLeaderRemoval handles removal when not leader.
func (d *RaftDelegate) handleNonLeaderRemoval(raftMgr domain.RaftManager, nodeID string) bool {
	if raftMgr.IsLeader() {
		return false
	}

	d.mu.Lock()
	if d.pendingRemovals == nil {
		d.pendingRemovals = make(map[string]bool)
	}
	d.pendingRemovals[nodeID] = true
	d.mu.Unlock()

	d.logger.Debug("Not leader, caching peer for removal",
		domain.Field{Key: "node_id", Value: nodeID},
		domain.Field{Key: "pending_removals_count", Value: len(d.pendingRemovals)})
	return true
}

// performPeerRemoval performs the actual peer removal as leader.
func (d *RaftDelegate) performPeerRemoval(raftMgr domain.RaftManager, nodeID string) {
	d.logger.Info("Removing Raft peer",
		domain.Field{Key: "node_id", Value: nodeID})

	if removeErr := raftMgr.RemoveServer(nodeID); removeErr != nil {
		d.logger.Error("Failed to remove Raft peer",
			domain.Field{Key: "node_id", Value: nodeID},
			domain.Field{Key: "error", Value: removeErr.Error()})
	} else {
		d.logger.Info("Successfully removed Raft peer",
			domain.Field{Key: "node_id", Value: nodeID})
	}
}

// ProcessPendingPeers processes cached peers when this node becomes leader.
func (d *RaftDelegate) ProcessPendingPeers() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.raftMgr == nil || !d.raftMgr.IsLeader() {
		d.logger.Debug("Not leader or no Raft manager, cannot process pending peers")
		return
	}

	// Process pending peer additions
	if len(d.pendingPeers) > 0 {
		d.logger.Info("Processing pending peer additions (now leader)",
			domain.Field{Key: "pending_count", Value: len(d.pendingPeers)})

		processed := 0
		failed := 0

		for _, peer := range d.pendingPeers {
			d.logger.Info("Adding cached Raft peer",
				domain.Field{Key: "node_id", Value: peer.NodeID},
				domain.Field{Key: "raft_address", Value: peer.RaftAddress})

			if err := d.raftMgr.AddVoter(peer.NodeID, peer.RaftAddress); err != nil {
				d.logger.Error("Failed to add cached Raft peer",
					domain.Field{Key: "node_id", Value: peer.NodeID},
					domain.Field{Key: "raft_address", Value: peer.RaftAddress},
					domain.Field{Key: "error", Value: err.Error()})
				failed++
			} else {
				d.logger.Info("Successfully added cached Raft peer",
					domain.Field{Key: "node_id", Value: peer.NodeID})
				processed++
			}
		}

		// Clear pending peers after processing
		d.pendingPeers = d.pendingPeers[:0]

		d.logger.Info("Completed processing pending peer additions",
			domain.Field{Key: "processed", Value: processed},
			domain.Field{Key: "failed", Value: failed})
	}

	// Process pending peer removals
	if len(d.pendingRemovals) > 0 {
		d.logger.Info("Processing pending peer removals (now leader)",
			domain.Field{Key: "pending_removals_count", Value: len(d.pendingRemovals)})

		processed := 0
		failed := 0

		for nodeID := range d.pendingRemovals {
			d.logger.Info("Removing cached Raft peer",
				domain.Field{Key: "node_id", Value: nodeID})

			if err := d.raftMgr.RemoveServer(nodeID); err != nil {
				d.logger.Error("Failed to remove cached Raft peer",
					domain.Field{Key: "node_id", Value: nodeID},
					domain.Field{Key: "error", Value: err.Error()})
				failed++
			} else {
				d.logger.Info("Successfully removed cached Raft peer",
					domain.Field{Key: "node_id", Value: nodeID})
				processed++
			}
		}

		// Clear pending removals after processing
		d.pendingRemovals = make(map[string]bool)

		d.logger.Info("Completed processing pending peer removals",
			domain.Field{Key: "processed", Value: processed},
			domain.Field{Key: "failed", Value: failed})
	}
}

// QueueBroadcast queues a message for broadcast to the cluster.
func (d *RaftDelegate) QueueBroadcast(msg []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.broadcasts = append(d.broadcasts, msg)

	d.logger.Debug("Queued broadcast message",
		domain.Field{Key: "size", Value: len(msg)},
		domain.Field{Key: "queue_size", Value: len(d.broadcasts)})
}
