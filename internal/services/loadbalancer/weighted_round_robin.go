package loadbalancer

import (
	"context"
	"sync"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// WeightedRoundRobinBalancer implements weighted round-robin load balancing.
// Backends with higher weights receive proportionally more requests.
type WeightedRoundRobinBalancer struct {
	backends       []domain.Backend
	weights        []int
	currentWeights []int32 // atomic counters for thread safety
	totalWeight    int32
	mu             sync.RWMutex
	logger         domain.Logger
	closed         bool
	closeMu        sync.RWMutex
}

// NewWeightedRoundRobinBalancer creates a new weighted round-robin load balancer.
func NewWeightedRoundRobinBalancer(backends []domain.Backend, logger domain.Logger) *WeightedRoundRobinBalancer {
	weights := make([]int, len(backends))
	currentWeights := make([]int32, len(backends))
	totalWeight := int32(0)

	for i, backend := range backends {
		weight := backend.Weight
		if weight <= 0 {
			weight = 1 // Default weight
		}
		weights[i] = weight
		if totalWeight > (1<<31-1)-int32(weight) { //nolint:gosec // Check for int32 overflow
			logger.Error("Total weight too large for int32",
				domain.Field{Key: "total_weight", Value: totalWeight},
				domain.Field{Key: "adding_weight", Value: weight})
			weight = 1 // Fallback to prevent overflow
		}
		totalWeight += int32(weight) //nolint:gosec // already checked for overflow
	}

	return &WeightedRoundRobinBalancer{
		backends:       backends,
		weights:        weights,
		currentWeights: currentWeights,
		totalWeight:    totalWeight,
		logger:         logger.With(domain.Field{Key: "component", Value: "weighted_round_robin_balancer"}),
		closed:         false,
	}
}

// NextBackend returns the next available backend according to weighted round-robin strategy.
// Uses smooth weighted round-robin algorithm for better distribution.
func (wrr *WeightedRoundRobinBalancer) NextBackend(_ context.Context) (*domain.Backend, error) {
	wrr.closeMu.RLock()
	if wrr.closed {
		wrr.closeMu.RUnlock()
		return nil, domain.ErrNoHealthyBackends
	}
	wrr.closeMu.RUnlock()

	// Get healthy backends with their indices
	healthyIndices := wrr.getHealthyBackendIndices()
	if len(healthyIndices) == 0 {
		// Try fallback backends (rate-limited)
		fallbackIndices := wrr.getFallbackBackendIndices()
		if len(fallbackIndices) == 0 {
			wrr.logger.Warn("No healthy or fallback backends available")
			return nil, domain.ErrNoHealthyBackends
		}
		healthyIndices = fallbackIndices
	}

	// Fast path for single backend
	if len(healthyIndices) == 1 {
		idx := healthyIndices[0]
		backend := &wrr.backends[idx]
		wrr.logger.Debug("Selected single available backend",
			domain.Field{Key: "backend_url", Value: backend.URL},
			domain.Field{Key: "weight", Value: wrr.weights[idx]})
		return backend, nil
	}

	// Smooth weighted round-robin selection
	selectedIdx := wrr.selectByWeight(healthyIndices)
	backend := &wrr.backends[selectedIdx]

	wrr.logger.Debug("Selected backend using weighted round-robin",
		domain.Field{Key: "backend_url", Value: backend.URL},
		domain.Field{Key: "weight", Value: wrr.weights[selectedIdx]},
		domain.Field{Key: "healthy_count", Value: len(healthyIndices)})

	return backend, nil
}

// selectByWeight implements smooth weighted round-robin algorithm.
// This provides better distribution than simple random selection based on weights.
func (wrr *WeightedRoundRobinBalancer) selectByWeight(healthyIndices []int) int {
	wrr.mu.Lock()
	defer wrr.mu.Unlock()

	bestIdx := -1
	maxCurrentWeight := int32(-1)
	totalHealthyWeight := int32(0)

	// Calculate total weight of healthy backends
	for _, idx := range healthyIndices {
		weight := int32(wrr.weights[idx])          //nolint:gosec // overflow check
		if totalHealthyWeight > (1<<31-1)-weight { // Check for overflow
			wrr.logger.Error("Total healthy weight overflow prevented",
				domain.Field{Key: "total_weight", Value: totalHealthyWeight})
			weight = 1 // Fallback to prevent overflow
		}
		totalHealthyWeight += weight
	}

	// Find the backend with highest current weight after incrementing
	for _, idx := range healthyIndices {
		// Increase current weight by static weight
		weight := int32(wrr.weights[idx])               //nolint:gosec // overflow check
		if wrr.currentWeights[idx] > (1<<31-1)-weight { // Check for overflow
			wrr.logger.Error("Current weight overflow prevented",
				domain.Field{Key: "current_weight", Value: wrr.currentWeights[idx]})
			weight = 1 // Fallback to prevent overflow
		}
		wrr.currentWeights[idx] += weight

		if wrr.currentWeights[idx] > maxCurrentWeight {
			maxCurrentWeight = wrr.currentWeights[idx]
			bestIdx = idx
		}
	}

	// Decrease the selected backend's current weight by total weight of healthy backends
	if bestIdx != -1 {
		wrr.currentWeights[bestIdx] -= totalHealthyWeight
	}

	return bestIdx
}

// ReportResult reports the result of a request to a backend for health tracking.
func (wrr *WeightedRoundRobinBalancer) ReportResult(backend *domain.Backend, err error, statusCode int) {
	// Weighted round-robin balancer doesn't track individual results,
	// but we log for debugging purposes
	backendWeight := 1
	for i, b := range wrr.backends {
		if b.URL == backend.URL {
			backendWeight = wrr.weights[i]
			break
		}
	}

	if err != nil {
		wrr.logger.Debug("Backend request failed",
			domain.Field{Key: "backend_url", Value: backend.URL},
			domain.Field{Key: "weight", Value: backendWeight},
			domain.Field{Key: "error", Value: err.Error()},
			domain.Field{Key: "status_code", Value: statusCode})
	} else {
		wrr.logger.Debug("Backend request succeeded",
			domain.Field{Key: "backend_url", Value: backend.URL},
			domain.Field{Key: "weight", Value: backendWeight},
			domain.Field{Key: "status_code", Value: statusCode})
	}
}

// BackendsStatus returns the current status of all backends.
func (wrr *WeightedRoundRobinBalancer) BackendsStatus() []*domain.BackendStatus {
	wrr.mu.RLock()
	defer wrr.mu.RUnlock()

	status := make([]*domain.BackendStatus, len(wrr.backends))
	for i, backend := range wrr.backends {
		status[i] = &domain.BackendStatus{
			Backend:     backend,
			IsHealthy:   backend.State.IsAvailable(),
			ActiveConns: 0, // Weighted round-robin doesn't track connections
			LastCheck:   time.Now(),
			ErrorCount:  0, // Weighted round-robin doesn't track errors
		}
	}

	return status
}

// Close performs cleanup and graceful shutdown.
func (wrr *WeightedRoundRobinBalancer) Close() error {
	wrr.closeMu.Lock()
	defer wrr.closeMu.Unlock()

	if wrr.closed {
		return nil // Already closed
	}

	wrr.closed = true
	wrr.logger.Info("Weighted round-robin load balancer closed")
	return nil
}

// getHealthyBackendIndices returns indices of all backends that are healthy.
func (wrr *WeightedRoundRobinBalancer) getHealthyBackendIndices() []int {
	wrr.mu.RLock()
	defer wrr.mu.RUnlock()

	var healthy []int
	for i, backend := range wrr.backends {
		if backend.State.IsAvailable() {
			healthy = append(healthy, i)
		}
	}

	return healthy
}

// getFallbackBackendIndices returns indices of backends that can be used as fallbacks.
func (wrr *WeightedRoundRobinBalancer) getFallbackBackendIndices() []int {
	wrr.mu.RLock()
	defer wrr.mu.RUnlock()

	var fallback []int
	for i, backend := range wrr.backends {
		if backend.State.IsAvailableWithFallback() && !backend.State.IsAvailable() {
			fallback = append(fallback, i)
		}
	}

	return fallback
}

// UpdateBackendState updates the state of a specific backend.
func (wrr *WeightedRoundRobinBalancer) UpdateBackendState(backendURL string, newState domain.BackendState) {
	wrr.mu.Lock()
	defer wrr.mu.Unlock()

	for i, backend := range wrr.backends {
		if backend.URL == backendURL {
			oldState := backend.State
			wrr.backends[i].State = newState

			wrr.logger.Info("Backend state updated",
				domain.Field{Key: "backend_url", Value: backendURL},
				domain.Field{Key: "weight", Value: wrr.weights[i]},
				domain.Field{Key: "old_state", Value: oldState.String()},
				domain.Field{Key: "new_state", Value: newState.String()})
			return
		}
	}

	wrr.logger.Warn("Attempted to update state for unknown backend",
		domain.Field{Key: "backend_url", Value: backendURL})
}

// GetBackendCount returns the total number of backends.
func (wrr *WeightedRoundRobinBalancer) GetBackendCount() int {
	wrr.mu.RLock()
	defer wrr.mu.RUnlock()
	return len(wrr.backends)
}

// GetHealthyBackendCount returns the number of healthy backends.
func (wrr *WeightedRoundRobinBalancer) GetHealthyBackendCount() int {
	return len(wrr.getHealthyBackendIndices())
}

// GetTotalWeight returns the total weight of all backends.
func (wrr *WeightedRoundRobinBalancer) GetTotalWeight() int {
	return int(wrr.totalWeight)
}

// GetBackendWeight returns the weight of a specific backend.
func (wrr *WeightedRoundRobinBalancer) GetBackendWeight(backendURL string) int {
	wrr.mu.RLock()
	defer wrr.mu.RUnlock()

	for i, backend := range wrr.backends {
		if backend.URL == backendURL {
			return wrr.weights[i]
		}
	}
	return 0
}
