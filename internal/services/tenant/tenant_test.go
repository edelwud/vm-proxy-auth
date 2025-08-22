package tenant_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/tenant"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestService_FilterQuery_ORStrategy(t *testing.T) {
	logger := &testutils.MockLogger{}
	metrics := &MockMetricsService{}

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: true,
		TenantFilter: config.TenantFilterConfig{
			Strategy: "or_conditions", // Use secure OR strategy
		},
	}

	service := tenant.NewService(cfg, logger, metrics)

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

func TestService_FilterQuery_SingleTenant(t *testing.T) {
	logger := &testutils.MockLogger{}
	metrics := &MockMetricsService{}

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: true,
		TenantFilter: config.TenantFilterConfig{
			Strategy: "or_conditions", // OR strategy configured
		},
	}

	service := tenant.NewService(cfg, logger, metrics)

	user := &domain.User{
		ID: "test-user",
		VMTenants: []domain.VMTenant{
			{AccountID: "1000", ProjectID: "10"}, // Single tenant
		},
	}

	originalQuery := "up"
	filteredQuery, err := service.FilterQuery(context.Background(), user, originalQuery)
	require.NoError(t, err)

	t.Logf("Single tenant result: %s", filteredQuery)

	// With a single tenant, the query should now have the labels injected
	assert.NotEqual(t, originalQuery, filteredQuery, "Query should be modified for single tenant")
	assert.Contains(t, filteredQuery, "vm_account_id=\"1000\"")
	assert.Contains(t, filteredQuery, "vm_project_id=\"10\"")
	assert.NotContains(t, filteredQuery, " or ", "OR should not be used for a single tenant")
}

func TestService_FilterQuery_ComplexQuery_ORStrategy(t *testing.T) {
	logger := &testutils.MockLogger{}
	metrics := &MockMetricsService{}

	cfg := &config.UpstreamConfig{
		TenantLabel:  "vm_account_id",
		ProjectLabel: "vm_project_id",
		UseProjectID: true,
		TenantFilter: config.TenantFilterConfig{
			Strategy: "or_conditions",
		},
	}

	service := tenant.NewService(cfg, logger, metrics)

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
func (m *MockMetricsService) Handler() http.Handler                                               { return nil }
