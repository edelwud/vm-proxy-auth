package testutils

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// MockMetricsService implements domain.MetricsService for testing.
type MockMetricsService struct{}

// RecordRequest records a request metric.
func (m *MockMetricsService) RecordRequest(context.Context, string, string, string, time.Duration, *domain.User) {
}

// RecordUpstream records an upstream metric.
func (m *MockMetricsService) RecordUpstream(context.Context, string, string, string, time.Duration, []string) {
}

// RecordQueryFilter records a query filter metric.
func (m *MockMetricsService) RecordQueryFilter(context.Context, string, int, bool, time.Duration) {}

// RecordAuthAttempt records an authentication attempt.
func (m *MockMetricsService) RecordAuthAttempt(_ context.Context, _, _ string) {}

// RecordTenantAccess records tenant access.
func (m *MockMetricsService) RecordTenantAccess(context.Context, string, string, bool) {}

// RecordUpstreamBackend records backend-specific metrics.
func (m *MockMetricsService) RecordUpstreamBackend(
	context.Context,
	string,
	string,
	string,
	string,
	time.Duration,
	[]string,
) {
}

// RecordHealthCheck records health check results.
func (m *MockMetricsService) RecordHealthCheck(context.Context, string, bool, time.Duration) {}

// RecordBackendStateChange records backend state changes.
func (m *MockMetricsService) RecordBackendStateChange(
	context.Context,
	string,
	domain.BackendState,
	domain.BackendState,
) {
}

// RecordCircuitBreakerStateChange records circuit breaker state changes.
func (m *MockMetricsService) RecordCircuitBreakerStateChange(context.Context, string, domain.CircuitBreakerState) {
}

// RecordQueueOperation records queue operations.
func (m *MockMetricsService) RecordQueueOperation(context.Context, string, time.Duration, int) {}

// RecordLoadBalancerSelection records load balancer selections.
func (m *MockMetricsService) RecordLoadBalancerSelection(
	context.Context,
	domain.LoadBalancingStrategy,
	string,
	time.Duration,
) {
}

// Handler returns the metrics HTTP handler.
func (m *MockMetricsService) Handler() http.Handler { return nil }

// MockEnhancedMetricsService implements domain.MetricsService for testing enhanced proxy with tracking capabilities.
type MockEnhancedMetricsService struct {
	mu                          sync.Mutex
	backendStateChangeCallCount int
	loadBalancerSelectionCount  int
	upstreamBackendCallCount    int
}

// RecordRequest records a request metric.
func (m *MockEnhancedMetricsService) RecordRequest(
	context.Context,
	string,
	string,
	string,
	time.Duration,
	*domain.User,
) {
}

// RecordUpstream records an upstream metric.
func (m *MockEnhancedMetricsService) RecordUpstream(context.Context, string, string, string, time.Duration, []string) {
}

// RecordQueryFilter records a query filter metric.
func (m *MockEnhancedMetricsService) RecordQueryFilter(context.Context, string, int, bool, time.Duration) {
}

// RecordAuthAttempt records an authentication attempt.
func (m *MockEnhancedMetricsService) RecordAuthAttempt(context.Context, string, string) {}

// RecordTenantAccess records tenant access.
func (m *MockEnhancedMetricsService) RecordTenantAccess(context.Context, string, string, bool) {}

// RecordUpstreamBackend records backend-specific upstream metrics.
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

// RecordHealthCheck records health check results.
func (m *MockEnhancedMetricsService) RecordHealthCheck(context.Context, string, bool, time.Duration) {
}

// RecordBackendStateChange records backend state changes.
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

// RecordCircuitBreakerStateChange records circuit breaker state changes.
func (m *MockEnhancedMetricsService) RecordCircuitBreakerStateChange(
	context.Context,
	string,
	domain.CircuitBreakerState,
) {
}

// RecordQueueOperation records queue operations.
func (m *MockEnhancedMetricsService) RecordQueueOperation(context.Context, string, time.Duration, int) {
}

// RecordLoadBalancerSelection records load balancer selections.
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

// Handler returns the metrics HTTP handler.
func (m *MockEnhancedMetricsService) Handler() http.Handler { return nil }

// GetCallCounts returns the call counts for testing.
func (m *MockEnhancedMetricsService) GetCallCounts() (int, int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.backendStateChangeCallCount, m.loadBalancerSelectionCount, m.upstreamBackendCallCount
}
