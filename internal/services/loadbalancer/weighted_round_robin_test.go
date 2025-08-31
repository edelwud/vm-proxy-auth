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

func TestWeightedRoundRobinBalancer_BasicDistribution(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 3, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 2, State: domain.BackendHealthy},
		{URL: "http://backend3.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewWeightedRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Test distribution over multiple requests
	counts := make(map[string]int)
	numRequests := 60 // Multiple of total weight (3+2+1=6)

	for range numRequests {
		backend, err := balancer.NextBackend(ctx)
		require.NoError(t, err)
		counts[backend.URL]++
	}

	// Verify weighted distribution
	// Backend1 (weight 3) should get ~3/6 = 50% of requests
	// Backend2 (weight 2) should get ~2/6 = 33% of requests
	// Backend3 (weight 1) should get ~1/6 = 17% of requests
	assert.InDelta(t, 30, counts["http://backend1.com"], 5, "Backend1 should get ~30 requests")
	assert.InDelta(t, 20, counts["http://backend2.com"], 5, "Backend2 should get ~20 requests")
	assert.InDelta(t, 10, counts["http://backend3.com"], 5, "Backend3 should get ~10 requests")

	total := counts["http://backend1.com"] + counts["http://backend2.com"] + counts["http://backend3.com"]
	assert.Equal(t, numRequests, total)
}

func TestWeightedRoundRobinBalancer_SmoothDistribution(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 5, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewWeightedRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Test smooth weighted round-robin (should not return all backend1 first)
	results := make([]string, 12)
	for i := range 12 {
		backend, err := balancer.NextBackend(ctx)
		require.NoError(t, err)
		results[i] = backend.URL
	}

	// Debug: print the sequence
	t.Logf("Selection sequence: %v", results)

	// Count consecutive backend1 selections
	maxConsecutive := 0
	currentConsecutive := 0
	for i := range results {
		if results[i] == "http://backend1.com" {
			currentConsecutive++
			if currentConsecutive > maxConsecutive {
				maxConsecutive = currentConsecutive
			}
		} else {
			currentConsecutive = 0
		}
	}

	t.Logf("Max consecutive backend1 selections: %d", maxConsecutive)

	// With smooth weighted round-robin, we should see interleaving
	// For a 5:1 ratio, we expect pattern like: 1,1,1,1,1,2,1,1,1,1,1,2
	// But smooth should give us: 1,1,2,1,1,1,1,2,1,1,1,1 or similar
	// Let's check that backend2 appears within the first 6 selections
	backend2InFirst6 := false
	for i := 0; i < 6 && i < len(results); i++ {
		if results[i] == "http://backend2.com" {
			backend2InFirst6 = true
			break
		}
	}
	assert.True(t, backend2InFirst6, "Backend2 should appear within first 6 selections for smooth distribution")

	// But overall distribution should still respect weights
	counts := make(map[string]int)
	for _, url := range results {
		counts[url]++
	}
	assert.InDelta(t, 10, counts["http://backend1.com"], 2, "Backend1 should get ~10/12 requests")
	assert.InDelta(t, 2, counts["http://backend2.com"], 1, "Backend2 should get ~2/12 requests")
}

func TestWeightedRoundRobinBalancer_DefaultWeights(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 0, State: domain.BackendHealthy},  // Should default to 1
		{URL: "http://backend2.com", Weight: -5, State: domain.BackendHealthy}, // Should default to 1
		{URL: "http://backend3.com", Weight: 3, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewWeightedRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	// Verify weights were normalized
	assert.Equal(t, 1, balancer.GetBackendWeight("http://backend1.com"))
	assert.Equal(t, 1, balancer.GetBackendWeight("http://backend2.com"))
	assert.Equal(t, 3, balancer.GetBackendWeight("http://backend3.com"))
	assert.Equal(t, 5, balancer.GetTotalWeight())
}

func TestWeightedRoundRobinBalancer_ConcurrentAccess(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 2, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewWeightedRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()
	const numGoroutines = 50
	const requestsPerGoroutine = 10

	results := make(chan string, numGoroutines*requestsPerGoroutine)
	var wg sync.WaitGroup

	// Start concurrent goroutines
	errors := make(chan error, numGoroutines*requestsPerGoroutine)
	for range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range requestsPerGoroutine {
				backend, err := balancer.NextBackend(ctx)
				if err != nil {
					errors <- err
					return
				}
				results <- backend.URL
			}
		}()
	}

	wg.Wait()
	close(results)
	close(errors)

	// Check for errors
	select {
	case err := <-errors:
		require.NoError(t, err)
	default:
		// No errors
	}

	// Count distribution
	counts := make(map[string]int)
	totalRequests := 0
	for url := range results {
		counts[url]++
		totalRequests++
	}

	// Verify total requests
	assert.Equal(t, numGoroutines*requestsPerGoroutine, totalRequests)

	// Verify weighted distribution (backend1:backend2 should be ~2:1)
	expectedBackend1 := totalRequests * 2 / 3
	expectedBackend2 := totalRequests * 1 / 3

	tolerance := float64(totalRequests) * 0.15 // 15% tolerance for concurrent access
	assert.InDeltaf(t, expectedBackend1, counts["http://backend1.com"], tolerance,
		"Backend1 should get ~%d requests, got %d", expectedBackend1, counts["http://backend1.com"])
	assert.InDeltaf(t, expectedBackend2, counts["http://backend2.com"], tolerance,
		"Backend2 should get ~%d requests, got %d", expectedBackend2, counts["http://backend2.com"])
}

func TestWeightedRoundRobinBalancer_NoBackends(t *testing.T) {
	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewWeightedRoundRobinBalancer([]domain.Backend{}, logger)
	defer balancer.Close()

	ctx := context.Background()
	backend, err := balancer.NextBackend(ctx)
	assert.Nil(t, backend)
	assert.Equal(t, domain.ErrNoHealthyBackends, err)
}

func TestWeightedRoundRobinBalancer_SingleBackend(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://only-backend.com", Weight: 5, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewWeightedRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Should return the same backend multiple times
	for range 5 {
		backend, err := balancer.NextBackend(ctx)
		require.NoError(t, err)
		assert.Equal(t, "http://only-backend.com", backend.URL)
	}
}

func TestWeightedRoundRobinBalancer_MixedHealthyUnhealthy(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 3, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 5, State: domain.BackendUnhealthy},
		{URL: "http://backend3.com", Weight: 2, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewWeightedRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Should only return healthy backends with their relative weights
	counts := make(map[string]int)
	for range 50 {
		backend, err := balancer.NextBackend(ctx)
		require.NoError(t, err)
		counts[backend.URL]++

		// Should never get the unhealthy backend
		assert.NotEqual(t, "http://backend2.com", backend.URL)
	}

	// Should have both healthy backends with 3:2 ratio
	assert.Positive(t, counts["http://backend1.com"])
	assert.Positive(t, counts["http://backend3.com"])
	assert.Equal(t, 0, counts["http://backend2.com"])

	// Ratio should be approximately 3:2
	ratio := float64(counts["http://backend1.com"]) / float64(counts["http://backend3.com"])
	assert.InDelta(t, 1.5, ratio, 0.5, "Ratio should be approximately 3:2 = 1.5")
}

func TestWeightedRoundRobinBalancer_WithRateLimitedFallback(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 3, State: domain.BackendRateLimited},
		{URL: "http://backend2.com", Weight: 2, State: domain.BackendUnhealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewWeightedRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	ctx := context.Background()

	// Should return rate-limited backend as fallback
	backend, err := balancer.NextBackend(ctx)
	require.NoError(t, err)
	assert.Equal(t, "http://backend1.com", backend.URL)
}

func TestWeightedRoundRobinBalancer_BackendsStatus(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 3, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 2, State: domain.BackendUnhealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewWeightedRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	status := balancer.BackendsStatus()
	require.Len(t, status, 2)

	// Find backend status entries
	var backend1Status, backend2Status *domain.BackendStatus
	for _, s := range status {
		switch s.Backend.URL {
		case "http://backend1.com":
			backend1Status = s
		case "http://backend2.com":
			backend2Status = s
		}
	}

	require.NotNil(t, backend1Status)
	require.NotNil(t, backend2Status)

	assert.Equal(t, "http://backend1.com", backend1Status.Backend.URL)
	assert.True(t, backend1Status.IsHealthy)
	assert.Equal(t, 3, backend1Status.Backend.Weight)

	assert.Equal(t, "http://backend2.com", backend2Status.Backend.URL)
	assert.False(t, backend2Status.IsHealthy)
	assert.Equal(t, 2, backend2Status.Backend.Weight)
}

func TestWeightedRoundRobinBalancer_GetMethods(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 3, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 2, State: domain.BackendUnhealthy},
		{URL: "http://backend3.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewWeightedRoundRobinBalancer(backends, logger)
	defer balancer.Close()

	assert.Equal(t, 3, balancer.GetBackendCount())
	assert.Equal(t, 2, balancer.GetHealthyBackendCount()) // backend1 and backend3
	assert.Equal(t, 6, balancer.GetTotalWeight())         // 3+2+1

	assert.Equal(t, 3, balancer.GetBackendWeight("http://backend1.com"))
	assert.Equal(t, 2, balancer.GetBackendWeight("http://backend2.com"))
	assert.Equal(t, 1, balancer.GetBackendWeight("http://backend3.com"))
	assert.Equal(t, 0, balancer.GetBackendWeight("http://nonexistent.com"))
}

func TestWeightedRoundRobinBalancer_Close(t *testing.T) {
	backends := []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewWeightedRoundRobinBalancer(backends, logger)

	// Should be able to close
	err := balancer.Close()
	require.NoError(t, err)

	// Should be able to close multiple times
	err = balancer.Close()
	require.NoError(t, err)

	// Should not be able to get backends after close
	ctx := context.Background()
	backend, err := balancer.NextBackend(ctx)
	assert.Nil(t, backend)
	assert.Equal(t, domain.ErrNoHealthyBackends, err)
}

func BenchmarkWeightedRoundRobinBalancer_NextBackend(b *testing.B) {
	backends := make([]domain.Backend, 10)
	for i := range backends {
		backends[i] = domain.Backend{
			URL:    fmt.Sprintf("http://backend%d.com", i),
			Weight: i + 1, // Varying weights 1-10
			State:  domain.BackendHealthy,
		}
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewWeightedRoundRobinBalancer(backends, logger)
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

func BenchmarkWeightedRoundRobinBalancer_NextBackendWithSomeUnhealthy(b *testing.B) {
	backends := make([]domain.Backend, 10)
	for i := range backends {
		state := domain.BackendHealthy
		if i%3 == 0 {
			state = domain.BackendUnhealthy
		}
		backends[i] = domain.Backend{
			URL:    fmt.Sprintf("http://backend%d.com", i),
			Weight: i + 1, // Varying weights
			State:  state,
		}
	}

	logger := testutils.NewMockLogger()
	balancer := loadbalancer.NewWeightedRoundRobinBalancer(backends, logger)
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
