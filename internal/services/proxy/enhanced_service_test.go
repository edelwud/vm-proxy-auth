package proxy_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config/modules/proxy"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	proxyService "github.com/edelwud/vm-proxy-auth/internal/services/proxy"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

// createTestBackend creates a test backend that handles both health checks and actual requests.
func createTestBackend(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthcheck-disabled" {
			// Health check endpoint
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
			return
		}
		// Delegate to actual handler
		handler(w, r)
	}))
}

func createTestRequest(userID string, query string) *domain.ProxyRequest {
	path := "/api/v1/query"
	u, _ := url.Parse(fmt.Sprintf("http://localhost%s", path))
	if query != "" {
		u.RawQuery = query
	}

	req := &http.Request{
		Method: http.MethodGet,
		URL:    u,
		Header: make(http.Header),
		Body:   io.NopCloser(strings.NewReader("")),
	}

	return &domain.ProxyRequest{
		User: &domain.User{
			ID:             userID,
			AllowedTenants: []string{"1000"},
		},
		OriginalRequest: req,
		FilteredQuery:   "",
		TargetTenant:    "1000",
	}
}

func TestEnhancedService_BasicForwarding(t *testing.T) {
	// Create test backend
	backend := createTestBackend(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/query", r.URL.Path)
		assert.Equal(t, "up", r.URL.Query().Get("query"))
		assert.Equal(t, "1000", r.Header.Get("X-Prometheus-Tenant"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	}))
	t.Cleanup(func() { backend.Close() })

	config := proxy.Config{
		Upstreams: []proxy.UpstreamConfig{
			{URL: backend.URL, Weight: 1},
		},
		Routing: proxy.RoutingConfig{
			Strategy: "round-robin",
			HealthCheck: proxy.HealthCheckConfig{
				Interval:           24 * time.Hour, // Effectively disable checks
				Timeout:            100 * time.Millisecond,
				Endpoint:           "/healthcheck-disabled",
				HealthyThreshold:   1,
				UnhealthyThreshold: 10,
			},
		},
		Reliability: proxy.ReliabilityConfig{
			Timeout: 5 * time.Second,
			Retries: 3,
			Backoff: 100 * time.Millisecond,
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &testutils.MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxyService.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	t.Cleanup(func() { service.Close() })

	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)

	// Create test request
	req := createTestRequest("test-user", "query=up")
	req.FilteredQuery = "up"

	// Forward request
	response, err := service.Forward(ctx, req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, "application/json", response.Headers.Get("Content-Type"))
	assert.JSONEq(t, `{"status":"success"}`, string(response.Body))
}

func TestEnhancedService_LoadBalancing_RoundRobin(t *testing.T) {
	// Create multiple test backends
	var requestCounts [3]int32
	var backends [3]*httptest.Server

	for i := range 3 {
		backends[i] = createTestBackend(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&requestCounts[i], 1)
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"backend":%d}`, i)
		}))
		defer backends[i].Close()
	}

	config := proxy.Config{
		Upstreams: []proxy.UpstreamConfig{
			{URL: backends[0].URL, Weight: 1},
			{URL: backends[1].URL, Weight: 1},
			{URL: backends[2].URL, Weight: 1},
		},
		Routing: proxy.RoutingConfig{
			Strategy: "round-robin",
			HealthCheck: proxy.HealthCheckConfig{
				Interval:           24 * time.Hour, // Effectively disable checks
				Timeout:            100 * time.Millisecond,
				Endpoint:           "/healthcheck-disabled",
				HealthyThreshold:   1,
				UnhealthyThreshold: 10,
			},
		},
		Reliability: proxy.ReliabilityConfig{
			Timeout: 5 * time.Second,
			Retries: 3,
			Backoff: 100 * time.Millisecond,
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &testutils.MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxyService.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	t.Cleanup(func() { service.Close() })

	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)

	// Send multiple requests
	const numRequests = 12
	for range numRequests {
		req := createTestRequest("test-user", "query=up")
		_, forwardErr := service.Forward(ctx, req)
		require.NoError(t, forwardErr)
	}

	// Verify load balancing distribution
	for i := range 3 {
		count := atomic.LoadInt32(&requestCounts[i])
		assert.Equal(t, int32(4), count, "Backend %d should receive 4 requests", i)
	}
}

func TestEnhancedService_LoadBalancing_WeightedRoundRobin(t *testing.T) {
	var requestCounts [2]int32
	var backends [2]*httptest.Server

	for i := range 2 {
		backends[i] = createTestBackend(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&requestCounts[i], 1)
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"backend":%d}`, i)
		}))
		defer backends[i].Close()
	}

	config := proxy.Config{
		Upstreams: []proxy.UpstreamConfig{
			{URL: backends[0].URL, Weight: 3}, // Should get 3x more requests
			{URL: backends[1].URL, Weight: 1},
		},
		Routing: proxy.RoutingConfig{
			Strategy: "weighted-round-robin",
			HealthCheck: proxy.HealthCheckConfig{
				Interval:           24 * time.Hour, // Effectively disable checks
				Timeout:            100 * time.Millisecond,
				Endpoint:           "/healthcheck-disabled",
				HealthyThreshold:   1,
				UnhealthyThreshold: 10,
			},
		},
		Reliability: proxy.ReliabilityConfig{
			Timeout: 5 * time.Second,
			Retries: 3,
			Backoff: 100 * time.Millisecond,
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &testutils.MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxyService.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	t.Cleanup(func() { service.Close() })

	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)

	// Send requests
	const numRequests = 40
	for range numRequests {
		req := createTestRequest("test-user", "query=up")
		_, forwardErr := service.Forward(ctx, req)
		require.NoError(t, forwardErr)
	}

	// Verify weighted distribution (should be ~3:1 ratio)
	count0 := atomic.LoadInt32(&requestCounts[0])
	count1 := atomic.LoadInt32(&requestCounts[1])

	// Allow some tolerance for weighted distribution
	expectedRatio := float64(count0) / float64(count1)
	assert.InDelta(t, 3.0, expectedRatio, 1.0, "Weighted distribution should be approximately 3:1")
	assert.Equal(t, numRequests, int(count0+count1), "Total requests should match")
}

func TestEnhancedService_RetryOnFailure(t *testing.T) {
	var attemptCount int32
	backend := createTestBackend(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := atomic.AddInt32(&attemptCount, 1)
		if count <= 2 {
			// Fail first 2 attempts
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Succeed on 3rd attempt
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	}))
	t.Cleanup(func() { backend.Close() })

	config := proxy.Config{
		Upstreams: []proxy.UpstreamConfig{
			{URL: backend.URL, Weight: 1},
		},
		Routing: proxy.RoutingConfig{
			Strategy: "round-robin",
			HealthCheck: proxy.HealthCheckConfig{
				Interval:           24 * time.Hour, // Effectively disable checks
				Timeout:            100 * time.Millisecond,
				Endpoint:           "/healthcheck-disabled",
				HealthyThreshold:   1,
				UnhealthyThreshold: 10,
			},
		},
		Reliability: proxy.ReliabilityConfig{
			Timeout: 5 * time.Second,
			Retries: 3,
			Backoff: 10 * time.Millisecond,
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &testutils.MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxyService.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	t.Cleanup(func() { service.Close() })

	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)

	req := createTestRequest("test-user", "query=up")

	start := time.Now()
	response, err := service.Forward(ctx, req)
	duration := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, int32(3), atomic.LoadInt32(&attemptCount))

	// Should take at least 2 * retryBackoff for the retries
	assert.GreaterOrEqual(t, duration, 20*time.Millisecond)
}

func TestEnhancedService_MaxRetriesExceeded(t *testing.T) {
	backend := createTestBackend(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Always fail
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(func() { backend.Close() })

	config := proxy.Config{
		Upstreams: []proxy.UpstreamConfig{
			{URL: backend.URL, Weight: 1},
		},
		Routing: proxy.RoutingConfig{
			Strategy: "round-robin",
			HealthCheck: proxy.HealthCheckConfig{
				Interval:           24 * time.Hour, // Effectively disable checks
				Timeout:            100 * time.Millisecond,
				Endpoint:           "/healthcheck-disabled",
				HealthyThreshold:   1,
				UnhealthyThreshold: 10,
			},
		},
		Reliability: proxy.ReliabilityConfig{
			Timeout: 5 * time.Second,
			Retries: 2,
			Backoff: 1 * time.Millisecond,
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &testutils.MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxyService.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	t.Cleanup(func() { service.Close() })

	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)

	req := createTestRequest("test-user", "query=up")

	_, err = service.Forward(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request failed after 2 attempts")
}

func TestEnhancedService_NoHealthyBackends(t *testing.T) {
	// Don't start any backends, so no healthy backends available
	config := proxy.Config{
		Upstreams: []proxy.UpstreamConfig{
			{URL: "http://localhost:99999", Weight: 1}, // Non-existent backend
		},
		Routing: proxy.RoutingConfig{
			Strategy: "round-robin",
			HealthCheck: proxy.HealthCheckConfig{
				Interval:           50 * time.Millisecond,
				Timeout:            10 * time.Millisecond,
				Endpoint:           "/health",
				HealthyThreshold:   1,
				UnhealthyThreshold: 1, // Mark unhealthy quickly
			},
		},
		Reliability: proxy.ReliabilityConfig{
			Timeout: 5 * time.Second,
			Retries: 3,
			Backoff: 100 * time.Millisecond,
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &testutils.MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxyService.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	t.Cleanup(func() { service.Close() })

	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)

	// Wait for health check to mark backend as unhealthy
	time.Sleep(100 * time.Millisecond)

	req := createTestRequest("test-user", "query=up")

	_, err = service.Forward(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no available backend")
}

func TestEnhancedService_MaintenanceMode(t *testing.T) {
	backend := createTestBackend(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	}))
	t.Cleanup(func() { backend.Close() })

	config := proxy.Config{
		Upstreams: []proxy.UpstreamConfig{
			{URL: backend.URL, Weight: 1},
		},
		Routing: proxy.RoutingConfig{
			Strategy: "round-robin",
			HealthCheck: proxy.HealthCheckConfig{
				Interval:           24 * time.Hour, // Effectively disable checks
				Timeout:            100 * time.Millisecond,
				Endpoint:           "/healthcheck-disabled",
				HealthyThreshold:   1,
				UnhealthyThreshold: 10,
			},
		},
		Reliability: proxy.ReliabilityConfig{
			Timeout: 5 * time.Second,
			Retries: 3,
			Backoff: 100 * time.Millisecond,
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &testutils.MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxyService.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	t.Cleanup(func() { service.Close() })

	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)

	// Enable maintenance mode
	err = service.SetMaintenanceMode(backend.URL, true)
	require.NoError(t, err)

	req := createTestRequest("test-user", "query=up")

	// Should fail because backend is in maintenance
	_, err = service.Forward(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no available backend")

	// Disable maintenance mode
	err = service.SetMaintenanceMode(backend.URL, false)
	require.NoError(t, err)

	// Should work now
	_, err = service.Forward(ctx, req)
	require.NoError(t, err)
}

func TestEnhancedService_BackendsStatus(t *testing.T) {
	backend := createTestBackend(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(func() { backend.Close() })

	config := proxy.Config{
		Upstreams: []proxy.UpstreamConfig{
			{URL: backend.URL, Weight: 2},
		},
		Routing: proxy.RoutingConfig{
			Strategy: "round-robin",
			HealthCheck: proxy.HealthCheckConfig{
				Interval:           24 * time.Hour, // Effectively disable checks
				Timeout:            100 * time.Millisecond,
				Endpoint:           "/healthcheck-disabled",
				HealthyThreshold:   1,
				UnhealthyThreshold: 10,
			},
		},
		Reliability: proxy.ReliabilityConfig{
			Timeout: 5 * time.Second,
			Retries: 3,
			Backoff: 100 * time.Millisecond,
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &testutils.MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxyService.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	t.Cleanup(func() { service.Close() })

	status := service.GetBackendsStatus()
	require.Len(t, status, 1)

	assert.Equal(t, backend.URL, status[0].Backend.URL)
	assert.Equal(t, 2, status[0].Backend.Weight)
	assert.True(t, status[0].IsHealthy)
}

func TestEnhancedService_ConcurrentRequests(t *testing.T) {
	var requestCount int32
	backend := createTestBackend(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		// Add small delay to simulate processing
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	}))
	t.Cleanup(func() { backend.Close() })

	config := proxy.Config{
		Upstreams: []proxy.UpstreamConfig{
			{URL: backend.URL, Weight: 1},
		},
		Routing: proxy.RoutingConfig{
			Strategy: "round-robin",
			HealthCheck: proxy.HealthCheckConfig{
				Interval:           24 * time.Hour, // Effectively disable checks
				Timeout:            100 * time.Millisecond,
				Endpoint:           "/healthcheck-disabled",
				HealthyThreshold:   1,
				UnhealthyThreshold: 10,
			},
		},
		Reliability: proxy.ReliabilityConfig{
			Timeout: 5 * time.Second,
			Retries: 3,
			Backoff: 100 * time.Millisecond,
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &testutils.MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxyService.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	t.Cleanup(func() { service.Close() })

	ctx := context.Background()
	err = service.Start(ctx)
	require.NoError(t, err)

	// Send concurrent requests
	const numRequests = 20
	var wg sync.WaitGroup
	var errors int32

	start := time.Now()
	for i := range numRequests {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req := createTestRequest(fmt.Sprintf("user-%d", id), "query=up")
			_, forwardErr := service.Forward(ctx, req)
			if forwardErr != nil {
				atomic.AddInt32(&errors, 1)
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	// All requests should succeed
	assert.Equal(t, int32(0), atomic.LoadInt32(&errors))
	assert.Equal(t, int32(numRequests), atomic.LoadInt32(&requestCount))

	// Should complete much faster than sequential (20 * 10ms = 200ms)
	assert.Less(t, duration, 100*time.Millisecond)
}

func TestEnhancedService_ContextCancellation(t *testing.T) {
	backend := createTestBackend(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Add delay to allow context cancellation
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(func() { backend.Close() })

	config := proxy.Config{
		Upstreams: []proxy.UpstreamConfig{
			{URL: backend.URL, Weight: 1},
		},
		Routing: proxy.RoutingConfig{
			Strategy: "round-robin",
			HealthCheck: proxy.HealthCheckConfig{
				Interval:           24 * time.Hour, // Effectively disable checks
				Timeout:            100 * time.Millisecond,
				Endpoint:           "/healthcheck-disabled",
				HealthyThreshold:   1,
				UnhealthyThreshold: 10,
			},
		},
		Reliability: proxy.ReliabilityConfig{
			Timeout: 5 * time.Second,
			Retries: 3,
			Backoff: 100 * time.Millisecond,
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &testutils.MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxyService.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	t.Cleanup(func() { service.Close() })

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	t.Cleanup(cancel)

	err = service.Start(context.Background()) // Use different context for startup
	require.NoError(t, err)

	req := createTestRequest("test-user", "query=up")

	// Request should be cancelled
	_, err = service.Forward(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}
