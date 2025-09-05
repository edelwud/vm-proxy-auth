package loadbalancer_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/loadbalancer"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func createTestBackends() []domain.Backend {
	return []domain.Backend{
		{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
		{URL: "http://backend2.com", Weight: 2, State: domain.BackendHealthy},
		{URL: "http://backend3.com", Weight: 1, State: domain.BackendHealthy},
	}
}

func TestFactory_CreateLoadBalancer_RoundRobin(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	factory := loadbalancer.NewFactory(logger)
	backends := createTestBackends()

	lb, err := factory.CreateLoadBalancer(domain.LoadBalancingStrategyRoundRobin, backends)
	require.NoError(t, err)
	require.NotNil(t, lb)

	// Verify it's a Round Robin balancer by checking type
	_, ok := lb.(*loadbalancer.RoundRobinBalancer)
	assert.True(t, ok, "Expected RoundRobinBalancer")
}

func TestFactory_CreateLoadBalancer_WeightedRoundRobin(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	factory := loadbalancer.NewFactory(logger)
	backends := createTestBackends()

	lb, err := factory.CreateLoadBalancer(domain.LoadBalancingStrategyWeighted, backends)
	require.NoError(t, err)
	require.NotNil(t, lb)

	// Verify it's a Weighted Round Robin balancer
	_, ok := lb.(*loadbalancer.WeightedRoundRobinBalancer)
	assert.True(t, ok, "Expected WeightedRoundRobinBalancer")
}

func TestFactory_CreateLoadBalancer_LeastConnections(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	factory := loadbalancer.NewFactory(logger)
	backends := createTestBackends()

	lb, err := factory.CreateLoadBalancer(domain.LoadBalancingStrategyLeastConnection, backends)
	require.NoError(t, err)
	require.NotNil(t, lb)

	// Verify it's a Least Connections balancer
	_, ok := lb.(*loadbalancer.LeastConnectionsBalancer)
	assert.True(t, ok, "Expected LeastConnectionsBalancer")
}

func TestFactory_CreateLoadBalancer_UnsupportedStrategy(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	factory := loadbalancer.NewFactory(logger)
	backends := createTestBackends()

	lb, err := factory.CreateLoadBalancer("unsupported-strategy", backends)
	require.Error(t, err)
	assert.Nil(t, lb)
	assert.Contains(t, err.Error(), "unsupported load balancing strategy")
}

func TestFactory_CreateLoadBalancer_EmptyBackends(t *testing.T) {
	t.Parallel()
	logger := testutils.NewMockLogger()
	factory := loadbalancer.NewFactory(logger)

	lb, err := factory.CreateLoadBalancer(domain.LoadBalancingStrategyRoundRobin, []domain.Backend{})
	require.Error(t, err)
	assert.Nil(t, lb)
	assert.Contains(t, err.Error(), "cannot create load balancer with empty backends list")
}

func TestFactory_GetSupportedStrategies(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	factory := loadbalancer.NewFactory(logger)

	strategies := factory.GetSupportedStrategies()

	expectedStrategies := []domain.LoadBalancingStrategy{
		domain.LoadBalancingStrategyRoundRobin,
		domain.LoadBalancingStrategyWeighted,
		domain.LoadBalancingStrategyLeastConnection,
	}

	assert.ElementsMatch(t, expectedStrategies, strategies)
	assert.Len(t, strategies, len(expectedStrategies))
}

func TestFactory_ValidateStrategy(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	factory := loadbalancer.NewFactory(logger)

	testCases := []struct {
		name     string
		strategy domain.LoadBalancingStrategy
		wantErr  bool
	}{
		{
			name:     "Valid Round Robin",
			strategy: domain.LoadBalancingStrategyRoundRobin,
			wantErr:  false,
		},
		{
			name:     "Valid Weighted Round Robin",
			strategy: domain.LoadBalancingStrategyWeighted,
			wantErr:  false,
		},
		{
			name:     "Valid Least Connections",
			strategy: domain.LoadBalancingStrategyLeastConnection,
			wantErr:  false,
		},
		{
			name:     "Invalid Strategy",
			strategy: "invalid-strategy",
			wantErr:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := factory.ValidateStrategy(tc.strategy)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported load balancing strategy")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFactory_GetStrategyDescription(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	factory := loadbalancer.NewFactory(logger)

	testCases := []struct {
		strategy      domain.LoadBalancingStrategy
		shouldContain string
	}{
		{
			strategy:      domain.LoadBalancingStrategyRoundRobin,
			shouldContain: "Round Robin",
		},
		{
			strategy:      domain.LoadBalancingStrategyWeighted,
			shouldContain: "Weighted Round Robin",
		},
		{
			strategy:      domain.LoadBalancingStrategyLeastConnection,
			shouldContain: "Least Connections",
		},
		{
			strategy:      "unknown-strategy",
			shouldContain: "Unknown strategy",
		},
	}

	for _, tc := range testCases {
		t.Run(string(tc.strategy), func(t *testing.T) {
			t.Parallel()
			description := factory.GetStrategyDescription(tc.strategy)
			assert.Contains(t, description, tc.shouldContain)
			assert.NotEmpty(t, description)
		})
	}
}

func TestFactory_CreatedLoadBalancersWork(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	factory := loadbalancer.NewFactory(logger)
	backends := createTestBackends()

	strategies := []domain.LoadBalancingStrategy{
		domain.LoadBalancingStrategyRoundRobin,
		domain.LoadBalancingStrategyWeighted,
		domain.LoadBalancingStrategyLeastConnection,
	}

	for _, strategy := range strategies {
		t.Run(string(strategy), func(t *testing.T) {
			t.Parallel()
			lb, err := factory.CreateLoadBalancer(strategy, backends)
			require.NoError(t, err)
			require.NotNil(t, lb)

			// Test that the load balancer can select backends
			backend, err := lb.NextBackend(context.Background())
			require.NoError(t, err)
			assert.NotNil(t, backend)

			// Verify backend is from our list
			found := false
			for _, b := range backends {
				if b.URL == backend.URL {
					found = true
					break
				}
			}
			assert.True(t, found, "Selected backend should be from the provided list")

			// Test backend status
			status := lb.BackendsStatus()
			assert.Len(t, status, len(backends))

			// Clean up
			err = lb.Close()
			require.NoError(t, err)
		})
	}
}

func TestFactory_MultipleInstancesAreSeparate(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	factory := loadbalancer.NewFactory(logger)
	backends := createTestBackends()

	// Create two load balancers of the same type
	lb1, err1 := factory.CreateLoadBalancer(domain.LoadBalancingStrategyRoundRobin, backends)
	require.NoError(t, err1)
	t.Cleanup(func() { lb1.Close() })

	lb2, err2 := factory.CreateLoadBalancer(domain.LoadBalancingStrategyRoundRobin, backends)
	require.NoError(t, err2)
	t.Cleanup(func() { lb2.Close() })

	// They should be different instances
	assert.NotEqual(t, lb1, lb2, "Factory should create separate instances")

	// But both should work independently
	backend1, err := lb1.NextBackend(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, backend1)

	backend2, err := lb2.NextBackend(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, backend2)
}

func BenchmarkFactory_CreateLoadBalancer(b *testing.B) {
	logger := testutils.NewMockLogger()
	factory := loadbalancer.NewFactory(logger)
	backends := createTestBackends()

	for b.Loop() {
		lb, err := factory.CreateLoadBalancer(domain.LoadBalancingStrategyRoundRobin, backends)
		if err != nil {
			b.Fatal(err)
		}
		lb.Close()
	}
}
