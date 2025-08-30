package health_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/health"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestChecker_CheckHealth_Success(t *testing.T) {
	// Create mock health endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/health", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	}))
	defer server.Close()

	backends := []domain.Backend{
		{URL: server.URL, Weight: 1, State: domain.BackendHealthy},
	}

	config := health.CheckerConfig{
		Timeout:            time.Second,
		HealthyThreshold:   1,
		UnhealthyThreshold: 1,
		HealthEndpoint:     "/health",
	}

	logger := testutils.NewMockLogger()
	var stateChanges []stateChange
	var stateChangeMu sync.Mutex

	checker := health.NewChecker(config, backends, func(url string, oldState, newState domain.BackendState) {
		stateChangeMu.Lock()
		stateChanges = append(stateChanges, stateChange{url, oldState, newState})
		stateChangeMu.Unlock()
	}, logger)

	ctx := context.Background()
	err := checker.CheckHealth(ctx, &backends[0])
	require.NoError(t, err)

	// Verify stats
	stats := checker.GetStats()
	backendStats := stats[server.URL]
	assert.Equal(t, int64(1), backendStats.TotalChecks)
	assert.Equal(t, int64(0), backendStats.TotalFailures)
	assert.Equal(t, 1, backendStats.ConsecutiveSuccesses)
	assert.Equal(t, 0, backendStats.ConsecutiveFailures)
	assert.InEpsilon(t, 100.0, backendStats.SuccessRate, 0.01)
}

func TestChecker_CheckHealth_Failure(t *testing.T) {
	// Create mock health endpoint that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"unhealthy"}`))
	}))
	defer server.Close()

	backends := []domain.Backend{
		{URL: server.URL, Weight: 1, State: domain.BackendHealthy},
	}

	config := health.CheckerConfig{
		Timeout:            time.Second,
		HealthyThreshold:   1,
		UnhealthyThreshold: 1,
		HealthEndpoint:     "/health",
	}

	logger := testutils.NewMockLogger()
	checker := health.NewChecker(config, backends, nil, logger)

	ctx := context.Background()
	err := checker.CheckHealth(ctx, &backends[0])
	require.Error(t, err)
	assert.Contains(t, err.Error(), "health check returned status: 503")

	// Verify stats
	stats := checker.GetStats()
	backendStats := stats[server.URL]
	assert.Equal(t, int64(1), backendStats.TotalChecks)
	assert.Equal(t, int64(1), backendStats.TotalFailures)
	assert.Equal(t, 0, backendStats.ConsecutiveSuccesses)
	assert.Equal(t, 1, backendStats.ConsecutiveFailures)
	assert.InDelta(t, 0.0, backendStats.SuccessRate, 0.01)
}

func TestChecker_StateTransitions(t *testing.T) {
	// Create controllable mock server
	var healthyResponse bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if healthyResponse {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	backends := []domain.Backend{
		{URL: server.URL, Weight: 1, State: domain.BackendHealthy},
	}

	config := health.CheckerConfig{
		Timeout:            time.Second,
		HealthyThreshold:   2, // Need 2 consecutive successes
		UnhealthyThreshold: 2, // Need 2 consecutive failures
		HealthEndpoint:     "/health",
	}

	logger := testutils.NewMockLogger()
	var stateChanges []stateChange
	var stateChangeMu sync.Mutex

	checker := health.NewChecker(config, backends, func(url string, oldState, newState domain.BackendState) {
		stateChangeMu.Lock()
		stateChanges = append(stateChanges, stateChange{url, oldState, newState})
		stateChangeMu.Unlock()
	}, logger)

	ctx := context.Background()
	backend := &backends[0]

	// Start with unhealthy responses
	healthyResponse = false

	// First failure - should not change state (need 2 consecutive)
	checker.CheckHealth(ctx, backend)
	assert.Empty(t, stateChanges)

	// Second failure - should transition to unhealthy
	checker.CheckHealth(ctx, backend)
	assert.Len(t, stateChanges, 1)
	assert.Equal(t, domain.BackendHealthy, stateChanges[0].oldState)
	assert.Equal(t, domain.BackendUnhealthy, stateChanges[0].newState)

	// Now switch to healthy responses
	healthyResponse = true

	// First success - should not change state
	checker.CheckHealth(ctx, backend)
	assert.Len(t, stateChanges, 1) // No new state change

	// Second success - should transition to healthy
	checker.CheckHealth(ctx, backend)
	assert.Len(t, stateChanges, 2)
	assert.Equal(t, domain.BackendUnhealthy, stateChanges[1].oldState)
	assert.Equal(t, domain.BackendHealthy, stateChanges[1].newState)
}

func TestChecker_MaintenanceMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable) // Would normally be unhealthy
	}))
	defer server.Close()

	backends := []domain.Backend{
		{URL: server.URL, Weight: 1, State: domain.BackendMaintenance},
	}

	config := health.CheckerConfig{
		Timeout:            time.Second,
		HealthyThreshold:   1,
		UnhealthyThreshold: 1,
		HealthEndpoint:     "/health",
	}

	logger := testutils.NewMockLogger()
	var stateChanges []stateChange

	checker := health.NewChecker(config, backends, func(url string, oldState, newState domain.BackendState) {
		stateChanges = append(stateChanges, stateChange{url, oldState, newState})
	}, logger)

	ctx := context.Background()

	// Health check should not change maintenance state
	checker.CheckHealth(ctx, &backends[0])
	assert.Empty(t, stateChanges) // Should not change from maintenance
}

func TestChecker_ConcurrentChecks(t *testing.T) {
	// Create multiple mock servers
	servers := make([]*httptest.Server, 5)
	backends := make([]domain.Backend, 5)

	for i := range 5 {
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// Add small delay to simulate network latency
			time.Sleep(10 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer servers[i].Close()

		backends[i] = domain.Backend{
			URL:    servers[i].URL,
			Weight: 1,
			State:  domain.BackendHealthy,
		}
	}

	config := health.CheckerConfig{
		Timeout:            time.Second,
		HealthyThreshold:   1,
		UnhealthyThreshold: 1,
		HealthEndpoint:     "/health",
	}

	logger := testutils.NewMockLogger()
	checker := health.NewChecker(config, backends, nil, logger)

	ctx := context.Background()

	// Perform concurrent health checks
	start := time.Now()
	var wg sync.WaitGroup
	for _, backend := range backends {
		wg.Add(1)
		go func(b domain.Backend) {
			defer wg.Done()
			err := checker.CheckHealth(ctx, &b)
			assert.NoError(t, err)
		}(backend)
	}
	wg.Wait()
	duration := time.Since(start)

	// Should complete in much less time than sequential (5 * 10ms + overhead)
	assert.Less(t, duration, 100*time.Millisecond)

	// Verify all checks completed
	stats := checker.GetStats()
	assert.Len(t, stats, 5)

	for _, server := range servers {
		backendStats := stats[server.URL]
		assert.Equal(t, int64(1), backendStats.TotalChecks)
		assert.InEpsilon(t, 100.0, backendStats.SuccessRate, 0.01)
	}
}

func TestChecker_StartStopMonitoring(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	backends := []domain.Backend{
		{URL: server.URL, Weight: 1, State: domain.BackendHealthy},
	}

	config := health.CheckerConfig{
		CheckInterval:      50 * time.Millisecond, // Fast interval for testing
		Timeout:            time.Second,
		HealthyThreshold:   1,
		UnhealthyThreshold: 1,
		HealthEndpoint:     "/health",
	}

	logger := testutils.NewMockLogger()
	checker := health.NewChecker(config, backends, nil, logger)

	ctx := t.Context()

	// Start monitoring
	err := checker.StartMonitoring(ctx)
	require.NoError(t, err)

	// Should not be able to start twice
	err = checker.StartMonitoring(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Wait for a few checks to complete
	time.Sleep(150 * time.Millisecond)

	// Stop monitoring
	err = checker.Stop()
	require.NoError(t, err)

	// Should be able to stop multiple times
	err = checker.Stop()
	require.NoError(t, err)

	// Verify checks were performed
	stats := checker.GetStats()
	backendStats := stats[server.URL]
	assert.Greater(t, backendStats.TotalChecks, int64(1))
}

func TestChecker_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Add delay to allow context cancellation
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	backends := []domain.Backend{
		{URL: server.URL, Weight: 1, State: domain.BackendHealthy},
	}

	config := health.CheckerConfig{
		Timeout:            time.Second,
		HealthyThreshold:   1,
		UnhealthyThreshold: 1,
		HealthEndpoint:     "/health",
	}

	logger := testutils.NewMockLogger()
	checker := health.NewChecker(config, backends, nil, logger)

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Health check should be cancelled
	err := checker.CheckHealth(ctx, &backends[0])
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestChecker_DefaultConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/health", r.URL.Path) // Default endpoint
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	backends := []domain.Backend{
		{URL: server.URL, Weight: 1, State: domain.BackendHealthy},
	}

	// Use empty config to test defaults
	config := health.CheckerConfig{}

	logger := testutils.NewMockLogger()
	checker := health.NewChecker(config, backends, nil, logger)

	ctx := context.Background()
	err := checker.CheckHealth(ctx, &backends[0])
	require.NoError(t, err)
}

func TestChecker_CustomHealthEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/custom/healthz", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	backends := []domain.Backend{
		{URL: server.URL, Weight: 1, State: domain.BackendHealthy},
	}

	config := health.CheckerConfig{
		Timeout:        time.Second,
		HealthEndpoint: "/custom/healthz",
	}

	logger := testutils.NewMockLogger()
	checker := health.NewChecker(config, backends, nil, logger)

	ctx := context.Background()
	err := checker.CheckHealth(ctx, &backends[0])
	require.NoError(t, err)
}

// Helper type for tracking state changes.
type stateChange struct {
	url      string
	oldState domain.BackendState
	newState domain.BackendState
}

func BenchmarkChecker_CheckHealth(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	backends := []domain.Backend{
		{URL: server.URL, Weight: 1, State: domain.BackendHealthy},
	}

	config := health.CheckerConfig{
		Timeout:            time.Second,
		HealthyThreshold:   1,
		UnhealthyThreshold: 1,
		HealthEndpoint:     "/health",
	}

	logger := testutils.NewMockLogger()
	checker := health.NewChecker(config, backends, nil, logger)

	ctx := context.Background()
	backend := &backends[0]

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			err := checker.CheckHealth(ctx, backend)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
