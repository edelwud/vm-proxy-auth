package testutils

import (
	"context"
	"net/http"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

const (
	// DefaultTestQueryFilterDuration is the default duration for query filter operations.
	DefaultTestQueryFilterDuration = 10 * time.Millisecond
	// DefaultTestUpstreamDuration is the default duration for upstream operations.
	DefaultTestUpstreamDuration = 50 * time.Millisecond
)

// MockAccessService is a mock implementation for testing access control.
type MockAccessService struct {
	Err error
}

// CanAccess checks if a user can access a resource.
func (m *MockAccessService) CanAccess(_ context.Context, _ *domain.User, _, _ string) error {
	return m.Err
}

// TestableMetricsCollector collects metrics calls for verification.
type TestableMetricsCollector struct {
	RequestCalls      []RequestCall
	UpstreamCalls     []UpstreamCall
	QueryFilterCalls  []QueryFilterCall
	AuthAttemptCalls  []AuthAttemptCall
	TenantAccessCalls []TenantAccessCall
}

// RequestCall represents a request metrics call.
type RequestCall struct {
	Method   string
	Path     string
	Status   string
	Duration time.Duration
	User     *domain.User
}

// UpstreamCall represents an upstream metrics call.
type UpstreamCall struct {
	Method   string
	Path     string
	Status   string
	Duration time.Duration
	Tenants  []string
}

// QueryFilterCall represents a query filter metrics call.
type QueryFilterCall struct {
	UserID        string
	TenantCount   int
	FilterApplied bool
	Duration      time.Duration
}

// AuthAttemptCall represents an auth attempt metrics call.
type AuthAttemptCall struct {
	UserID string
	Status string
}

// TenantAccessCall represents a tenant access metrics call.
type TenantAccessCall struct {
	UserID   string
	TenantID string
	Allowed  bool
}

// RecordRequest records a request metric call.
func (t *TestableMetricsCollector) RecordRequest(
	_ context.Context, method, path, status string, duration time.Duration, user *domain.User,
) {
	t.RequestCalls = append(t.RequestCalls, RequestCall{
		Method:   method,
		Path:     path,
		Status:   status,
		Duration: duration,
		User:     user,
	})
}

// RecordUpstream records an upstream metric call.
func (t *TestableMetricsCollector) RecordUpstream(
	_ context.Context, method, path, status string, duration time.Duration, tenants []string,
) {
	t.UpstreamCalls = append(t.UpstreamCalls, UpstreamCall{
		Method:   method,
		Path:     path,
		Status:   status,
		Duration: duration,
		Tenants:  tenants,
	})
}

// RecordQueryFilter records a query filter metric call.
func (t *TestableMetricsCollector) RecordQueryFilter(
	_ context.Context, userID string, tenantCount int, filterApplied bool, duration time.Duration,
) {
	t.QueryFilterCalls = append(t.QueryFilterCalls, QueryFilterCall{
		UserID:        userID,
		TenantCount:   tenantCount,
		FilterApplied: filterApplied,
		Duration:      duration,
	})
}

// RecordAuthAttempt records an auth attempt metric call.
func (t *TestableMetricsCollector) RecordAuthAttempt(_ context.Context, userID, status string) {
	t.AuthAttemptCalls = append(t.AuthAttemptCalls, AuthAttemptCall{
		UserID: userID,
		Status: status,
	})
}

// RecordTenantAccess records a tenant access metric call.
func (t *TestableMetricsCollector) RecordTenantAccess(_ context.Context, userID, tenantID string, allowed bool) {
	t.TenantAccessCalls = append(t.TenantAccessCalls, TenantAccessCall{
		UserID:   userID,
		TenantID: tenantID,
		Allowed:  allowed,
	})
}

// RecordUpstreamBackend records upstream backend metrics (no-op for testing).
func (t *TestableMetricsCollector) RecordUpstreamBackend(
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
func (t *TestableMetricsCollector) RecordHealthCheck(context.Context, string, bool, time.Duration) {}

// RecordBackendStateChange records backend state changes.
func (t *TestableMetricsCollector) RecordBackendStateChange(
	context.Context,
	string,
	domain.BackendState,
	domain.BackendState,
) {
}

// RecordCircuitBreakerStateChange records circuit breaker state changes.
func (t *TestableMetricsCollector) RecordCircuitBreakerStateChange(
	context.Context,
	string,
	domain.CircuitBreakerState,
) {
}

// RecordQueueOperation records queue operations.
func (t *TestableMetricsCollector) RecordQueueOperation(context.Context, string, time.Duration, int) {
}

// RecordLoadBalancerSelection records load balancer selections.
func (t *TestableMetricsCollector) RecordLoadBalancerSelection(
	context.Context,
	domain.LoadBalancingStrategy,
	string,
	time.Duration,
) {
}

// Handler returns the metrics HTTP handler.
func (t *TestableMetricsCollector) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("# Mock metrics endpoint\n")); err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
		}
	})
}

// TestableAuthService is a mock auth service that uses the testable metrics collector.
type TestableAuthService struct {
	User    *domain.User
	Err     error
	Metrics *TestableMetricsCollector
}

// Authenticate authenticates a user and records metrics.
func (m *TestableAuthService) Authenticate(ctx context.Context, _ string) (*domain.User, error) {
	if m.Err != nil {
		m.Metrics.RecordAuthAttempt(ctx, "unknown", "failed")
		return nil, m.Err
	}

	m.Metrics.RecordAuthAttempt(ctx, m.User.ID, "success")
	return m.User, nil
}

// TestableTenantService is a mock tenant service that uses the testable metrics collector.
type TestableTenantService struct {
	FilteredQuery string
	CanAccess     bool
	TargetTenant  string
	Err           error
	Metrics       *TestableMetricsCollector
}

// FilterQuery filters a query and records metrics.
func (m *TestableTenantService) FilterQuery(ctx context.Context, user *domain.User, query string) (string, error) {
	// Simulate metrics recording
	filterApplied := query != m.FilteredQuery
	m.Metrics.RecordQueryFilter(ctx, user.ID, len(user.VMTenants), filterApplied, DefaultTestQueryFilterDuration)

	if m.Err != nil {
		return "", m.Err
	}

	return m.FilteredQuery, nil
}

// CanAccessTenant checks if a user can access a tenant and records metrics.
func (m *TestableTenantService) CanAccessTenant(ctx context.Context, user *domain.User, tenantID string) bool {
	m.Metrics.RecordTenantAccess(ctx, user.ID, tenantID, m.CanAccess)
	return m.CanAccess
}

// DetermineTargetTenant determines the target tenant for a request.
func (m *TestableTenantService) DetermineTargetTenant(
	_ context.Context,
	_ *domain.User,
	_ *http.Request,
) (string, error) {
	if m.Err != nil {
		return "", m.Err
	}

	return m.TargetTenant, nil
}

// TestableProxyService is a mock proxy service that uses the testable metrics collector.
type TestableProxyService struct {
	Response *domain.ProxyResponse
	Err      error
	Metrics  *TestableMetricsCollector
}

// Forward forwards a request and records metrics.
func (m *TestableProxyService) Forward(ctx context.Context, req *domain.ProxyRequest) (*domain.ProxyResponse, error) {
	// Simulate upstream metrics recording
	if m.Response != nil {
		tenants := make([]string, len(req.User.VMTenants))
		for i, tenant := range req.User.VMTenants {
			tenants[i] = tenant.String()
		}

		m.Metrics.RecordUpstream(
			ctx,
			req.OriginalRequest.Method,
			req.OriginalRequest.URL.Path,
			http.StatusText(m.Response.StatusCode),
			DefaultTestUpstreamDuration,
			tenants,
		)
	}

	if m.Err != nil {
		return nil, m.Err
	}

	return m.Response, nil
}

// GetBackendsStatus returns mock backend status for testing.
func (m *TestableProxyService) GetBackendsStatus() []*domain.BackendStatus {
	return []*domain.BackendStatus{
		{
			Backend: domain.Backend{
				URL:    "http://localhost:8080",
				Weight: 1,
				State:  domain.BackendHealthy,
			},
			IsHealthy: true,
			LastCheck: time.Now(),
		},
	}
}

// SetMaintenanceMode is a no-op for testing.
func (m *TestableProxyService) SetMaintenanceMode(_ string, _ bool) error {
	return nil
}
