package loadbalancer

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// RoundRobinBalancer implements round-robin load balancing.
type RoundRobinBalancer struct {
	backends []domain.Backend
	current  int32 // atomic counter for thread safety
	mu       sync.RWMutex
	logger   domain.Logger
	closed   bool
	closeMu  sync.RWMutex
}

// NewRoundRobinBalancer creates a new round-robin load balancer.
func NewRoundRobinBalancer(backends []domain.Backend, logger domain.Logger) *RoundRobinBalancer {
	return &RoundRobinBalancer{
		backends: backends,
		current:  -1, // Start at -1 so first increment gives us 0
		logger:   logger.With(domain.Field{Key: "component", Value: "round_robin_balancer"}),
		closed:   false,
	}
}

// NextBackend returns the next available backend according to round-robin strategy.
func (rrb *RoundRobinBalancer) NextBackend(_ context.Context) (*domain.Backend, error) {
	rrb.closeMu.RLock()
	if rrb.closed {
		rrb.closeMu.RUnlock()
		return nil, domain.ErrNoHealthyBackends
	}
	rrb.closeMu.RUnlock()

	// Get healthy backends
	healthyBackends := rrb.getHealthyBackends()
	if len(healthyBackends) == 0 {
		// Try fallback backends (rate-limited)
		fallbackBackends := rrb.getFallbackBackends()
		if len(fallbackBackends) == 0 {
			rrb.logger.Warn("No healthy or fallback backends available")
			return nil, domain.ErrNoHealthyBackends
		}
		healthyBackends = fallbackBackends
	}

	// Fast path for single backend
	if len(healthyBackends) == 1 {
		backend := &healthyBackends[0]
		rrb.logger.Debug("Selected single available backend",
			domain.Field{Key: "backend_url", Value: backend.URL})
		return backend, nil
	}

	// Round-robin selection
	healthyCount := len(healthyBackends)
	if healthyCount > (1<<31 - 1) { // Check for int32 overflow
		rrb.logger.Error("Too many backends for int32 counter",
			domain.Field{Key: "backend_count", Value: healthyCount})
		return nil, domain.ErrNoHealthyBackends
	}
	index := atomic.AddInt32(&rrb.current, 1) % int32(healthyCount)
	backend := &healthyBackends[index]

	rrb.logger.Debug("Selected backend using round-robin",
		domain.Field{Key: "backend_url", Value: backend.URL},
		domain.Field{Key: "index", Value: index},
		domain.Field{Key: "healthy_count", Value: len(healthyBackends)})

	return backend, nil
}

// ReportResult reports the result of a request to a backend for health tracking.
func (rrb *RoundRobinBalancer) ReportResult(backend *domain.Backend, err error, statusCode int) {
	// Round-robin balancer doesn't track individual results,
	// but we log for debugging purposes
	if err != nil {
		rrb.logger.Debug("Backend request failed",
			domain.Field{Key: "backend_url", Value: backend.URL},
			domain.Field{Key: "error", Value: err.Error()},
			domain.Field{Key: "status_code", Value: statusCode})
	} else {
		rrb.logger.Debug("Backend request succeeded",
			domain.Field{Key: "backend_url", Value: backend.URL},
			domain.Field{Key: "status_code", Value: statusCode})
	}
}

// BackendsStatus returns the current status of all backends.
func (rrb *RoundRobinBalancer) BackendsStatus() []*domain.BackendStatus {
	rrb.mu.RLock()
	defer rrb.mu.RUnlock()

	status := make([]*domain.BackendStatus, len(rrb.backends))
	for i, backend := range rrb.backends {
		status[i] = &domain.BackendStatus{
			Backend:     backend,
			IsHealthy:   backend.State.IsAvailable(),
			ActiveConns: 0, // Round-robin doesn't track connections
			LastCheck:   time.Now(),
			ErrorCount:  0, // Round-robin doesn't track errors
		}
	}

	return status
}

// Close performs cleanup and graceful shutdown.
func (rrb *RoundRobinBalancer) Close() error {
	rrb.closeMu.Lock()
	defer rrb.closeMu.Unlock()

	if rrb.closed {
		return nil // Already closed
	}

	rrb.closed = true
	rrb.logger.Info("Round-robin load balancer closed")
	return nil
}

// getHealthyBackends returns all backends that are healthy.
func (rrb *RoundRobinBalancer) getHealthyBackends() []domain.Backend {
	rrb.mu.RLock()
	defer rrb.mu.RUnlock()

	var healthy []domain.Backend
	for _, backend := range rrb.backends {
		if backend.State.IsAvailable() {
			healthy = append(healthy, backend)
		}
	}

	return healthy
}

// getFallbackBackends returns backends that can be used as fallbacks (rate-limited).
func (rrb *RoundRobinBalancer) getFallbackBackends() []domain.Backend {
	rrb.mu.RLock()
	defer rrb.mu.RUnlock()

	var fallback []domain.Backend
	for _, backend := range rrb.backends {
		if backend.State.IsAvailableWithFallback() && !backend.State.IsAvailable() {
			fallback = append(fallback, backend)
		}
	}

	return fallback
}

// UpdateBackendState updates the state of a specific backend.
// This method is typically called by a health checker.
func (rrb *RoundRobinBalancer) UpdateBackendState(backendURL string, newState domain.BackendState) {
	rrb.mu.Lock()
	defer rrb.mu.Unlock()

	for i, backend := range rrb.backends {
		if backend.URL == backendURL {
			oldState := backend.State
			rrb.backends[i].State = newState

			rrb.logger.Info("Backend state updated",
				domain.Field{Key: "backend_url", Value: backendURL},
				domain.Field{Key: "old_state", Value: oldState.String()},
				domain.Field{Key: "new_state", Value: newState.String()})
			return
		}
	}

	rrb.logger.Warn("Attempted to update state for unknown backend",
		domain.Field{Key: "backend_url", Value: backendURL})
}

// GetBackendCount returns the total number of backends.
func (rrb *RoundRobinBalancer) GetBackendCount() int {
	rrb.mu.RLock()
	defer rrb.mu.RUnlock()
	return len(rrb.backends)
}

// GetHealthyBackendCount returns the number of healthy backends.
func (rrb *RoundRobinBalancer) GetHealthyBackendCount() int {
	return len(rrb.getHealthyBackends())
}
