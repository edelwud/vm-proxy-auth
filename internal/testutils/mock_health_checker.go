package testutils

import (
	"context"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// MockHealthChecker is a mock implementation of domain.HealthChecker for testing.
type MockHealthChecker struct {
	CheckHealthFunc     func(ctx context.Context, backend *domain.Backend) error
	StartMonitoringFunc func(ctx context.Context) error
	StopFunc            func() error
}

// CheckHealth checks the health of a backend.
func (m *MockHealthChecker) CheckHealth(ctx context.Context, backend *domain.Backend) error {
	if m.CheckHealthFunc != nil {
		return m.CheckHealthFunc(ctx, backend)
	}
	return nil
}

// StartMonitoring starts health monitoring.
func (m *MockHealthChecker) StartMonitoring(ctx context.Context) error {
	if m.StartMonitoringFunc != nil {
		return m.StartMonitoringFunc(ctx)
	}
	return nil
}

// Stop stops the health checker.
func (m *MockHealthChecker) Stop() error {
	if m.StopFunc != nil {
		return m.StopFunc()
	}
	return nil
}
