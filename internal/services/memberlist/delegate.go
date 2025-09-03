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
	raftMgr       RaftManager

	mu           sync.RWMutex
	nodeMetadata *NodeMetadata
	broadcasts   [][]byte
}

// NewRaftDelegate creates a new delegate for Raft-memberlist integration.
func NewRaftDelegate(memberlistSvc *Service, logger domain.Logger) *RaftDelegate {
	return &RaftDelegate{
		memberlistSvc: memberlistSvc,
		logger:        logger.With(domain.Field{Key: domain.LogFieldComponent, Value: "memberlist.delegate"}),
		broadcasts:    make([][]byte, 0),
	}
}

// SetRaftManager sets the Raft manager for the delegate.
func (d *RaftDelegate) SetRaftManager(raftMgr RaftManager) {
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

// --- memberlist.Delegate interface implementation ---

// NodeMeta returns metadata about the local node.
func (d *RaftDelegate) NodeMeta(limit int) []byte {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.nodeMetadata == nil {
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
		return nil
	}

	data, err := d.nodeMetadata.Marshal()
	if err != nil {
		d.logger.Error("Failed to marshal local state",
			domain.Field{Key: "error", Value: err.Error()})
		return nil
	}

	d.logger.Debug("Returning local state",
		domain.Field{Key: "join", Value: join},
		domain.Field{Key: "size", Value: len(data)})

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
	d.logger.Info("Node joined cluster",
		domain.Field{Key: "node_name", Value: node.Name},
		domain.Field{Key: "node_address", Value: fmt.Sprintf("%s:%d", node.Addr.String(), node.Port)})

	// Parse node metadata to get Raft information
	if len(node.Meta) > 0 {
		metadata, err := UnmarshalNodeMetadata(node.Meta)
		if err != nil {
			d.logger.Error("Failed to parse node metadata",
				domain.Field{Key: "node_name", Value: node.Name},
				domain.Field{Key: "error", Value: err.Error()})
			return
		}

		// Add to Raft cluster if it's a peer role
		if metadata.Role == "peer" && metadata.RaftAddress != "" {
			d.addRaftPeer(metadata.NodeID, metadata.RaftAddress)
		}
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
		if metadata.Role == "peer" {
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

// addRaftPeer adds a peer to the Raft cluster.
func (d *RaftDelegate) addRaftPeer(nodeID, raftAddress string) {
	d.mu.RLock()
	raftMgr := d.raftMgr
	d.mu.RUnlock()

	if raftMgr == nil {
		d.logger.Debug("Raft manager not set, cannot add peer",
			domain.Field{Key: "node_id", Value: nodeID})
		return
	}

	// Only leader can add peers
	if !raftMgr.IsLeader() {
		d.logger.Debug("Not leader, skipping peer addition",
			domain.Field{Key: "node_id", Value: nodeID})
		return
	}

	d.logger.Info("Adding Raft peer",
		domain.Field{Key: "node_id", Value: nodeID},
		domain.Field{Key: "raft_address", Value: raftAddress})

	if err := raftMgr.AddVoter(nodeID, raftAddress); err != nil {
		d.logger.Error("Failed to add Raft peer",
			domain.Field{Key: "node_id", Value: nodeID},
			domain.Field{Key: "raft_address", Value: raftAddress},
			domain.Field{Key: "error", Value: err.Error()})
	} else {
		d.logger.Info("Successfully added Raft peer",
			domain.Field{Key: "node_id", Value: nodeID})
	}
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

	// Only leader can remove peers
	if !raftMgr.IsLeader() {
		d.logger.Debug("Not leader, skipping peer removal",
			domain.Field{Key: "node_id", Value: nodeID})
		return
	}

	d.logger.Info("Removing Raft peer",
		domain.Field{Key: "node_id", Value: nodeID})

	if err := raftMgr.RemoveServer(nodeID); err != nil {
		d.logger.Error("Failed to remove Raft peer",
			domain.Field{Key: "node_id", Value: nodeID},
			domain.Field{Key: "error", Value: err.Error()})
	} else {
		d.logger.Info("Successfully removed Raft peer",
			domain.Field{Key: "node_id", Value: nodeID})
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
