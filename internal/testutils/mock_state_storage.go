package testutils

import (
	"context"
	"sync"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// MockStateStorage is a simple in-memory mock for testing StateStorage interface.
type MockStateStorage struct {
	data map[string][]byte
	mu   sync.RWMutex
}

// NewMockStateStorage creates a new mock state storage instance.
func NewMockStateStorage() *MockStateStorage {
	return &MockStateStorage{
		data: make(map[string][]byte),
	}
}

// Get retrieves a value by key.
func (m *MockStateStorage) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if value, exists := m.data[key]; exists {
		// Return a copy to avoid data races
		result := make([]byte, len(value))
		copy(result, value)
		return result, nil
	}
	return nil, domain.ErrKeyNotFound
}

// Set stores a value with optional TTL.
func (m *MockStateStorage) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Store a copy to avoid data races
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	m.data[key] = valueCopy
	return nil
}

// Delete removes a key.
func (m *MockStateStorage) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.data, key)
	return nil
}

// GetMultiple retrieves multiple values efficiently.
func (m *MockStateStorage) GetMultiple(_ context.Context, keys []string) (map[string][]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]byte)
	for _, key := range keys {
		if value, exists := m.data[key]; exists {
			// Return a copy to avoid data races
			valueCopy := make([]byte, len(value))
			copy(valueCopy, value)
			result[key] = valueCopy
		}
	}
	return result, nil
}

// SetMultiple stores multiple values efficiently.
func (m *MockStateStorage) SetMultiple(_ context.Context, items map[string][]byte, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, value := range items {
		// Store a copy to avoid data races
		valueCopy := make([]byte, len(value))
		copy(valueCopy, value)
		m.data[key] = valueCopy
	}
	return nil
}

// Watch observes changes to keys matching the prefix.
func (m *MockStateStorage) Watch(_ context.Context, _ string) (<-chan domain.StateEvent, error) {
	ch := make(chan domain.StateEvent)
	close(ch) // Return closed channel for simplicity in tests
	return ch, nil
}

// Close performs cleanup.
func (m *MockStateStorage) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear all data
	m.data = make(map[string][]byte)
	return nil
}

// Ping checks the health of the storage system.
func (m *MockStateStorage) Ping(_ context.Context) error {
	return nil
}

// Clear removes all data (useful for test cleanup).
func (m *MockStateStorage) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data = make(map[string][]byte)
}

// Size returns the number of stored items.
func (m *MockStateStorage) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.data)
}

// HasKey checks if a key exists.
func (m *MockStateStorage) HasKey(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.data[key]
	return exists
}
