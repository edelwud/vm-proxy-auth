package proxy_test

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
	"github.com/edelwud/vm-proxy-auth/internal/services/proxy"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

// TestHealthCheckerLoadBalancerIntegration tests that health checker properly
// updates load balancer state when backends become unhealthy or recover.
func TestHealthCheckerLoadBalancerIntegration(t *testing.T) {
	// Create controllable mock backends
	var backend1Healthy, backend2Healthy bool = true, true
	var backend1Calls, backend2Calls int32
	var mu sync.RWMutex

	// Backend 1
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		healthy := backend1Healthy
		mu.RUnlock()

		if r.URL.Path == "/health" {
			if healthy {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}

		// Regular request
		if healthy {
			backend1Calls++
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"backend": "1"}`))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer backend1.Close()

	// Backend 2
	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		healthy := backend2Healthy
		mu.RUnlock()

		if r.URL.Path == "/health" {
			if healthy {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}

		// Regular request
		if healthy {
			backend2Calls++
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"backend": "2"}`))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer backend2.Close()

	// Create enhanced service with fast health checking
	config := proxy.EnhancedServiceConfig{
		Backends: []proxy.BackendConfig{
			{URL: backend1.URL, Weight: 1},
			{URL: backend2.URL, Weight: 1},
		},
		LoadBalancing: proxy.LoadBalancingConfig{
			Strategy: domain.LoadBalancingRoundRobin,
		},
		HealthCheck: health.CheckerConfig{
			CheckInterval:      50 * time.Millisecond, // Fast for testing
			Timeout:            1 * time.Second,
			HealthyThreshold:   1, // Quick recovery
			UnhealthyThreshold: 1, // Quick detection
			HealthEndpoint:     "/health",
		},
		Timeout:    5 * time.Second,
		MaxRetries: 1, // Don't retry on health integration test
	}

	logger := testutils.NewMockLogger()
	metrics := &MockEnhancedMetricsService{}

	service, err := proxy.NewEnhancedService(config, logger, metrics)
	require.NoError(t, err)
	defer service.Close()

	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)

	// Initially both backends should be healthy
	status := service.GetBackendsStatus()
	require.Len(t, status, 2)

	// Send some requests to verify both backends are used
	for i := 0; i < 10; i++ {
		req := createTestRequest("test-user", "/api/v1/query", "query=up")
		resp, err := service.Forward(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	// Both backends should have received requests
	assert.Greater(t, backend1Calls, int32(0), "Backend 1 should have received requests")
	assert.Greater(t, backend2Calls, int32(0), "Backend 2 should have received requests")

	t.Logf("Initial requests - Backend 1: %d, Backend 2: %d", backend1Calls, backend2Calls)

	// Make backend 1 unhealthy
	mu.Lock()
	backend1Healthy = false
	mu.Unlock()

	t.Log("Made backend 1 unhealthy, waiting for health check detection...")

	// Wait for health checker to detect unhealthy backend
	require.Eventually(t, func() bool {
		status := service.GetBackendsStatus()
		for _, s := range status {
			if s.Backend.URL == backend1.URL {
				return !s.IsHealthy
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "Backend 1 should be marked unhealthy")

	// Reset counters
	backend1Calls = 0
	backend2Calls = 0

	// Send more requests - all should go to backend 2 only
	for i := 0; i < 10; i++ {
		req := createTestRequest("test-user", "/api/v1/query", "query=up")
		resp, err := service.Forward(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	// Only backend 2 should receive requests now
	assert.Equal(t, int32(0), backend1Calls, "Backend 1 should not receive requests when unhealthy")
	assert.Greater(t, backend2Calls, int32(0), "Backend 2 should receive all requests")

	t.Logf("After backend 1 failure - Backend 1: %d, Backend 2: %d", backend1Calls, backend2Calls)

	// Make backend 1 healthy again
	mu.Lock()
	backend1Healthy = true
	mu.Unlock()

	t.Log("Made backend 1 healthy again, waiting for recovery...")

	// Wait for health checker to mark backend 1 as healthy again
	require.Eventually(t, func() bool {
		status := service.GetBackendsStatus()
		for _, s := range status {
			if s.Backend.URL == backend1.URL {
				return s.IsHealthy
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "Backend 1 should recover to healthy")

	// Reset counters
	backend1Calls = 0
	backend2Calls = 0

	// Send more requests - should be distributed again
	for i := 0; i < 20; i++ {
		req := createTestRequest("test-user", "/api/v1/query", "query=up")
		resp, err := service.Forward(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	// Both backends should receive requests again
	assert.Greater(t, backend1Calls, int32(0), "Backend 1 should receive requests after recovery")
	assert.Greater(t, backend2Calls, int32(0), "Backend 2 should continue receiving requests")

	t.Logf("After backend 1 recovery - Backend 1: %d, Backend 2: %d", backend1Calls, backend2Calls)

	// Final verification - check backend status
	status = service.GetBackendsStatus()
	require.Len(t, status, 2)

	for _, s := range status {
		assert.True(t, s.IsHealthy, "Both backends should be healthy at the end")
	}
}

// TestHealthCheckerMaintenanceMode tests that maintenance mode is properly
// handled by both health checker and load balancer.
func TestHealthCheckerMaintenanceMode(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer backend.Close()

	config := proxy.EnhancedServiceConfig{
		Backends: []proxy.BackendConfig{
			{URL: backend.URL, Weight: 1},
		},
		LoadBalancing: proxy.LoadBalancingConfig{
			Strategy: domain.LoadBalancingRoundRobin,
		},
		HealthCheck: health.CheckerConfig{
			CheckInterval:      50 * time.Millisecond,
			Timeout:            1 * time.Second,
			HealthyThreshold:   1,
			UnhealthyThreshold: 1,
			HealthEndpoint:     "/health",
		},
		Timeout: 5 * time.Second,
	}

	logger := testutils.NewMockLogger()
	metrics := &MockEnhancedMetricsService{}

	service, err := proxy.NewEnhancedService(config, logger, metrics)
	require.NoError(t, err)
	defer service.Close()

	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)

	// Initially should work
	req := createTestRequest("test-user", "/api/v1/query", "query=up")
	resp, err := service.Forward(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Enable maintenance mode
	err = service.SetMaintenanceMode(backend.URL, true)
	require.NoError(t, err)

	// Request should fail - no healthy backends
	req = createTestRequest("test-user", "/api/v1/query", "query=up")
	_, err = service.Forward(ctx, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no available backend")

	// Disable maintenance mode
	err = service.SetMaintenanceMode(backend.URL, false)
	require.NoError(t, err)

	// Should work again
	req = createTestRequest("test-user", "/api/v1/query", "query=up")
	resp, err = service.Forward(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestHealthCheckerWithWeightedLoadBalancer tests health checking integration
// with weighted round robin to ensure weights are preserved during health transitions.
func TestHealthCheckerWithWeightedLoadBalancer(t *testing.T) {
	var backend1Healthy, backend2Healthy bool = true, true
	var backend1Calls, backend2Calls int32
	var mu sync.RWMutex

	// Backend 1 (higher weight)
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		healthy := backend1Healthy
		mu.RUnlock()

		if r.URL.Path == "/health" {
			if healthy {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}

		if healthy {
			backend1Calls++
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"backend": "1"}`))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer backend1.Close()

	// Backend 2 (lower weight)
	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		healthy := backend2Healthy
		mu.RUnlock()

		if r.URL.Path == "/health" {
			if healthy {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}

		if healthy {
			backend2Calls++
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"backend": "2"}`))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer backend2.Close()

	config := proxy.EnhancedServiceConfig{
		Backends: []proxy.BackendConfig{
			{URL: backend1.URL, Weight: 3}, // Higher weight
			{URL: backend2.URL, Weight: 1}, // Lower weight
		},
		LoadBalancing: proxy.LoadBalancingConfig{
			Strategy: domain.LoadBalancingWeightedRoundRobin,
		},
		HealthCheck: health.CheckerConfig{
			CheckInterval:      50 * time.Millisecond,
			Timeout:            1 * time.Second,
			HealthyThreshold:   1,
			UnhealthyThreshold: 1,
			HealthEndpoint:     "/health",
		},
		Timeout: 5 * time.Second,
	}

	logger := testutils.NewMockLogger()
	metrics := &MockEnhancedMetricsService{}

	service, err := proxy.NewEnhancedService(config, logger, metrics)
	require.NoError(t, err)
	defer service.Close()

	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)

	// Send requests to establish baseline weighted distribution
	for i := 0; i < 40; i++ {
		req := createTestRequest("test-user", "/api/v1/query", "query=up")
		resp, err := service.Forward(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	// Verify weighted distribution (approximately 3:1 ratio)
	ratio := float64(backend1Calls) / float64(backend2Calls)
	assert.InDelta(t, 3.0, ratio, 1.0, "Should maintain weighted distribution before health issues")

	t.Logf("Initial weighted distribution - Backend 1 (weight=3): %d, Backend 2 (weight=1): %d, ratio: %.2f",
		backend1Calls, backend2Calls, ratio)

	// Make backend 1 (higher weight) unhealthy
	mu.Lock()
	backend1Healthy = false
	mu.Unlock()

	// Wait for health detection
	require.Eventually(t, func() bool {
		status := service.GetBackendsStatus()
		for _, s := range status {
			if s.Backend.URL == backend1.URL {
				return !s.IsHealthy
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond)

	// Reset counters
	backend1Calls = 0
	backend2Calls = 0

	// All requests should go to backend 2 now
	for i := 0; i < 10; i++ {
		req := createTestRequest("test-user", "/api/v1/query", "query=up")
		resp, err := service.Forward(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	assert.Equal(t, int32(0), backend1Calls, "Unhealthy backend should not receive requests")
	assert.Equal(t, int32(10), backend2Calls, "Healthy backend should receive all requests")

	// Restore backend 1 health
	mu.Lock()
	backend1Healthy = true
	mu.Unlock()

	// Wait for recovery
	require.Eventually(t, func() bool {
		status := service.GetBackendsStatus()
		for _, s := range status {
			if s.Backend.URL == backend1.URL {
				return s.IsHealthy
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond)

	// Reset and test weighted distribution restoration
	backend1Calls = 0
	backend2Calls = 0

	for i := 0; i < 40; i++ {
		req := createTestRequest("test-user", "/api/v1/query", "query=up")
		resp, err := service.Forward(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	// Should restore weighted distribution after recovery
	ratio = float64(backend1Calls) / float64(backend2Calls)
	assert.InDelta(t, 3.0, ratio, 1.0, "Should restore weighted distribution after recovery")

	t.Logf("Restored weighted distribution - Backend 1: %d, Backend 2: %d, ratio: %.2f",
		backend1Calls, backend2Calls, ratio)
}
