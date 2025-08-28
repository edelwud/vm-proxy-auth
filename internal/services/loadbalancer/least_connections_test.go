package loadbalancer_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/loadbalancer"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestLeastConnectionsBalancer_BasicDistribution(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 1, State: domain.BackendHealthy},
		{URL: "http://backend3.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewLeastConnectionsBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Initially all backends have 0 connections, so should distribute round-robin style
	selectedBackends := make(map[string]int)
	for i := 0; i < 6; i++ {
		backend, err := balancer.NextBackend(ctx)
		require.NoError(t, err)
		selectedBackends[backend.URL]++
	}

	// Each backend should get some requests
	assert.Equal(t, 3, len(selectedBackends), "All backends should be selected")
	for url, count := range selectedBackends {
		assert.Greater(t, count, 0, "Backend %s should have received requests", url)
	}
}

func TestLeastConnectionsBalancer_ConnectionCounting(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewLeastConnectionsBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Get first backend
	backend1, err := balancer.NextBackend(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, balancer.GetBackendConnections(backend1.URL))

	// Get second backend
	backend2, err := balancer.NextBackend(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, balancer.GetBackendConnections(backend2.URL))

	// Next request should go to first backend again (or second, both have 1 connection)
	_, err = balancer.NextBackend(ctx)
	require.NoError(t, err)

	// One backend should have 2 connections, the other 1
	conn1 := balancer.GetBackendConnections("http://backend1.com")
	conn2 := balancer.GetBackendConnections("http://backend2.com")
	assert.Equal(t, 3, conn1+conn2, "Total connections should be 3")

	// Report completion of first request
	balancer.ReportResult(backend1, nil, 200)

	// Connection count for backend1 should decrease
	newConn1 := balancer.GetBackendConnections(backend1.URL)
	assert.Equal(t, conn1-1, newConn1, "Backend1 connections should decrease by 1")
}

func TestLeastConnectionsBalancer_LeastConnectionsSelection(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 1, State: domain.BackendHealthy},
		{URL: "http://backend3.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewLeastConnectionsBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Create uneven load
	backend1, _ := balancer.NextBackend(ctx) // backend1: 1 connection
	backend2, _ := balancer.NextBackend(ctx) // backend2: 1 connection
	backend3, _ := balancer.NextBackend(ctx) // backend3: 1 connection

	// Complete request to backend3
	balancer.ReportResult(backend3, nil, 200) // backend3: 0 connections

	// Next request should go to backend3 (least connections)
	nextBackend, err := balancer.NextBackend(ctx)
	require.NoError(t, err)
	assert.Equal(t, backend3.URL, nextBackend.URL, "Should select backend with least connections")

	// Verify connection counts
	assert.Equal(t, 1, balancer.GetBackendConnections(backend1.URL))
	assert.Equal(t, 1, balancer.GetBackendConnections(backend2.URL))
	assert.Equal(t, 1, balancer.GetBackendConnections(backend3.URL))
}

func TestLeastConnectionsBalancer_ConcurrentAccess(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewLeastConnectionsBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()
	const numGoroutines = 50
	const requestsPerGoroutine = 10

	results := make(chan *domain.Backend, numGoroutines*requestsPerGoroutine)
	var wg sync.WaitGroup

	// Start concurrent goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				backend, err := balancer.NextBackend(ctx)
				require.NoError(t, err)
				results <- backend
			}
		}()
	}

	wg.Wait()
	close(results)

	// Count distribution
	counts := make(map[string]int)
	totalRequests := 0
	for backend := range results {
		counts[backend.URL]++
		totalRequests++
	}

	// Verify total requests
	assert.Equal(t, numGoroutines*requestsPerGoroutine, totalRequests)

	// Both backends should get requests (distribution may be uneven due to timing)
	assert.Greater(t, counts["http://backend1.com"], 0)
	assert.Greater(t, counts["http://backend2.com"], 0)

	// Verify total connection count matches
	totalConns := balancer.GetBackendConnections("http://backend1.com") +
		balancer.GetBackendConnections("http://backend2.com")
	assert.Equal(t, totalRequests, totalConns)
}

func TestLeastConnectionsBalancer_NoBackends(t *testing.T) {
	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewLeastConnectionsBalancer([]domain.Backend{}, logger)
	defer balancer.Close()

	ctx := context.Background()
	backend, err := balancer.NextBackend(ctx)
	assert.Nil(t, backend)
	assert.Equal(t, domain.ErrNoHealthyBackends, err)
}

func TestLeastConnectionsBalancer_SingleBackend(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://only-backend.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewLeastConnectionsBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Should return the same backend multiple times
	for i := 1; i <= 5; i++ {
		backend, err := balancer.NextBackend(ctx)
		require.NoError(t, err)
		assert.Equal(t, "http://only-backend.com", backend.URL)
		assert.Equal(t, i, balancer.GetBackendConnections(backend.URL))
	}
}

func TestLeastConnectionsBalancer_MixedHealthyUnhealthy(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 1, State: domain.BackendUnhealthy},
		{URL: "http://backend3.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewLeastConnectionsBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Should only return healthy backends
	seenBackends := make(map[string]bool)
	for i := 0; i < 10; i++ {
		backend, err := balancer.NextBackend(ctx)
		require.NoError(t, err)
		seenBackends[backend.URL] = true

		// Should never get the unhealthy backend
		assert.NotEqual(t, "http://backend2.com", backend.URL)
	}

	// Should have seen both healthy backends
	assert.True(t, seenBackends["http://backend1.com"])
	assert.True(t, seenBackends["http://backend3.com"])
	assert.False(t, seenBackends["http://backend2.com"])
}

func TestLeastConnectionsBalancer_WithRateLimitedFallback(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendRateLimited},
		{URL: "http://backend2.com", Weight: 1, State: domain.BackendUnhealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewLeastConnectionsBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Should return rate-limited backend as fallback
	backend, err := balancer.NextBackend(ctx)
	require.NoError(t, err)
	assert.Equal(t, "http://backend1.com", backend.URL)
	assert.Equal(t, 1, balancer.GetBackendConnections(backend.URL))
}

func TestLeastConnectionsBalancer_BackendsStatus(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 1, State: domain.BackendUnhealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewLeastConnectionsBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Create some connections
	backend1, _ := balancer.NextBackend(ctx)
	balancer.NextBackend(ctx) // Should go to backend1 again since backend2 is unhealthy

	status := balancer.BackendsStatus()
	require.Len(t, status, 2)

	// Find backend status entries
	var backend1Status, backend2Status *domain.BackendStatus
	for _, s := range status {
		if s.Backend.URL == "http://backend1.com" {
			backend1Status = s
		} else if s.Backend.URL == "http://backend2.com" {
			backend2Status = s
		}
	}

	require.NotNil(t, backend1Status)
	require.NotNil(t, backend2Status)

	assert.Equal(t, "http://backend1.com", backend1Status.Backend.URL)
	assert.True(t, backend1Status.IsHealthy)
	assert.Equal(t, int32(2), backend1Status.ActiveConns)

	assert.Equal(t, "http://backend2.com", backend2Status.Backend.URL)
	assert.False(t, backend2Status.IsHealthy)
	assert.Equal(t, int32(0), backend2Status.ActiveConns)

	// Report completion to verify connection tracking
	balancer.ReportResult(backend1, nil, 200)

	updatedStatus := balancer.BackendsStatus()
	for _, s := range updatedStatus {
		if s.Backend.URL == "http://backend1.com" {
			assert.Equal(t, int32(1), s.ActiveConns, "Active connections should decrease after reporting completion")
		}
	}
}

func TestLeastConnectionsBalancer_GetMethods(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 1, State: domain.BackendUnhealthy},
		{URL: "http://backend3.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewLeastConnectionsBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	assert.Equal(t, 3, balancer.GetBackendCount())
	assert.Equal(t, 2, balancer.GetHealthyBackendCount()) // backend1 and backend3

	// Initially all backends should have 0 connections
	assert.Equal(t, 0, balancer.GetBackendConnections("http://backend1.com"))
	assert.Equal(t, 0, balancer.GetBackendConnections("http://backend2.com"))
	assert.Equal(t, 0, balancer.GetBackendConnections("http://backend3.com"))
	assert.Equal(t, -1, balancer.GetBackendConnections("http://nonexistent.com"))

	// Make a request and verify connection count
	backend, _ := balancer.NextBackend(ctx)
	assert.Equal(t, 1, balancer.GetBackendConnections(backend.URL))
	assert.Equal(t, int64(1), balancer.GetBackendTotalRequests(backend.URL))
}

func TestLeastConnectionsBalancer_ResetStats(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewLeastConnectionsBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Create some activity
	backend, _ := balancer.NextBackend(ctx)
	assert.Equal(t, 1, balancer.GetBackendConnections(backend.URL))
	assert.Equal(t, int64(1), balancer.GetBackendTotalRequests(backend.URL))

	// Reset stats
	balancer.ResetStats()

	assert.Equal(t, 0, balancer.GetBackendConnections(backend.URL))
	assert.Equal(t, int64(0), balancer.GetBackendTotalRequests(backend.URL))
}

func TestLeastConnectionsBalancer_Close(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewLeastConnectionsBalancer(backends, logger)

	// Create some activity
	ctx := context.Background()
	backend, _ := balancer.NextBackend(ctx)
	assert.Equal(t, 1, balancer.GetBackendConnections(backend.URL))

	// Should be able to close
	err := balancer.Close()
	assert.NoError(t, err)

	// Should be able to close multiple times
	err = balancer.Close()
	assert.NoError(t, err)

	// Should not be able to get backends after close
	backend, err = balancer.NextBackend(ctx)
	assert.Nil(t, backend)
	assert.Equal(t, domain.ErrNoHealthyBackends, err)
}

func TestLeastConnectionsBalancer_ErrorReporting(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewLeastConnectionsBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()
	backend, _ := balancer.NextBackend(ctx)

	// Report error - should still decrement connection count
	balancer.ReportResult(backend, fmt.Errorf("connection failed"), 502)
	assert.Equal(t, 0, balancer.GetBackendConnections(backend.URL))

	// Report result for unknown backend - should not cause panic
	unknownBackend := &domain.Backend{URL: "http://unknown.com", State: domain.BackendHealthy}
	balancer.ReportResult(unknownBackend, nil, 200)
}

func BenchmarkLeastConnectionsBalancer_NextBackend(b *testing.B) {
	backends := make([]domain.Backend, 10)
	for i := range backends {
		backends[i] = domain.Backend{
			URL:    fmt.Sprintf("http://backend%d.com", i),
			Weight: 1,
			State:  domain.BackendHealthy,
		}
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewLeastConnectionsBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			backend, err := balancer.NextBackend(ctx)
			if err != nil {
				b.Fatal(err)
			}
			// Simulate request completion to avoid connection buildup
			balancer.ReportResult(backend, nil, 200)
		}
	})
}

func BenchmarkLeastConnectionsBalancer_NextBackendWithSomeUnhealthy(b *testing.B) {
	backends := make([]domain.Backend, 10)
	for i := range backends {
		state := domain.BackendHealthy
		if i%3 == 0 {
			state = domain.BackendUnhealthy
		}
		backends[i] = domain.Backend{
			URL:    fmt.Sprintf("http://backend%d.com", i),
			Weight: 1,
			State:  state,
		}
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewLeastConnectionsBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			backend, err := balancer.NextBackend(ctx)
			if err != nil {
				b.Fatal(err)
			}
			// Simulate request completion
			balancer.ReportResult(backend, nil, 200)
		}
	})
}
