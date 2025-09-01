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

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/health"
	"github.com/edelwud/vm-proxy-auth/internal/services/proxy"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

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
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/query", r.URL.Path)
		assert.Equal(t, "up", r.URL.Query().Get("query"))
		assert.Equal(t, "1000", r.Header.Get("X-Prometheus-Tenant"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
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
			CheckInterval: -1, // Explicitly disable health checking
		},
		Timeout: 5 * time.Second,
	}

	logger := testutils.NewMockLogger()
	metrics := &MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxy.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	defer service.Close()

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
		backends[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&requestCounts[i], 1)
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"backend":%d}`, i)
		}))
		defer backends[i].Close()
	}

	config := proxy.EnhancedServiceConfig{
		Backends: []proxy.BackendConfig{
			{URL: backends[0].URL, Weight: 1},
			{URL: backends[1].URL, Weight: 1},
			{URL: backends[2].URL, Weight: 1},
		},
		LoadBalancing: proxy.LoadBalancingConfig{
			Strategy: domain.LoadBalancingRoundRobin,
		},
		HealthCheck: health.CheckerConfig{
			CheckInterval: -1, // Explicitly disable health checking
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxy.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	defer service.Close()

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
		backends[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt32(&requestCounts[i], 1)
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"backend":%d}`, i)
		}))
		defer backends[i].Close()
	}

	config := proxy.EnhancedServiceConfig{
		Backends: []proxy.BackendConfig{
			{URL: backends[0].URL, Weight: 3}, // Should get 3x more requests
			{URL: backends[1].URL, Weight: 1},
		},
		LoadBalancing: proxy.LoadBalancingConfig{
			Strategy: domain.LoadBalancingWeightedRoundRobin,
		},
		HealthCheck: health.CheckerConfig{
			CheckInterval: -1, // Explicitly disable health checking
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxy.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	defer service.Close()

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
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	defer backend.Close()

	config := proxy.EnhancedServiceConfig{
		Backends: []proxy.BackendConfig{
			{URL: backend.URL, Weight: 1},
		},
		LoadBalancing: proxy.LoadBalancingConfig{
			Strategy: domain.LoadBalancingRoundRobin,
		},
		MaxRetries:   3,
		RetryBackoff: 10 * time.Millisecond,
		HealthCheck: health.CheckerConfig{
			CheckInterval: -1, // Explicitly disable health checking
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxy.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	defer service.Close()

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
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Always fail
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	config := proxy.EnhancedServiceConfig{
		Backends: []proxy.BackendConfig{
			{URL: backend.URL, Weight: 1},
		},
		LoadBalancing: proxy.LoadBalancingConfig{
			Strategy: domain.LoadBalancingRoundRobin,
		},
		MaxRetries:   2,
		RetryBackoff: 1 * time.Millisecond,
		HealthCheck: health.CheckerConfig{
			CheckInterval: -1, // Explicitly disable health checking
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxy.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	defer service.Close()

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
	config := proxy.EnhancedServiceConfig{
		Backends: []proxy.BackendConfig{
			{URL: "http://localhost:99999", Weight: 1}, // Non-existent backend
		},
		LoadBalancing: proxy.LoadBalancingConfig{
			Strategy: domain.LoadBalancingRoundRobin,
		},
		HealthCheck: health.CheckerConfig{
			CheckInterval:      50 * time.Millisecond,
			Timeout:            10 * time.Millisecond,
			UnhealthyThreshold: 1, // Mark unhealthy quickly
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxy.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	defer service.Close()

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
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
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
			CheckInterval: -1, // Explicitly disable health checking
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxy.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	defer service.Close()

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
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	config := proxy.EnhancedServiceConfig{
		Backends: []proxy.BackendConfig{
			{URL: backend.URL, Weight: 2},
		},
		LoadBalancing: proxy.LoadBalancingConfig{
			Strategy: domain.LoadBalancingRoundRobin,
		},
		HealthCheck: health.CheckerConfig{
			CheckInterval: -1, // Explicitly disable health checking
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxy.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	defer service.Close()

	status := service.GetBackendsStatus()
	require.Len(t, status, 1)

	assert.Equal(t, backend.URL, status[0].Backend.URL)
	assert.Equal(t, 2, status[0].Backend.Weight)
	assert.True(t, status[0].IsHealthy)
}

func TestEnhancedService_ConcurrentRequests(t *testing.T) {
	var requestCount int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		// Add small delay to simulate processing
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
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
			CheckInterval: -1, // Explicitly disable health checking
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxy.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	defer service.Close()

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
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Add delay to allow context cancellation
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
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
			CheckInterval: -1, // Explicitly disable health checking
		},
	}

	logger := testutils.NewMockLogger()
	metrics := &MockEnhancedMetricsService{}

	stateStorage := testutils.NewMockStateStorage()
	service, err := proxy.NewEnhancedService(config, logger, metrics, stateStorage)
	require.NoError(t, err)
	defer service.Close()

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = service.Start(context.Background()) // Use different context for startup
	require.NoError(t, err)

	req := createTestRequest("test-user", "query=up")

	// Request should be cancelled
	_, err = service.Forward(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

// MockEnhancedMetricsService implements domain.MetricsService for testing enhanced proxy.
type MockEnhancedMetricsService struct {
	mu                          sync.Mutex
	backendStateChangeCallCount int
	loadBalancerSelectionCount  int
	upstreamBackendCallCount    int
}

// Add all the required methods for domain.MetricsService.
func (m *MockEnhancedMetricsService) RecordRequest(
	context.Context,
	string,
	string,
	string,
	time.Duration,
	*domain.User,
) {
}

func (m *MockEnhancedMetricsService) RecordUpstream(context.Context, string, string, string, time.Duration, []string) {
}

func (m *MockEnhancedMetricsService) RecordQueryFilter(context.Context, string, int, bool, time.Duration) {
}
func (m *MockEnhancedMetricsService) RecordAuthAttempt(context.Context, string, string)        {}
func (m *MockEnhancedMetricsService) RecordTenantAccess(context.Context, string, string, bool) {}

func (m *MockEnhancedMetricsService) RecordUpstreamBackend(
	context.Context,
	string,
	string,
	string,
	string,
	time.Duration,
	[]string,
) {
	m.mu.Lock()
	m.upstreamBackendCallCount++
	m.mu.Unlock()
}

func (m *MockEnhancedMetricsService) RecordHealthCheck(context.Context, string, bool, time.Duration) {
}

func (m *MockEnhancedMetricsService) RecordBackendStateChange(
	context.Context,
	string,
	domain.BackendState,
	domain.BackendState,
) {
	m.mu.Lock()
	m.backendStateChangeCallCount++
	m.mu.Unlock()
}

func (m *MockEnhancedMetricsService) RecordCircuitBreakerStateChange(
	context.Context,
	string,
	domain.CircuitBreakerState,
) {
}

func (m *MockEnhancedMetricsService) RecordQueueOperation(context.Context, string, time.Duration, int) {
}

func (m *MockEnhancedMetricsService) RecordLoadBalancerSelection(
	context.Context,
	domain.LoadBalancingStrategy,
	string,
	time.Duration,
) {
	m.mu.Lock()
	m.loadBalancerSelectionCount++
	m.mu.Unlock()
}

func (m *MockEnhancedMetricsService) Handler() http.Handler { return nil }

func (m *MockEnhancedMetricsService) GetCallCounts() (int, int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.backendStateChangeCallCount, m.loadBalancerSelectionCount, m.upstreamBackendCallCount
}
