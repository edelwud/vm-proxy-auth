package proxy_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/health"
	"github.com/edelwud/vm-proxy-auth/internal/services/proxy"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

// TestQueueIntegrationWithBackendFailures tests that requests are properly queued
// when backends are unavailable and processed when they recover.
//
//nolint:gocognit // Integration test with multiple phases is necessarily complex
func TestQueueIntegrationWithBackendFailures(t *testing.T) {
	// Create controllable mock backend
	var backendHealthy int32 = 1 // Start healthy
	var requestCount int32

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			if atomic.LoadInt32(&backendHealthy) == 1 {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}

		// Regular request - only succeed if healthy
		if atomic.LoadInt32(&backendHealthy) == 1 {
			atomic.AddInt32(&requestCount, 1)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status": "ok", "backend": "test"}`))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error": "backend unhealthy"}`))
		}
	}))
	defer backend.Close()

	// Create logger and metrics service
	logger := testutils.NewMockLogger()
	metricsService := &MockEnhancedMetricsService{}

	// Create enhanced service config with queueing enabled
	config := proxy.EnhancedServiceConfig{
		Backends: []proxy.BackendConfig{
			{URL: backend.URL, Weight: 1},
		},
		LoadBalancing: proxy.LoadBalancingConfig{
			Strategy: domain.LoadBalancingRoundRobin,
		},
		HealthCheck: health.CheckerConfig{
			CheckInterval:      200 * time.Millisecond,
			Timeout:            100 * time.Millisecond,
			HealthyThreshold:   1,
			UnhealthyThreshold: 1,
			HealthEndpoint:     "/health",
		},
		Queue: proxy.QueueConfig{
			MaxSize: 10,
			Timeout: 2 * time.Second,
		},
		Timeout:        5 * time.Second,
		MaxRetries:     2,
		RetryBackoff:   50 * time.Millisecond,
		EnableQueueing: true,
	}

	// Create and start service
	stateStorage := testutils.NewMockStateStorage()
	service, err := proxy.NewEnhancedService(config, logger, metricsService, stateStorage)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = service.Start(ctx)
	require.NoError(t, err)
	defer service.Close()

	// Wait for health checker to start
	time.Sleep(300 * time.Millisecond)

	// Test Phase 1: Normal operation with healthy backend
	t.Run("Phase1_HealthyBackend", func(t *testing.T) {
		req := createTestRequest("test-user", "query=up")

		response, fwdErr := service.Forward(ctx, req)
		require.NoError(t, fwdErr)
		assert.Equal(t, http.StatusOK, response.StatusCode)
		assert.Contains(t, string(response.Body), "backend")
	})

	// Test Phase 2: Backend becomes unhealthy - requests should be queued
	t.Run("Phase2_QueueWhenUnhealthy", func(t *testing.T) {
		// Mark backend as unhealthy
		atomic.StoreInt32(&backendHealthy, 0)

		// Wait for health checker to detect unhealthy state
		time.Sleep(400 * time.Millisecond)

		// Verify backend status is unhealthy
		status := service.GetBackendsStatus()
		require.Len(t, status, 1)
		assert.False(t, status[0].IsHealthy)

		// Create multiple requests that should be queued
		var wg sync.WaitGroup
		var responses []string
		var errors []error
		mu := &sync.Mutex{}

		for range 3 {
			wg.Add(1)
			go func() {
				defer wg.Done()

				req := createTestRequest("test-user", "query=up")

				// These should be queued since backend is unhealthy
				response, fwdErr := service.Forward(ctx, req)

				mu.Lock()
				if fwdErr != nil {
					errors = append(errors, fwdErr)
				} else {
					responses = append(responses, string(response.Body))
				}
				mu.Unlock()
			}()
		}

		wg.Wait()

		// All requests should fail or timeout since backend is unhealthy
		// and queueing will timeout waiting for healthy backend
		assert.True(t, len(errors) > 0 || len(responses) == 0,
			"Expected errors or no responses when backend is unhealthy")
	})

	// Test Phase 3: Backend recovers - queue should process
	t.Run("Phase3_QueueProcessingOnRecovery", func(t *testing.T) {
		// Mark backend as healthy again
		atomic.StoreInt32(&backendHealthy, 1)

		// Wait for health checker to detect recovery
		time.Sleep(400 * time.Millisecond)

		// Verify backend status is healthy
		status := service.GetBackendsStatus()
		require.Len(t, status, 1)
		assert.True(t, status[0].IsHealthy)

		// Now requests should process successfully
		req := createTestRequest("test-user", "query=up")

		response, fwdErr := service.Forward(ctx, req)
		require.NoError(t, fwdErr)
		assert.Equal(t, http.StatusOK, response.StatusCode)
		assert.Contains(t, string(response.Body), "backend")
	})

	// Test Phase 4: Queue overflow behavior
	t.Run("Phase4_QueueOverflow", func(t *testing.T) {
		// Mark backend as unhealthy again
		atomic.StoreInt32(&backendHealthy, 0)
		time.Sleep(400 * time.Millisecond)

		// Create more requests than queue capacity to test overflow
		var wg sync.WaitGroup
		var overflowErrors int32

		for range 15 { // More than queue maxSize of 10
			wg.Add(1)
			go func() {
				defer wg.Done()

				req := createTestRequest("test-user", "query=up")

				_, fwdErr := service.Forward(ctx, req)
				if fwdErr != nil && strings.Contains(fwdErr.Error(), "queue") {
					atomic.AddInt32(&overflowErrors, 1)
				}
			}()
		}

		wg.Wait()

		// Should have some queue overflow errors
		assert.Positive(t, int(atomic.LoadInt32(&overflowErrors)),
			"Expected some queue overflow errors when sending more requests than queue capacity")
	})
}

// TestQueueStats verifies that queue statistics are properly tracked.
func TestQueueStats(t *testing.T) {
	// Create a backend that's initially unhealthy
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer backend.Close()

	logger := testutils.NewMockLogger()
	metricsService := &MockEnhancedMetricsService{}

	config := proxy.EnhancedServiceConfig{
		Backends: []proxy.BackendConfig{
			{URL: backend.URL, Weight: 1},
		},
		LoadBalancing: proxy.LoadBalancingConfig{
			Strategy: domain.LoadBalancingRoundRobin,
		},
		HealthCheck: health.CheckerConfig{
			CheckInterval:      100 * time.Millisecond,
			Timeout:            50 * time.Millisecond,
			HealthyThreshold:   1,
			UnhealthyThreshold: 1,
			HealthEndpoint:     "/health",
		},
		Queue: proxy.QueueConfig{
			MaxSize: 5,
			Timeout: 200 * time.Millisecond,
		},
		Timeout:        1 * time.Second,
		MaxRetries:     1,
		RetryBackoff:   10 * time.Millisecond,
		EnableQueueing: true,
	}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxy.NewEnhancedService(config, logger, metricsService, stateStorage)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = service.Start(ctx)
	require.NoError(t, err)
	defer service.Close()

	// Wait for health checker to mark backend as unhealthy
	time.Sleep(200 * time.Millisecond)

	// Attempt request - should trigger queue operations
	req := createTestRequest("test-user", "query=up")

	// Error is expected since backend is unhealthy and queue will timeout
	_, _ = service.Forward(ctx, req)

	// Verify that queue stats are available
	queueStats := service.GetQueueStats()
	require.NotNil(t, queueStats, "Queue stats should be available when queueing is enabled")
	assert.Equal(t, 5, queueStats.MaxSize, "Queue max size should match configuration")
}
