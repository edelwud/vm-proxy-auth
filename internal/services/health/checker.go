package health

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// CheckerConfig holds configuration for the health checker.
type CheckerConfig struct {
	CheckInterval     time.Duration `yaml:"check_interval" default:"30s"`
	Timeout           time.Duration `yaml:"timeout" default:"10s"`
	HealthyThreshold  int           `yaml:"healthy_threshold" default:"2"`
	UnhealthyThreshold int          `yaml:"unhealthy_threshold" default:"3"`
	HealthEndpoint    string        `yaml:"health_endpoint" default:"/health"`
}

// Checker monitors backend health using HTTP health checks.
type Checker struct {
	config        CheckerConfig
	backends      []domain.Backend
	backendStates map[string]*backendState
	statesMu      sync.RWMutex
	onStateChange func(backendURL string, oldState, newState domain.BackendState)
	httpClient    *http.Client
	logger        domain.Logger
	stopCh        chan struct{}
	stoppedCh     chan struct{}
	running       bool
	runningMu     sync.RWMutex
}

// backendState tracks health check state for a backend.
type backendState struct {
	consecutiveSuccesses int
	consecutiveFailures  int
	lastCheckTime        time.Time
	lastError            error
	totalChecks          int64
	totalFailures        int64
}

// NewChecker creates a new health checker.
func NewChecker(
	config CheckerConfig,
	backends []domain.Backend,
	onStateChange func(string, domain.BackendState, domain.BackendState),
	logger domain.Logger,
) *Checker {
	// Set defaults
	if config.CheckInterval == 0 {
		config.CheckInterval = 30 * time.Second
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.HealthyThreshold == 0 {
		config.HealthyThreshold = 2
	}
	if config.UnhealthyThreshold == 0 {
		config.UnhealthyThreshold = 3
	}
	if config.HealthEndpoint == "" {
		config.HealthEndpoint = "/health"
	}

	backendStates := make(map[string]*backendState)
	for _, backend := range backends {
		backendStates[backend.URL] = &backendState{
			lastCheckTime: time.Now(),
		}
	}

	return &Checker{
		config:        config,
		backends:      backends,
		backendStates: backendStates,
		onStateChange: onStateChange,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		logger:    logger.With(domain.Field{Key: "component", Value: "health_checker"}),
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}
}

// CheckHealth performs a health check on a specific backend.
func (hc *Checker) CheckHealth(ctx context.Context, backend *domain.Backend) error {
	// Skip health checks for manually maintained backends
	if backend.State == domain.BackendMaintenance {
		return nil
	}

	healthURL := backend.URL + hc.config.HealthEndpoint
	
	hc.logger.Debug("Performing health check",
		domain.Field{Key: "backend_url", Value: backend.URL},
		domain.Field{Key: "health_url", Value: healthURL})

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		hc.recordHealthCheck(backend.URL, false, time.Since(start), err)
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := hc.httpClient.Do(req)
	duration := time.Since(start)
	
	if err != nil {
		hc.recordHealthCheck(backend.URL, false, duration, err)
		return fmt.Errorf("health check request failed: %w", err)
	}
	defer resp.Body.Close()

	isHealthy := resp.StatusCode >= 200 && resp.StatusCode < 300
	
	if !isHealthy {
		err = fmt.Errorf("health check returned status: %d", resp.StatusCode)
		hc.recordHealthCheck(backend.URL, false, duration, err)
		return err
	}

	hc.recordHealthCheck(backend.URL, true, duration, nil)
	return nil
}

// recordHealthCheck records the result of a health check and updates backend state.
func (hc *Checker) recordHealthCheck(backendURL string, isHealthy bool, duration time.Duration, checkErr error) {
	hc.statesMu.Lock()
	defer hc.statesMu.Unlock()

	state, exists := hc.backendStates[backendURL]
	if !exists {
		hc.logger.Warn("Health check for unknown backend",
			domain.Field{Key: "backend_url", Value: backendURL})
		return
	}

	// Update counters
	state.totalChecks++
	state.lastCheckTime = time.Now()
	state.lastError = checkErr

	if isHealthy {
		state.consecutiveSuccesses++
		state.consecutiveFailures = 0
	} else {
		state.consecutiveFailures++
		state.consecutiveSuccesses = 0
		state.totalFailures++
	}

	// Determine new state based on thresholds
	var newState domain.BackendState
	var currentBackend *domain.Backend
	
	// Find current backend
	for i, backend := range hc.backends {
		if backend.URL == backendURL {
			currentBackend = &hc.backends[i]
			break
		}
	}

	if currentBackend == nil {
		hc.logger.Warn("Backend not found for health check",
			domain.Field{Key: "backend_url", Value: backendURL})
		return
	}

	// Don't change maintenance state via health checks
	if currentBackend.State == domain.BackendMaintenance {
		return
	}

	currentState := currentBackend.State

	// Determine new state
	if isHealthy && state.consecutiveSuccesses >= hc.config.HealthyThreshold {
		newState = domain.BackendHealthy
	} else if !isHealthy && state.consecutiveFailures >= hc.config.UnhealthyThreshold {
		newState = domain.BackendUnhealthy
	} else {
		// Keep current state
		newState = currentState
	}

	// Update backend state if changed
	if newState != currentState {
		// Find the index and update the backend
		for i, backend := range hc.backends {
			if backend.URL == backendURL {
				hc.backends[i].State = newState
				break
			}
		}

		hc.logger.Info("Backend state changed",
			domain.Field{Key: "backend_url", Value: backendURL},
			domain.Field{Key: "old_state", Value: currentState.String()},
			domain.Field{Key: "new_state", Value: newState.String()},
			domain.Field{Key: "consecutive_successes", Value: state.consecutiveSuccesses},
			domain.Field{Key: "consecutive_failures", Value: state.consecutiveFailures})

		// Notify callback
		if hc.onStateChange != nil {
			hc.onStateChange(backendURL, currentState, newState)
		}
	}

	hc.logger.Debug("Health check completed",
		domain.Field{Key: "backend_url", Value: backendURL},
		domain.Field{Key: "healthy", Value: isHealthy},
		domain.Field{Key: "duration", Value: duration},
		domain.Field{Key: "state", Value: newState.String()},
		domain.Field{Key: "consecutive_successes", Value: state.consecutiveSuccesses},
		domain.Field{Key: "consecutive_failures", Value: state.consecutiveFailures})
}

// findBackendIndex finds the index of a backend by URL.
func (hc *Checker) findBackendIndex(backendURL string) int {
	for i, backend := range hc.backends {
		if backend.URL == backendURL {
			return i
		}
	}
	return -1
}

// StartMonitoring begins continuous health monitoring.
func (hc *Checker) StartMonitoring(ctx context.Context) error {
	hc.runningMu.Lock()
	if hc.running {
		hc.runningMu.Unlock()
		return fmt.Errorf("health checker is already running")
	}
	hc.running = true
	hc.runningMu.Unlock()

	hc.logger.Info("Starting health monitoring",
		domain.Field{Key: "backends", Value: len(hc.backends)},
		domain.Field{Key: "check_interval", Value: hc.config.CheckInterval},
		domain.Field{Key: "timeout", Value: hc.config.Timeout})

	go hc.monitoringLoop(ctx)
	return nil
}

// monitoringLoop runs the continuous health check loop.
func (hc *Checker) monitoringLoop(ctx context.Context) {
	defer close(hc.stoppedCh)
	defer func() {
		hc.runningMu.Lock()
		hc.running = false
		hc.runningMu.Unlock()
	}()

	ticker := time.NewTicker(hc.config.CheckInterval)
	defer ticker.Stop()

	// Perform initial health checks
	hc.performHealthChecks(ctx)

	for {
		select {
		case <-ctx.Done():
			hc.logger.Info("Health monitoring stopped due to context cancellation")
			return
		case <-hc.stopCh:
			hc.logger.Info("Health monitoring stopped")
			return
		case <-ticker.C:
			hc.performHealthChecks(ctx)
		}
	}
}

// performHealthChecks performs health checks on all backends concurrently.
func (hc *Checker) performHealthChecks(ctx context.Context) {
	var wg sync.WaitGroup
	
	for _, backend := range hc.backends {
		if backend.State == domain.BackendMaintenance {
			continue // Skip maintenance backends
		}

		wg.Add(1)
		go func(b domain.Backend) {
			defer wg.Done()
			
			checkCtx, cancel := context.WithTimeout(ctx, hc.config.Timeout)
			defer cancel()
			
			err := hc.CheckHealth(checkCtx, &b)
			if err != nil {
				hc.logger.Debug("Health check failed",
					domain.Field{Key: "backend_url", Value: b.URL},
					domain.Field{Key: "error", Value: err.Error()})
			}
		}(backend)
	}

	wg.Wait()
}

// Stop stops health monitoring.
func (hc *Checker) Stop() error {
	hc.runningMu.RLock()
	if !hc.running {
		hc.runningMu.RUnlock()
		return nil // Already stopped
	}
	hc.runningMu.RUnlock()

	close(hc.stopCh)
	
	// Wait for monitoring loop to stop
	select {
	case <-hc.stoppedCh:
		// Monitoring stopped
	case <-time.After(5 * time.Second):
		hc.logger.Warn("Timeout waiting for health monitoring to stop")
	}

	hc.logger.Info("Health checker stopped")
	return nil
}

// GetStats returns health check statistics for all backends.
func (hc *Checker) GetStats() map[string]BackendHealthStats {
	hc.statesMu.RLock()
	defer hc.statesMu.RUnlock()

	stats := make(map[string]BackendHealthStats)
	for backendURL, state := range hc.backendStates {
		var backend *domain.Backend
		for _, b := range hc.backends {
			if b.URL == backendURL {
				backend = &b
				break
			}
		}

		if backend != nil {
			stats[backendURL] = BackendHealthStats{
				Backend:              *backend,
				ConsecutiveSuccesses: state.consecutiveSuccesses,
				ConsecutiveFailures:  state.consecutiveFailures,
				TotalChecks:         state.totalChecks,
				TotalFailures:       state.totalFailures,
				LastCheckTime:       state.lastCheckTime,
				LastError:           state.lastError,
				SuccessRate:         hc.calculateSuccessRate(state),
			}
		}
	}

	return stats
}

// calculateSuccessRate calculates the success rate for a backend.
func (hc *Checker) calculateSuccessRate(state *backendState) float64 {
	if state.totalChecks == 0 {
		return 0.0
	}
	successCount := state.totalChecks - state.totalFailures
	return float64(successCount) / float64(state.totalChecks) * 100
}

// BackendHealthStats represents health statistics for a backend.
type BackendHealthStats struct {
	Backend              domain.Backend `json:"backend"`
	ConsecutiveSuccesses int            `json:"consecutive_successes"`
	ConsecutiveFailures  int            `json:"consecutive_failures"`
	TotalChecks         int64          `json:"total_checks"`
	TotalFailures       int64          `json:"total_failures"`
	LastCheckTime       time.Time      `json:"last_check_time"`
	LastError           error          `json:"last_error,omitempty"`
	SuccessRate         float64        `json:"success_rate"`
}

// IsHealthy returns true if the backend is considered healthy.
func (bhs BackendHealthStats) IsHealthy() bool {
	return bhs.Backend.State.IsAvailable()
}