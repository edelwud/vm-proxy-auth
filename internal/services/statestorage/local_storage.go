package statestorage

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// LocalStorage provides in-memory state storage for single-instance deployments.
type LocalStorage struct {
	data        sync.Map
	watches     map[string][]chan domain.StateEvent
	watchMu     sync.RWMutex
	nodeID      string
	closed      bool
	closeMu     sync.RWMutex
	closedChans map[chan domain.StateEvent]bool
	closedChMu  sync.RWMutex
}

// storageItem represents an item stored in local storage with TTL support.
type storageItem struct {
	value     []byte
	createdAt time.Time
	ttl       time.Duration
}

// isExpired checks if the storage item has expired.
func (si *storageItem) isExpired() bool {
	if si.ttl == 0 {
		return false // No expiration
	}
	return time.Since(si.createdAt) > si.ttl
}

// NewLocalStorage creates a new local storage instance.
func NewLocalStorage(nodeID string) *LocalStorage {
	return &LocalStorage{
		watches:     make(map[string][]chan domain.StateEvent),
		nodeID:      nodeID,
		closed:      false,
		closedChans: make(map[chan domain.StateEvent]bool),
	}
}

// Get retrieves a value by key.
func (ls *LocalStorage) Get(_ context.Context, key string) ([]byte, error) {
	if value, exists := ls.data.Load(key); exists {
		if item, ok := value.(*storageItem); ok && !item.isExpired() {
			// Create a copy to avoid data races
			result := make([]byte, len(item.value))
			copy(result, item.value)
			return result, nil
		}
		// Clean up expired item
		ls.data.Delete(key)
	}
	return nil, domain.ErrKeyNotFound
}

// Set stores a value with optional TTL.
func (ls *LocalStorage) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	// Create a copy to avoid data races
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	item := &storageItem{
		value:     valueCopy,
		createdAt: time.Now(),
		ttl:       ttl,
	}

	ls.data.Store(key, item)

	// Notify watchers
	ls.notifyWatchers(key, domain.StateEvent{
		Type:      domain.StateEventSet,
		Key:       key,
		Value:     valueCopy,
		Timestamp: time.Now(),
		NodeID:    ls.nodeID,
	})

	return nil
}

// Delete removes a key.
func (ls *LocalStorage) Delete(_ context.Context, key string) error {
	ls.data.Delete(key)

	// Notify watchers
	ls.notifyWatchers(key, domain.StateEvent{
		Type:      domain.StateEventDelete,
		Key:       key,
		Timestamp: time.Now(),
		NodeID:    ls.nodeID,
	})

	return nil
}

// GetMultiple retrieves multiple values efficiently.
func (ls *LocalStorage) GetMultiple(ctx context.Context, keys []string) (map[string][]byte, error) {
	result := make(map[string][]byte)

	for _, key := range keys {
		if value, err := ls.Get(ctx, key); err == nil {
			result[key] = value
		}
		// Ignore missing keys - don't return error for partial results
	}

	return result, nil
}

// SetMultiple stores multiple values efficiently.
func (ls *LocalStorage) SetMultiple(
	ctx context.Context,
	items map[string][]byte,
	ttl time.Duration,
) error {
	for key, value := range items {
		if err := ls.Set(ctx, key, value, ttl); err != nil {
			return err
		}
	}
	return nil
}

// Watch observes changes to keys matching the prefix.
func (ls *LocalStorage) Watch(
	ctx context.Context,
	keyPrefix string,
) (<-chan domain.StateEvent, error) {
	ch := make(chan domain.StateEvent, domain.DefaultWatchChannelBufferSize)

	ls.watchMu.Lock()
	ls.watches[keyPrefix] = append(ls.watches[keyPrefix], ch)
	ls.watchMu.Unlock()

	// Handle context cancellation
	go func() {
		<-ctx.Done()
		ls.removeWatcherAndClose(keyPrefix, ch)
	}()

	return ch, nil
}

// Close performs cleanup and graceful shutdown.
func (ls *LocalStorage) Close() error {
	ls.closeMu.Lock()
	defer ls.closeMu.Unlock()

	if ls.closed {
		return nil // Already closed
	}

	ls.closed = true

	// Close all watch channels
	ls.watchMu.Lock()
	ls.closedChMu.Lock()
	for prefix, channels := range ls.watches {
		for _, ch := range channels {
			if !ls.closedChans[ch] {
				ls.closedChans[ch] = true
				close(ch)
			}
		}
		delete(ls.watches, prefix)
	}
	// Clear closed channels map
	ls.closedChans = make(map[chan domain.StateEvent]bool)
	ls.closedChMu.Unlock()
	ls.watchMu.Unlock()

	// Clear all data
	ls.data.Range(func(key, _ any) bool {
		ls.data.Delete(key)
		return true
	})

	return nil
}

// Ping checks the health of the storage system.
func (ls *LocalStorage) Ping(_ context.Context) error {
	ls.closeMu.RLock()
	defer ls.closeMu.RUnlock()

	if ls.closed {
		return domain.ErrStorageUnavailable
	}
	return nil
}

// notifyWatchers sends events to all watchers with matching key prefixes.
func (ls *LocalStorage) notifyWatchers(key string, event domain.StateEvent) {
	ls.closeMu.RLock()
	if ls.closed {
		ls.closeMu.RUnlock()
		return
	}
	ls.closeMu.RUnlock()

	ls.watchMu.RLock()
	defer ls.watchMu.RUnlock()

	for prefix, channels := range ls.watches {
		if strings.HasPrefix(key, prefix) {
			for _, ch := range channels {
				select {
				case ch <- event:
					// Event sent successfully
				default:
					// Channel full, skip to avoid blocking
				}
			}
		}
	}
}

// removeWatcherAndClose removes a watcher and closes its channel safely.
func (ls *LocalStorage) removeWatcherAndClose(keyPrefix string, ch chan domain.StateEvent) {
	ls.watchMu.Lock()
	defer ls.watchMu.Unlock()

	channels, exists := ls.watches[keyPrefix]
	if !exists {
		return
	}

	ls.removeChannelFromWatchers(keyPrefix, ch, channels)
	ls.closeChannelSafely(ch)
}

func (ls *LocalStorage) removeChannelFromWatchers(
	keyPrefix string,
	targetCh chan domain.StateEvent,
	channels []chan domain.StateEvent,
) {
	for i, existingCh := range channels {
		if existingCh == targetCh {
			// Remove channel from slice
			ls.watches[keyPrefix] = append(channels[:i], channels[i+1:]...)
			break
		}
	}

	// If no more channels for this prefix, remove the prefix entry
	if len(ls.watches[keyPrefix]) == 0 {
		delete(ls.watches, keyPrefix)
	}
}

func (ls *LocalStorage) closeChannelSafely(ch chan domain.StateEvent) {
	ls.closedChMu.Lock()
	defer ls.closedChMu.Unlock()

	if !ls.closedChans[ch] {
		ls.closedChans[ch] = true
		close(ch)
	}
}
