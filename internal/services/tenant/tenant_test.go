package tenant_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config/modules/tenant"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	tenantservice "github.com/edelwud/vm-proxy-auth/internal/services/tenant"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestService_FilterQuery_ORStrategy(t *testing.T) {
	t.Parallel()
	logger := testutils.NewMockLogger()
	metrics := &MockMetricsService{}

	tenantCfg := &tenant.FilterConfig{
		Strategy: string(domain.TenantFilterStrategyOrConditions),
		Labels: tenant.LabelsConfig{
			Account:      "vm_account_id",
			Project:      "vm_project_id",
			UseProjectID: true,
		},
	}

	service := tenantservice.NewService(*tenantCfg, logger, metrics)

	user := &domain.User{
		ID: "test-user",
		VMTenants: []domain.VMTenant{
			{AccountID: "1000", ProjectID: ".*"},
			{AccountID: "2000", ProjectID: "20"},
		},
	}

	originalQuery := "up"
	filteredQuery, err := service.FilterQuery(context.Background(), user, originalQuery)
	require.NoError(t, err)

	t.Logf("OR strategy result: %s", filteredQuery)

	// Should use OR strategy (secure tenant isolation)
	assert.Contains(t, filteredQuery, "vm_account_id=\"1000\"")
	assert.Contains(t, filteredQuery, "vm_account_id=\"2000\"")
	assert.Contains(t, filteredQuery, "vm_project_id=\"20\"")
	assert.Contains(t, filteredQuery, " or ")
}

func TestService_FilterQuery_OneTenant(t *testing.T) {
	t.Parallel()
	logger := testutils.NewMockLogger()
	metrics := &MockMetricsService{}

	tenantCfg := &tenant.FilterConfig{
		Strategy: string(domain.TenantFilterStrategyOrConditions),
		Labels: tenant.LabelsConfig{
			Account:      "vm_account_id",
			Project:      "vm_project_id",
			UseProjectID: true,
		},
	}

	service := tenantservice.NewService(*tenantCfg, logger, metrics)

	user := &domain.User{
		ID: "test-user",
		VMTenants: []domain.VMTenant{
			{AccountID: "1000", ProjectID: "10"},
		},
	}

	originalQuery := "up"
	filteredQuery, err := service.FilterQuery(context.Background(), user, originalQuery)
	require.NoError(t, err)

	t.Logf("One tenant result: %s", filteredQuery)

	assert.NotEqual(t, originalQuery, filteredQuery, "Query should be modified")
	assert.Contains(t, filteredQuery, "vm_account_id=\"1000\"")
	assert.Contains(t, filteredQuery, "vm_project_id=\"10\"")
	assert.NotContains(t, filteredQuery, " or ", "OR should not be used for one tenant")
}

func TestService_FilterQuery_ComplexQuery_ORStrategy(t *testing.T) {
	t.Parallel()
	logger := testutils.NewMockLogger()
	metrics := &MockMetricsService{}

	tenantCfg := &tenant.FilterConfig{
		Strategy: string(domain.TenantFilterStrategyOrConditions),
		Labels: tenant.LabelsConfig{
			Account:      "vm_account_id",
			Project:      "vm_project_id",
			UseProjectID: true,
		},
	}

	service := tenantservice.NewService(*tenantCfg, logger, metrics)

	user := &domain.User{
		ID: "test-user",
		VMTenants: []domain.VMTenant{
			{AccountID: "1000", ProjectID: ".*"},
			{AccountID: "2000", ProjectID: "20"},
		},
	}

	// Complex query with metric division
	originalQuery := "rate(http_requests_total[5m]) / rate(http_requests_duration_seconds_count[5m])"
	filteredQuery, err := service.FilterQuery(context.Background(), user, originalQuery)
	require.NoError(t, err)

	t.Logf("Complex query OR result: %s", filteredQuery)

	// Should create secure OR conditions for each metric
	assert.Contains(t, filteredQuery, "rate(http_requests_total{vm_account_id=\"1000\"}[5m])")
	assert.Contains(t, filteredQuery, "rate(http_requests_total{vm_account_id=\"2000\",vm_project_id=\"20\"}[5m])")
	assert.Contains(t, filteredQuery, " or ")
}

// MockMetricsService implements domain.MetricsService for testing.
type MockMetricsService struct{}

func (m *MockMetricsService) RecordRequest(context.Context, string, string, string, time.Duration, *domain.User) {
}

func (m *MockMetricsService) RecordUpstream(context.Context, string, string, string, time.Duration, []string) {
}
func (m *MockMetricsService) RecordQueryFilter(context.Context, string, int, bool, time.Duration) {}
func (m *MockMetricsService) RecordAuthAttempt(context.Context, string, string)                   {}
func (m *MockMetricsService) RecordTenantAccess(context.Context, string, string, bool)            {}

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
func (m *MockMetricsService) RecordHealthCheck(context.Context, string, bool, time.Duration) {}

func (m *MockMetricsService) RecordBackendStateChange(
	context.Context,
	string,
	domain.BackendState,
	domain.BackendState,
) {
}

func (m *MockMetricsService) RecordCircuitBreakerStateChange(context.Context, string, domain.CircuitBreakerState) {
}
func (m *MockMetricsService) RecordQueueOperation(context.Context, string, time.Duration, int) {}

func (m *MockMetricsService) RecordLoadBalancerSelection(
	context.Context,
	domain.LoadBalancingStrategy,
	string,
	time.Duration,
) {
}
func (m *MockMetricsService) Handler() http.Handler { return nil }

func TestService_CanAccessTenant(t *testing.T) {
	t.Parallel()
	logger := testutils.NewMockLogger()
	metrics := &MockMetricsService{}

	tenantCfg := &tenant.FilterConfig{
		Strategy: string(domain.TenantFilterStrategyOrConditions),
		Labels: tenant.LabelsConfig{
			Account:      "vm_account_id",
			Project:      "vm_project_id",
			UseProjectID: true,
		},
	}

	service := tenantservice.NewService(*tenantCfg, logger, metrics)

	t.Run("access_allowed_by_account_id", func(t *testing.T) {
		t.Parallel()
		user := &domain.User{
			ID: "test-user",
			VMTenants: []domain.VMTenant{
				{AccountID: "1000", ProjectID: "10"},
				{AccountID: "2000", ProjectID: "20"},
			},
		}

		// Should allow access by AccountID
		canAccess := service.CanAccessTenant(context.Background(), user, "1000")
		assert.True(t, canAccess)

		canAccess = service.CanAccessTenant(context.Background(), user, "2000")
		assert.True(t, canAccess)
	})

	t.Run("access_allowed_by_tenant_string", func(t *testing.T) {
		t.Parallel()
		user := &domain.User{
			ID: "test-user",
			VMTenants: []domain.VMTenant{
				{AccountID: "1000", ProjectID: "10"},
			},
		}

		// Should allow access by full tenant string (AccountID:ProjectID)
		canAccess := service.CanAccessTenant(context.Background(), user, "1000:10")
		assert.True(t, canAccess)
	})

	t.Run("access_denied_no_match", func(t *testing.T) {
		t.Parallel()
		user := &domain.User{
			ID: "test-user",
			VMTenants: []domain.VMTenant{
				{AccountID: "1000", ProjectID: "10"},
			},
		}

		// Should deny access to non-matching tenant
		canAccess := service.CanAccessTenant(context.Background(), user, "9999")
		assert.False(t, canAccess)

		canAccess = service.CanAccessTenant(context.Background(), user, "1000:99")
		assert.False(t, canAccess)
	})

	t.Run("access_denied_no_vm_tenants", func(t *testing.T) {
		t.Parallel()
		user := &domain.User{
			ID:        "test-user",
			VMTenants: []domain.VMTenant{}, // Empty VM tenants
		}

		// Should deny access when user has no VM tenants
		canAccess := service.CanAccessTenant(context.Background(), user, "1000")
		assert.False(t, canAccess)
	})
}

func TestService_DetermineTargetTenant(t *testing.T) {
	t.Parallel()
	logger := testutils.NewMockLogger()
	metrics := &MockMetricsService{}

	tenantCfg := &tenant.FilterConfig{
		Strategy: string(domain.TenantFilterStrategyOrConditions),
		Labels: tenant.LabelsConfig{
			Account:      "vm_account_id",
			Project:      "vm_project_id",
			UseProjectID: true,
		},
	}

	service := tenantservice.NewService(*tenantCfg, logger, metrics)

	t.Run("use_tenant_from_header_x_prometheus_tenant", func(t *testing.T) {
		t.Parallel()
		user := &domain.User{
			ID: "test-user",
			VMTenants: []domain.VMTenant{
				{AccountID: "1000", ProjectID: "10"},
				{AccountID: "2000", ProjectID: "20"},
			},
		}

		req := &http.Request{
			Header: http.Header{
				"X-Prometheus-Tenant": []string{"1000"},
			},
		}

		tenant, err := service.DetermineTargetTenant(context.Background(), user, req)
		require.NoError(t, err)
		assert.Equal(t, "1000", tenant)
	})

	t.Run("use_tenant_from_header_x_tenant_id", func(t *testing.T) {
		t.Parallel()
		user := &domain.User{
			ID: "test-user",
			VMTenants: []domain.VMTenant{
				{AccountID: "1000", ProjectID: "10"},
			},
		}

		req := &http.Request{
			Header: http.Header{
				"X-Tenant-ID": []string{"1000:10"},
			},
		}

		tenant, err := service.DetermineTargetTenant(context.Background(), user, req)
		require.NoError(t, err)
		assert.Equal(t, "1000:10", tenant)
	})

	t.Run("forbidden_tenant_in_header", func(t *testing.T) {
		t.Parallel()
		user := &domain.User{
			ID: "test-user",
			VMTenants: []domain.VMTenant{
				{AccountID: "1000", ProjectID: "10"},
			},
		}

		req := &http.Request{
			Header: http.Header{
				"X-Prometheus-Tenant": []string{"9999"}, // Tenant user cannot access
			},
		}

		tenant, err := service.DetermineTargetTenant(context.Background(), user, req)
		require.Error(t, err)
		assert.Empty(t, tenant)

		var appErr *domain.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, domain.ErrCodeForbidden, appErr.Code)
		assert.Equal(t, http.StatusForbidden, appErr.HTTPStatus)
		assert.Contains(t, appErr.Message, "User cannot access tenant: 9999")
	})

	t.Run("use_first_vm_tenant_default", func(t *testing.T) {
		t.Parallel()
		user := &domain.User{
			ID: "test-user",
			VMTenants: []domain.VMTenant{
				{AccountID: "1000", ProjectID: "10"},
				{AccountID: "2000", ProjectID: "20"},
			},
		}

		req := &http.Request{
			Header: http.Header{}, // No tenant headers
		}

		tenant, err := service.DetermineTargetTenant(context.Background(), user, req)
		require.NoError(t, err)
		assert.Equal(t, "1000:10", tenant) // Should use first VM tenant
	})

	t.Run("error_no_vm_tenants", func(t *testing.T) {
		t.Parallel()
		user := &domain.User{
			ID:        "test-user",
			VMTenants: []domain.VMTenant{}, // No VM tenants
		}

		req := &http.Request{
			Header: http.Header{},
		}

		tenant, err := service.DetermineTargetTenant(context.Background(), user, req)
		require.Error(t, err)
		assert.Empty(t, tenant)
		assert.Equal(t, domain.ErrNoVMTenants, err)
	})
}
