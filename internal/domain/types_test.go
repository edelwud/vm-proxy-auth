package domain_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

func TestBackendState_String(t *testing.T) {
	tests := []struct {
		name     string
		state    domain.BackendState
		expected string
	}{
		{
			name:     "healthy state",
			state:    domain.BackendHealthy,
			expected: "healthy",
		},
		{
			name:     "unhealthy state",
			state:    domain.BackendUnhealthy,
			expected: "unhealthy",
		},
		{
			name:     "rate limited state",
			state:    domain.BackendRateLimited,
			expected: "rate-limited",
		},
		{
			name:     "maintenance state",
			state:    domain.BackendMaintenance,
			expected: "maintenance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

func TestBackendState_IsAvailable(t *testing.T) {
	tests := []struct {
		name      string
		state     domain.BackendState
		available bool
	}{
		{
			name:      "healthy is available",
			state:     domain.BackendHealthy,
			available: true,
		},
		{
			name:      "unhealthy is not available",
			state:     domain.BackendUnhealthy,
			available: false,
		},
		{
			name:      "rate limited is not available",
			state:     domain.BackendRateLimited,
			available: false,
		},
		{
			name:      "maintenance is not available",
			state:     domain.BackendMaintenance,
			available: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.available, tt.state.IsAvailable())
		})
	}
}

func TestBackendState_IsAvailableWithFallback(t *testing.T) {
	tests := []struct {
		name      string
		state     domain.BackendState
		available bool
	}{
		{
			name:      "healthy is available",
			state:     domain.BackendHealthy,
			available: true,
		},
		{
			name:      "unhealthy is not available",
			state:     domain.BackendUnhealthy,
			available: false,
		},
		{
			name:      "rate limited is available as fallback",
			state:     domain.BackendRateLimited,
			available: true,
		},
		{
			name:      "maintenance is not available",
			state:     domain.BackendMaintenance,
			available: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.available, tt.state.IsAvailableWithFallback())
		})
	}
}

func TestBackend_String(t *testing.T) {
	backend := domain.Backend{
		URL:    "https://vmselect-1.example.com",
		Weight: 2,
		State:  domain.BackendHealthy,
	}

	result := backend.String()
	expected := "Backend{URL: https://vmselect-1.example.com, Weight: 2, State: healthy}"

	assert.Equal(t, expected, result)
}

func TestLoadBalancingStrategy_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		strategy domain.LoadBalancingStrategy
		valid    bool
	}{
		{
			name:     "round robin is valid",
			strategy: domain.LoadBalancingRoundRobin,
			valid:    true,
		},
		{
			name:     "weighted round robin is valid",
			strategy: domain.LoadBalancingWeightedRoundRobin,
			valid:    true,
		},
		{
			name:     "least connections is valid",
			strategy: domain.LoadBalancingLeastConnections,
			valid:    true,
		},
		{
			name:     "invalid strategy",
			strategy: domain.LoadBalancingStrategy("invalid"),
			valid:    false,
		},
		{
			name:     "empty strategy",
			strategy: domain.LoadBalancingStrategy(""),
			valid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, tt.strategy.IsValid())
		})
	}
}

func TestCircuitBreakerState_String(t *testing.T) {
	tests := []struct {
		name     string
		state    domain.CircuitBreakerState
		expected string
	}{
		{
			name:     "closed state",
			state:    domain.CircuitClosed,
			expected: "closed",
		},
		{
			name:     "open state",
			state:    domain.CircuitOpen,
			expected: "open",
		},
		{
			name:     "half open state",
			state:    domain.CircuitHalfOpen,
			expected: "half-open",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

func TestStateEventType_String(t *testing.T) {
	tests := []struct {
		name      string
		eventType domain.StateEventType
		expected  string
	}{
		{
			name:      "set event",
			eventType: domain.StateEventSet,
			expected:  "SET",
		},
		{
			name:      "delete event",
			eventType: domain.StateEventDelete,
			expected:  "DELETE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.eventType.String())
		})
	}
}

func TestBackendStatus_IsHealthy(t *testing.T) {
	tests := []struct {
		name     string
		status   domain.BackendStatus
		expected bool
	}{
		{
			name: "healthy backend",
			status: domain.BackendStatus{
				Backend:     domain.Backend{URL: "http://backend1.com", Weight: 1, State: domain.BackendHealthy},
				IsHealthy:   true,
				ActiveConns: 5,
			},
			expected: true,
		},
		{
			name: "unhealthy backend",
			status: domain.BackendStatus{
				Backend:     domain.Backend{URL: "http://backend2.com", Weight: 1, State: domain.BackendUnhealthy},
				IsHealthy:   false,
				ActiveConns: 0,
			},
			expected: false,
		},
		{
			name: "rate limited backend",
			status: domain.BackendStatus{
				Backend:     domain.Backend{URL: "http://backend3.com", Weight: 1, State: domain.BackendRateLimited},
				IsHealthy:   false,
				ActiveConns: 2,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status.IsHealthy)
		})
	}
}

func TestStateEvent_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		event    domain.StateEvent
		expected bool
	}{
		{
			name: "valid set event",
			event: domain.StateEvent{
				Type:      domain.StateEventSet,
				Key:       "backend:health:test",
				Value:     []byte("test-data"),
				Timestamp: time.Now(),
				NodeID:    "node-1",
			},
			expected: true,
		},
		{
			name: "valid delete event",
			event: domain.StateEvent{
				Type:      domain.StateEventDelete,
				Key:       "backend:health:test",
				Timestamp: time.Now(),
				NodeID:    "node-1",
			},
			expected: true,
		},
		{
			name: "invalid event - empty key",
			event: domain.StateEvent{
				Type:      domain.StateEventSet,
				Key:       "",
				Value:     []byte("test-data"),
				Timestamp: time.Now(),
				NodeID:    "node-1",
			},
			expected: false,
		},
		{
			name: "invalid event - empty node ID",
			event: domain.StateEvent{
				Type:      domain.StateEventSet,
				Key:       "backend:health:test",
				Value:     []byte("test-data"),
				Timestamp: time.Now(),
				NodeID:    "",
			},
			expected: false,
		},
		{
			name: "invalid event - zero timestamp",
			event: domain.StateEvent{
				Type:   domain.StateEventSet,
				Key:    "backend:health:test",
				Value:  []byte("test-data"),
				NodeID: "node-1",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.event.IsValid())
		})
	}
}

// Mock implementations for testing.
type mockLoadBalancer struct {
	nextBackendFunc    func(ctx context.Context) (*domain.Backend, error)
	reportResultFunc   func(backend *domain.Backend, err error, statusCode int)
	backendsStatusFunc func() []*domain.BackendStatus
	closeFunc          func() error
}

func (m *mockLoadBalancer) NextBackend(ctx context.Context) (*domain.Backend, error) {
	if m.nextBackendFunc != nil {
		return m.nextBackendFunc(ctx)
	}
	return nil, domain.ErrNoHealthyBackends
}

func (m *mockLoadBalancer) ReportResult(backend *domain.Backend, err error, statusCode int) {
	if m.reportResultFunc != nil {
		m.reportResultFunc(backend, err, statusCode)
	}
}

func (m *mockLoadBalancer) BackendsStatus() []*domain.BackendStatus {
	if m.backendsStatusFunc != nil {
		return m.backendsStatusFunc()
	}
	return nil
}

func (m *mockLoadBalancer) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func TestLoadBalancerInterface(t *testing.T) {
	backend := &domain.Backend{
		URL:    "http://test-backend.com",
		Weight: 1,
		State:  domain.BackendHealthy,
	}

	status := &domain.BackendStatus{
		Backend:     *backend,
		IsHealthy:   true,
		ActiveConns: 0,
		LastCheck:   time.Now(),
		ErrorCount:  0,
	}

	// Test NextBackend
	lb := &mockLoadBalancer{
		nextBackendFunc: func(_ context.Context) (*domain.Backend, error) {
			return backend, nil
		},
	}

	ctx := context.Background()
	result, err := lb.NextBackend(ctx)
	require.NoError(t, err)
	assert.Equal(t, backend, result)

	// Test ReportResult
	reportCalled := false
	lb.reportResultFunc = func(b *domain.Backend, err error, statusCode int) {
		assert.Equal(t, backend, b)
		assert.NoError(t, err)
		assert.Equal(t, 200, statusCode)
		reportCalled = true
	}

	lb.ReportResult(backend, nil, 200)
	assert.True(t, reportCalled)

	// Test BackendsStatus
	lb.backendsStatusFunc = func() []*domain.BackendStatus {
		return []*domain.BackendStatus{status}
	}

	statuses := lb.BackendsStatus()
	require.Len(t, statuses, 1)
	assert.Equal(t, status, statuses[0])

	// Test Close
	closeCalled := false
	lb.closeFunc = func() error {
		closeCalled = true
		return nil
	}

	err = lb.Close()
	assert.NoError(t, err)
	assert.True(t, closeCalled)
}

// Mock health checker for testing.
type mockHealthChecker struct {
	checkHealthFunc     func(ctx context.Context, backend *domain.Backend) error
	startMonitoringFunc func(ctx context.Context) error
	stopFunc            func() error
}

func (m *mockHealthChecker) CheckHealth(ctx context.Context, backend *domain.Backend) error {
	if m.checkHealthFunc != nil {
		return m.checkHealthFunc(ctx, backend)
	}
	return nil
}

func (m *mockHealthChecker) StartMonitoring(ctx context.Context) error {
	if m.startMonitoringFunc != nil {
		return m.startMonitoringFunc(ctx)
	}
	return nil
}

func (m *mockHealthChecker) Stop() error {
	if m.stopFunc != nil {
		return m.stopFunc()
	}
	return nil
}

func TestHealthCheckerInterface(t *testing.T) {
	backend := &domain.Backend{
		URL:    "http://test-backend.com",
		Weight: 1,
		State:  domain.BackendHealthy,
	}

	// Test CheckHealth
	hc := &mockHealthChecker{
		checkHealthFunc: func(_ context.Context, b *domain.Backend) error {
			assert.Equal(t, backend, b)
			return nil
		},
	}

	ctx := context.Background()
	err := hc.CheckHealth(ctx, backend)
	assert.NoError(t, err)

	// Test StartMonitoring
	monitoringStarted := false
	hc.startMonitoringFunc = func(_ context.Context) error {
		monitoringStarted = true
		return nil
	}

	err = hc.StartMonitoring(ctx)
	assert.NoError(t, err)
	assert.True(t, monitoringStarted)

	// Test Stop
	stopped := false
	hc.stopFunc = func() error {
		stopped = true
		return nil
	}

	err = hc.Stop()
	assert.NoError(t, err)
	assert.True(t, stopped)
}
