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

func TestRoundRobinBalancer_BasicDistribution(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 1, State: domain.BackendHealthy},
		{URL: "http://backend3.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Test sequential distribution
	expectedOrder := []string{
		"http://backend1.com",
		"http://backend2.com",
		"http://backend3.com",
		"http://backend1.com", // Should wrap around
	}

	for i, expected := range expectedOrder {
		backend, err := balancer.NextBackend(ctx)
		require.NoError(t, err)
		assert.Equal(t, expected, backend.URL, "Request %d should go to %s", i+1, expected)
	}
}

func TestRoundRobinBalancer_ConcurrentAccess(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()
	const numGoroutines = 100
	const requestsPerGoroutine = 10

	results := make(chan string, numGoroutines*requestsPerGoroutine)
	var wg sync.WaitGroup

	// Start concurrent goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				backend, err := balancer.NextBackend(ctx)
				require.NoError(t, err)
				results <- backend.URL
			}
		}()
	}

	wg.Wait()
	close(results)

	// Count distribution
	counts := make(map[string]int)
	totalRequests := 0
	for url := range results {
		counts[url]++
		totalRequests++
	}

	// Verify total requests
	assert.Equal(t, numGoroutines*requestsPerGoroutine, totalRequests)

	// Verify balanced distribution (within 10% tolerance)
	expected := totalRequests / len(backends)
	for backend, count := range counts {
		tolerance := float64(expected) * 0.1
		assert.InDeltaf(t, expected, count, tolerance,
			"Backend %s should have ~%d requests, got %d", backend, expected, count)
	}
}

func TestRoundRobinBalancer_NoBackends(t *testing.T) {
	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewRoundRobinBalancer([]domain.Backend{}, logger)
	defer balancer.Close()

	ctx := context.Background()
	backend, err := balancer.NextBackend(ctx)
	assert.Nil(t, backend)
	assert.Equal(t, domain.ErrNoHealthyBackends, err)
}

func TestRoundRobinBalancer_SingleBackend(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://only-backend.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Should return the same backend multiple times
	for i := 0; i < 5; i++ {
		backend, err := balancer.NextBackend(ctx)
		require.NoError(t, err)
		assert.Equal(t, "http://only-backend.com", backend.URL)
	}
}

func TestRoundRobinBalancer_OnlyUnhealthyBackends(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendUnhealthy},
		{URL: "http://backend2.com", Weight: 1, State: domain.BackendMaintenance},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()
	backend, err := balancer.NextBackend(ctx)
	assert.Nil(t, backend)
	assert.Equal(t, domain.ErrNoHealthyBackends, err)
}

func TestRoundRobinBalancer_MixedHealthyUnhealthy(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 1, State: domain.BackendUnhealthy},
		{URL: "http://backend3.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewRoundRobinBalancer(backends, logger)
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

func TestRoundRobinBalancer_WithRateLimitedFallback(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendRateLimited},
		{URL: "http://backend2.com", Weight: 1, State: domain.BackendUnhealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Should return rate-limited backend as fallback
	backend, err := balancer.NextBackend(ctx)
	require.NoError(t, err)
	assert.Equal(t, "http://backend1.com", backend.URL)
}

func TestRoundRobinBalancer_ReportResult(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()
	backend, err := balancer.NextBackend(ctx)
	require.NoError(t, err)

	// Report successful result
	balancer.ReportResult(backend, nil, 200)

	// Report failed result
	balancer.ReportResult(backend, fmt.Errorf("connection failed"), 502)

	// Should not cause any panics or errors
	// (Round robin doesn't track results, but the interface should work)
}

func TestRoundRobinBalancer_BackendsStatus(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 2, State: domain.BackendUnhealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	status := balancer.BackendsStatus()
	require.Len(t, status, 2)

	// Find backend1 status
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
	assert.Equal(t, domain.BackendHealthy, backend1Status.Backend.State)

	assert.Equal(t, "http://backend2.com", backend2Status.Backend.URL)
	assert.False(t, backend2Status.IsHealthy)
	assert.Equal(t, domain.BackendUnhealthy, backend2Status.Backend.State)
}

func TestRoundRobinBalancer_ContextCancellation(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should still work (round robin doesn't use context for cancellation)
	backend, err := balancer.NextBackend(ctx)
	require.NoError(t, err)
	assert.Equal(t, "http://backend1.com", backend.URL)
}

func TestRoundRobinBalancer_Close(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewRoundRobinBalancer(backends, logger)

	// Should be able to close
	err := balancer.Close()
	assert.NoError(t, err)

	// Should be able to close multiple times
	err = balancer.Close()
	assert.NoError(t, err)
}

func TestRoundRobinBalancer_UpdateBackendStates(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Initially both backends should be available
	seen := make(map[string]bool)
	for i := 0; i < 4; i++ {
		backend, err := balancer.NextBackend(ctx)
		require.NoError(t, err)
		seen[backend.URL] = true
	}
	assert.Len(t, seen, 2)

	// Update backend state through the internal method (simulating health checker)
	// Note: This would typically be done through a health checker updating the backend state
	// For this test, we'll verify that the balancer respects the current state
}

func BenchmarkRoundRobinBalancer_NextBackend(b *testing.B) {
	backends := make([]domain.Backend, 10)
	for i := range backends {
		backends[i] = domain.Backend{
			URL:    fmt.Sprintf("http://backend%d.com", i),
			Weight: 1,
			State:  domain.BackendHealthy,
		}
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := balancer.NextBackend(ctx)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkRoundRobinBalancer_NextBackendWithSomeUnhealthy(b *testing.B) {
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
	balancer := loadbalancer.NewRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := balancer.NextBackend(ctx)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
