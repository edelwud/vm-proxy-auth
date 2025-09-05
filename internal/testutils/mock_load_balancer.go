package testutils

import (
	"context"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// MockLoadBalancer is a mock implementation of domain.LoadBalancer for testing.
type MockLoadBalancer struct {
	NextBackendFunc    func(ctx context.Context) (*domain.Backend, error)
	ReportResultFunc   func(backend *domain.Backend, err error, statusCode int)
	BackendsStatusFunc func() []*domain.BackendStatus
	CloseFunc          func() error
}

// NextBackend returns the next backend to use for load balancing.
func (m *MockLoadBalancer) NextBackend(ctx context.Context) (*domain.Backend, error) {
	if m.NextBackendFunc != nil {
		return m.NextBackendFunc(ctx)
	}
	return nil, domain.ErrNoHealthyBackends
}

// ReportResult reports the result of using a backend.
func (m *MockLoadBalancer) ReportResult(backend *domain.Backend, err error, statusCode int) {
	if m.ReportResultFunc != nil {
		m.ReportResultFunc(backend, err, statusCode)
	}
}

// BackendsStatus returns the status of all backends.
func (m *MockLoadBalancer) BackendsStatus() []*domain.BackendStatus {
	if m.BackendsStatusFunc != nil {
		return m.BackendsStatusFunc()
	}
	return nil
}

// Close closes the load balancer.
func (m *MockLoadBalancer) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}
