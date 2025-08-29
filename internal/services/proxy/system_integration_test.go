package proxy_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// TestCompleteSystemIntegration tests the complete multiple upstream system
// including load balancing, health checking, request queuing, and metrics.
func TestCompleteSystemIntegration(t *testing.T) {
	// Create three mock VictoriaMetrics backends
	var backend1Calls, backend2Calls, backend3Calls int64
	var backend1Healthy, backend2Healthy, backend3Healthy = true, true, true

	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			if backend1Healthy {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}

		// Simulate VictoriaMetrics query response
		atomic.AddInt64(&backend1Calls, 1)
		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "vector",
				"result": []map[string]interface{}{
					{
						"metric": map[string]string{
							"__name__":      "up",
							"job":           "prometheus",
							"instance":      "localhost:9090",
							"vm_account_id": r.Header.Get("X-Prometheus-Tenant"),
						},
						"value": []interface{}{time.Now().Unix(), "1"},
					},
				},
			},
			"backend": "1",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer backend1.Close()

	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			if backend2Healthy {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}

		atomic.AddInt64(&backend2Calls, 1)
		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "vector",
				"result": []map[string]interface{}{
					{
						"metric": map[string]string{
							"__name__":      "up",
							"job":           "prometheus",
							"instance":      "localhost:9091",
							"vm_account_id": r.Header.Get("X-Prometheus-Tenant"),
						},
						"value": []interface{}{time.Now().Unix(), "1"},
					},
				},
			},
			"backend": "2",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer backend2.Close()

	backend3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			if backend3Healthy {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}

		atomic.AddInt64(&backend3Calls, 1)
		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "vector",
				"result": []map[string]interface{}{
					{
						"metric": map[string]string{
							"__name__":      "up",
							"job":           "prometheus",
							"instance":      "localhost:9092",
							"vm_account_id": r.Header.Get("X-Prometheus-Tenant"),
						},
						"value": []interface{}{time.Now().Unix(), "1"},
					},
				},
			},
			"backend": "3",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer backend3.Close()

	// Create comprehensive enhanced service configuration
	config := proxy.EnhancedServiceConfig{
		Backends: []proxy.BackendConfig{
			{URL: backend1.URL, Weight: 3}, // Primary backend
			{URL: backend2.URL, Weight: 2}, // Secondary backend
			{URL: backend3.URL, Weight: 1}, // Tertiary backend
		},
		LoadBalancing: proxy.LoadBalancingConfig{
			Strategy: domain.LoadBalancingWeightedRoundRobin,
		},
		HealthCheck: health.CheckerConfig{
			CheckInterval:      100 * time.Millisecond, // Fast for testing
			Timeout:            1 * time.Second,
			HealthyThreshold:   2,
			UnhealthyThreshold: 2,
			HealthEndpoint:     "/health",
		},
		Queue: proxy.QueueConfig{
			MaxSize: 100,
			Timeout: 1 * time.Second,
		},
		Timeout:        30 * time.Second,
		MaxRetries:     3,
		RetryBackoff:   50 * time.Millisecond,
		EnableQueueing: true,
	}

	logger := testutils.NewMockLogger()
	metrics := &MockEnhancedMetricsService{}

	service, err := proxy.NewEnhancedService(config, logger, metrics)
	require.NoError(t, err)
	defer service.Close()

	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)

	// Phase 1: Test normal operation with weighted load balancing
	t.Log("Phase 1: Testing weighted load balancing...")

	// Send requests to establish weighted distribution
	const phase1Requests = 60
	for i := 0; i < phase1Requests; i++ {
		req := createTestRequest("user1", "/api/v1/query", "query=up{job=\"prometheus\"}")
		req.TargetTenant = "1000"

		resp, err := service.Forward(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var response map[string]interface{}
		err = json.Unmarshal(resp.Body, &response)
		require.NoError(t, err)
		assert.Equal(t, "success", response["status"])
	}

	// Verify weighted distribution (approximately 3:2:1 ratio)
	calls1, calls2, calls3 := atomic.LoadInt64(&backend1Calls), atomic.LoadInt64(&backend2Calls), atomic.LoadInt64(&backend3Calls)
	totalCalls := calls1 + calls2 + calls3
	assert.Equal(t, int64(phase1Requests), totalCalls, "All requests should be processed")

	// Allow some tolerance in distribution
	ratio1 := float64(calls1) / float64(totalCalls)
	ratio2 := float64(calls2) / float64(totalCalls)
	ratio3 := float64(calls3) / float64(totalCalls)

	assert.InDelta(t, 0.5, ratio1, 0.15, "Backend 1 (weight=3) should get ~50% of requests")
	assert.InDelta(t, 0.33, ratio2, 0.15, "Backend 2 (weight=2) should get ~33% of requests")
	assert.InDelta(t, 0.17, ratio3, 0.15, "Backend 3 (weight=1) should get ~17% of requests")

	t.Logf("Weighted distribution: Backend1=%d (%.1f%%), Backend2=%d (%.1f%%), Backend3=%d (%.1f%%)",
		calls1, ratio1*100, calls2, ratio2*100, calls3, ratio3*100)

	// Phase 2: Test health check integration - make primary backend unhealthy
	t.Log("Phase 2: Testing health check integration...")

	backend1Healthy = false

	// Wait for health checker to detect and update load balancer
	require.Eventually(t, func() bool {
		status := service.GetBackendsStatus()
		for _, s := range status {
			if s.Backend.URL == backend1.URL {
				return !s.IsHealthy
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "Backend 1 should be marked unhealthy")

	// Reset counters and test failover
	atomic.StoreInt64(&backend1Calls, 0)
	atomic.StoreInt64(&backend2Calls, 0)
	atomic.StoreInt64(&backend3Calls, 0)

	const phase2Requests = 30
	for i := 0; i < phase2Requests; i++ {
		req := createTestRequest("user2", "/api/v1/query", "query=cpu_usage")
		req.TargetTenant = "1001"

		resp, err := service.Forward(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	// Verify failover - only backend 2 and 3 should receive requests
	calls1, calls2, calls3 = atomic.LoadInt64(&backend1Calls), atomic.LoadInt64(&backend2Calls), atomic.LoadInt64(&backend3Calls)
	assert.Equal(t, int64(0), calls1, "Unhealthy backend should not receive requests")
	assert.Greater(t, calls2, int64(0), "Backend 2 should receive requests")
	assert.Greater(t, calls3, int64(0), "Backend 3 should receive requests")

	// Should maintain 2:1 ratio between backend 2 and 3
	if calls2 > 0 && calls3 > 0 {
		ratio := float64(calls2) / float64(calls3)
		assert.InDelta(t, 2.0, ratio, 1.0, "Should maintain weight ratio during failover")
	}

	t.Logf("Failover distribution: Backend1=%d, Backend2=%d, Backend3=%d", calls1, calls2, calls3)

	// Phase 3: Test recovery
	t.Log("Phase 3: Testing backend recovery...")

	backend1Healthy = true

	// Wait for recovery
	require.Eventually(t, func() bool {
		status := service.GetBackendsStatus()
		for _, s := range status {
			if s.Backend.URL == backend1.URL {
				return s.IsHealthy
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "Backend 1 should recover to healthy")

	// Reset and test restored distribution
	atomic.StoreInt64(&backend1Calls, 0)
	atomic.StoreInt64(&backend2Calls, 0)
	atomic.StoreInt64(&backend3Calls, 0)

	const phase3Requests = 60
	for i := 0; i < phase3Requests; i++ {
		req := createTestRequest("user3", "/api/v1/query", "query=memory_usage")
		req.TargetTenant = "1002"

		resp, err := service.Forward(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	// Verify restored weighted distribution
	calls1, calls2, calls3 = atomic.LoadInt64(&backend1Calls), atomic.LoadInt64(&backend2Calls), atomic.LoadInt64(&backend3Calls)
	assert.Greater(t, calls1, int64(0), "Backend 1 should receive requests after recovery")
	assert.Greater(t, calls2, int64(0), "Backend 2 should continue receiving requests")
	assert.Greater(t, calls3, int64(0), "Backend 3 should continue receiving requests")

	t.Logf("Recovery distribution: Backend1=%d, Backend2=%d, Backend3=%d", calls1, calls2, calls3)

	// Phase 4: Test maintenance mode
	t.Log("Phase 4: Testing maintenance mode...")

	// Put backend 2 in maintenance mode
	err = service.SetMaintenanceMode(backend2.URL, true)
	require.NoError(t, err)

	// Verify maintenance mode in status
	require.Eventually(t, func() bool {
		status := service.GetBackendsStatus()
		for _, s := range status {
			if s.Backend.URL == backend2.URL {
				return s.Backend.State == domain.BackendMaintenance
			}
		}
		return false
	}, 2*time.Second, 50*time.Millisecond, "Backend 2 should be in maintenance mode")

	// Test that maintenance backend doesn't receive requests
	atomic.StoreInt64(&backend1Calls, 0)
	atomic.StoreInt64(&backend2Calls, 0)
	atomic.StoreInt64(&backend3Calls, 0)

	for i := 0; i < 20; i++ {
		req := createTestRequest("user4", "/api/v1/query", "query=disk_usage")
		resp, err := service.Forward(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	calls1, calls2, calls3 = atomic.LoadInt64(&backend1Calls), atomic.LoadInt64(&backend2Calls), atomic.LoadInt64(&backend3Calls)
	assert.Greater(t, calls1, int64(0), "Backend 1 should receive requests")
	assert.Equal(t, int64(0), calls2, "Backend 2 in maintenance should not receive requests")
	assert.Greater(t, calls3, int64(0), "Backend 3 should receive requests")

	// Disable maintenance mode
	err = service.SetMaintenanceMode(backend2.URL, false)
	require.NoError(t, err)

	// Phase 5: Test metrics and monitoring
	t.Log("Phase 5: Testing metrics and monitoring...")

	// Verify backend status
	status := service.GetBackendsStatus()
	require.Len(t, status, 3)

	healthyCount := 0
	for _, s := range status {
		if s.IsHealthy {
			healthyCount++
		}
		assert.True(t, s.Backend.Weight > 0, "All backends should have positive weights")
	}
	assert.Equal(t, 3, healthyCount, "All backends should be healthy at end")

	// Verify queue stats (queue is configured but requests are processed directly in current implementation)
	queueStats := service.GetQueueStats()
	if queueStats != nil {
		// Queue is available and configured, but requests may be processed directly
		assert.True(t, queueStats.MaxSize > 0, "Queue should be configured with positive max size")
		assert.False(t, queueStats.IsClosed, "Queue should not be closed")
		t.Logf("Queue stats: %+v", *queueStats)
	}

	// Verify health stats
	healthStats := service.GetHealthStats()
	if healthStats != nil {
		assert.Len(t, healthStats, 3, "Should have health stats for all backends")
		for url, stats := range healthStats {
			assert.True(t, stats.TotalChecks > 0, "Backend %s should have health check history", url)
			t.Logf("Health stats for %s: %+v", url, stats)
		}
	}

	t.Log("System integration test completed successfully!")
}
