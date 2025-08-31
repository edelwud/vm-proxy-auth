package loadbalancer

import (
	"context"
	"sync"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// LeastConnectionsBalancer implements least connections load balancing.
// Routes requests to the backend with the fewest active connections.
type LeastConnectionsBalancer struct {
	backends      []domain.Backend
	activeConns   []int32 // Number of active connections per backend
	totalRequests []int64 // Total requests sent to each backend for stats
	mu            sync.RWMutex
	logger        domain.Logger
	closed        bool
	closeMu       sync.RWMutex
}

// NewLeastConnectionsBalancer creates a new least connections load balancer.
func NewLeastConnectionsBalancer(backends []domain.Backend, logger domain.Logger) *LeastConnectionsBalancer {
	return &LeastConnectionsBalancer{
		backends:      backends,
		activeConns:   make([]int32, len(backends)),
		totalRequests: make([]int64, len(backends)),
		logger:        logger.With(domain.Field{Key: "component", Value: "least_connections_balancer"}),
		closed:        false,
	}
}

// NextBackend returns the backend with the least active connections.
func (lcb *LeastConnectionsBalancer) NextBackend(_ context.Context) (*domain.Backend, error) {
	lcb.closeMu.RLock()
	if lcb.closed {
		lcb.closeMu.RUnlock()
		return nil, domain.ErrNoHealthyBackends
	}
	lcb.closeMu.RUnlock()

	// Get healthy backends with their indices
	healthyIndices := lcb.getHealthyBackendIndices()
	if len(healthyIndices) == 0 {
		// Try fallback backends (rate-limited)
		fallbackIndices := lcb.getFallbackBackendIndices()
		if len(fallbackIndices) == 0 {
			lcb.logger.Warn("No healthy or fallback backends available")
			return nil, domain.ErrNoHealthyBackends
		}
		healthyIndices = fallbackIndices
	}

	// Fast path for single backend
	if len(healthyIndices) == 1 {
		idx := healthyIndices[0]
		backend := &lcb.backends[idx]

		// Increment connection count
		lcb.mu.Lock()
		lcb.activeConns[idx]++
		lcb.totalRequests[idx]++
		currentConns := lcb.activeConns[idx]
		lcb.mu.Unlock()

		lcb.logger.Debug("Selected single available backend",
			domain.Field{Key: "backend_url", Value: backend.URL},
			domain.Field{Key: "active_connections", Value: currentConns})
		return backend, nil
	}

	// Find backend with least connections
	lcb.mu.Lock()
	selectedIdx := healthyIndices[0]
	minConnections := lcb.activeConns[selectedIdx]

	for _, idx := range healthyIndices[1:] {
		if lcb.activeConns[idx] < minConnections {
			minConnections = lcb.activeConns[idx]
			selectedIdx = idx
		}
	}

	// Increment connection count for selected backend
	lcb.activeConns[selectedIdx]++
	lcb.totalRequests[selectedIdx]++
	currentConns := lcb.activeConns[selectedIdx]
	lcb.mu.Unlock()

	backend := &lcb.backends[selectedIdx]

	lcb.logger.Debug("Selected backend with least connections",
		domain.Field{Key: "backend_url", Value: backend.URL},
		domain.Field{Key: "active_connections", Value: currentConns},
		domain.Field{Key: "healthy_count", Value: len(healthyIndices)})

	return backend, nil
}

// ReportResult reports the result of a request and decrements the connection count.
func (lcb *LeastConnectionsBalancer) ReportResult(backend *domain.Backend, err error, statusCode int) {
	lcb.mu.Lock()
	defer lcb.mu.Unlock()

	// Find the backend and decrement its connection count
	for i, b := range lcb.backends {
		if b.URL == backend.URL {
			if lcb.activeConns[i] > 0 {
				lcb.activeConns[i]--
			}

			if err != nil {
				lcb.logger.Debug("Backend request failed",
					domain.Field{Key: "backend_url", Value: backend.URL},
					domain.Field{Key: "active_connections", Value: lcb.activeConns[i]},
					domain.Field{Key: "total_requests", Value: lcb.totalRequests[i]},
					domain.Field{Key: "error", Value: err.Error()},
					domain.Field{Key: "status_code", Value: statusCode})
			} else {
				lcb.logger.Debug("Backend request succeeded",
					domain.Field{Key: "backend_url", Value: backend.URL},
					domain.Field{Key: "active_connections", Value: lcb.activeConns[i]},
					domain.Field{Key: "total_requests", Value: lcb.totalRequests[i]},
					domain.Field{Key: "status_code", Value: statusCode})
			}
			return
		}
	}

	lcb.logger.Warn("Attempted to report result for unknown backend",
		domain.Field{Key: "backend_url", Value: backend.URL})
}

// BackendsStatus returns the current status of all backends.
func (lcb *LeastConnectionsBalancer) BackendsStatus() []*domain.BackendStatus {
	lcb.mu.RLock()
	defer lcb.mu.RUnlock()

	status := make([]*domain.BackendStatus, len(lcb.backends))
	for i, backend := range lcb.backends {
		status[i] = &domain.BackendStatus{
			Backend:     backend,
			IsHealthy:   backend.State.IsAvailable(),
			ActiveConns: lcb.activeConns[i],
			LastCheck:   time.Now(),
			ErrorCount:  0, // Would need separate error tracking for this
		}
	}

	return status
}

// Close performs cleanup and graceful shutdown.
func (lcb *LeastConnectionsBalancer) Close() error {
	lcb.closeMu.Lock()
	defer lcb.closeMu.Unlock()

	if lcb.closed {
		return nil // Already closed
	}

	lcb.closed = true

	// Log final statistics
	lcb.mu.RLock()
	for i, backend := range lcb.backends {
		if lcb.totalRequests[i] > 0 {
			lcb.logger.Info("Backend final statistics",
				domain.Field{Key: "backend_url", Value: backend.URL},
				domain.Field{Key: "total_requests", Value: lcb.totalRequests[i]},
				domain.Field{Key: "remaining_connections", Value: lcb.activeConns[i]})
		}
	}
	lcb.mu.RUnlock()

	lcb.logger.Info("Least connections load balancer closed")
	return nil
}

// getHealthyBackendIndices returns indices of all backends that are healthy.
func (lcb *LeastConnectionsBalancer) getHealthyBackendIndices() []int {
	lcb.mu.RLock()
	defer lcb.mu.RUnlock()

	var healthy []int
	for i, backend := range lcb.backends {
		if backend.State.IsAvailable() {
			healthy = append(healthy, i)
		}
	}

	return healthy
}

// getFallbackBackendIndices returns indices of backends that can be used as fallbacks.
func (lcb *LeastConnectionsBalancer) getFallbackBackendIndices() []int {
	lcb.mu.RLock()
	defer lcb.mu.RUnlock()

	var fallback []int
	for i, backend := range lcb.backends {
		if backend.State.IsAvailableWithFallback() && !backend.State.IsAvailable() {
			fallback = append(fallback, i)
		}
	}

	return fallback
}

// UpdateBackendState updates the state of a specific backend.
func (lcb *LeastConnectionsBalancer) UpdateBackendState(backendURL string, newState domain.BackendState) {
	lcb.mu.Lock()
	defer lcb.mu.Unlock()

	for i, backend := range lcb.backends {
		if backend.URL == backendURL {
			oldState := backend.State
			lcb.backends[i].State = newState

			lcb.logger.Info("Backend state updated",
				domain.Field{Key: "backend_url", Value: backendURL},
				domain.Field{Key: "active_connections", Value: lcb.activeConns[i]},
				domain.Field{Key: "total_requests", Value: lcb.totalRequests[i]},
				domain.Field{Key: "old_state", Value: oldState.String()},
				domain.Field{Key: "new_state", Value: newState.String()})
			return
		}
	}

	lcb.logger.Warn("Attempted to update state for unknown backend",
		domain.Field{Key: "backend_url", Value: backendURL})
}

// GetBackendCount returns the total number of backends.
func (lcb *LeastConnectionsBalancer) GetBackendCount() int {
	lcb.mu.RLock()
	defer lcb.mu.RUnlock()
	return len(lcb.backends)
}

// GetHealthyBackendCount returns the number of healthy backends.
func (lcb *LeastConnectionsBalancer) GetHealthyBackendCount() int {
	return len(lcb.getHealthyBackendIndices())
}

// GetBackendConnections returns the number of active connections for a specific backend.
func (lcb *LeastConnectionsBalancer) GetBackendConnections(backendURL string) int {
	lcb.mu.RLock()
	defer lcb.mu.RUnlock()

	for i, backend := range lcb.backends {
		if backend.URL == backendURL {
			return int(lcb.activeConns[i])
		}
	}
	return -1 // Backend not found
}

// GetBackendTotalRequests returns the total number of requests sent to a specific backend.
func (lcb *LeastConnectionsBalancer) GetBackendTotalRequests(backendURL string) int64 {
	lcb.mu.RLock()
	defer lcb.mu.RUnlock()

	for i, backend := range lcb.backends {
		if backend.URL == backendURL {
			return lcb.totalRequests[i]
		}
	}
	return -1 // Backend not found
}

// ResetStats resets the statistics for all backends (useful for testing).
func (lcb *LeastConnectionsBalancer) ResetStats() {
	lcb.mu.Lock()
	defer lcb.mu.Unlock()

	for i := range lcb.activeConns {
		lcb.activeConns[i] = 0
		lcb.totalRequests[i] = 0
	}

	lcb.logger.Debug("Statistics reset for all backends")
}
