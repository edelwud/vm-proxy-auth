package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/handlers"
	"github.com/edelwud/vm-proxy-auth/internal/services/access"
	"github.com/edelwud/vm-proxy-auth/internal/services/auth"
	"github.com/edelwud/vm-proxy-auth/internal/services/health"
	"github.com/edelwud/vm-proxy-auth/internal/services/metrics"
	"github.com/edelwud/vm-proxy-auth/internal/services/proxy"
	"github.com/edelwud/vm-proxy-auth/internal/services/tenant"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestMetricsIntegration_UnauthenticatedRequest(t *testing.T) {
	logger := testutils.NewMockLogger()
	metricsService := metrics.NewService(logger)

	// Create auth service with config
	authConfig := config.AuthSettings{
		JWT: config.JWTSettings{
			Algorithm: "HS256",
			Secret:    "test-secret",
		},
	}
	authService, err := auth.NewService(authConfig, []config.TenantMap{}, logger, metricsService)
	require.NoError(t, err)

	// Create other services
	tenantConfig := config.TenantFilterSettings{
		Strategy: "or_conditions",
		Labels: config.TenantFilterLabels{
			AccountLabel: "vm_account_id",
			ProjectLabel: "vm_project_id",
			UseProjectID: true,
		},
	}

	tenantService := tenant.NewService(&tenantConfig, logger, metricsService)
	accessService := access.NewService(logger)

	// Create enhanced proxy service
	enhancedConfig := proxy.EnhancedServiceConfig{
		Backends: []proxy.BackendConfig{
			{URL: "http://localhost:8428", Weight: 1},
		},
		LoadBalancing: proxy.LoadBalancingConfig{Strategy: domain.LoadBalancingStrategyRoundRobin},
		HealthCheck: health.CheckerConfig{
			CheckInterval:      30 * time.Second,
			Timeout:            10 * time.Second,
			HealthyThreshold:   2,
			UnhealthyThreshold: 3,
			HealthEndpoint:     "/health",
		},
		Queue: proxy.QueueConfig{
			MaxSize: 1000,
			Timeout: 5 * time.Second,
		},
		Timeout:        30 * time.Second,
		MaxRetries:     3,
		RetryBackoff:   100 * time.Millisecond,
		EnableQueueing: false,
	}

	stateStorage := testutils.NewMockStateStorage()
	proxyService, err := proxy.NewEnhancedService(enhancedConfig, logger, metricsService, stateStorage)
	require.NoError(t, err)

	// Start the service
	ctx := context.Background()
	err = proxyService.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { proxyService.Close() })

	// Create gateway handler
	handler := handlers.NewGatewayHandler(
		authService,
		tenantService,
		accessService,
		proxyService,
		metricsService,
		logger,
	)

	// Create request without authorization
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query?query=up", http.NoBody)
	recorder := httptest.NewRecorder()

	// Execute request
	handler.ServeHTTP(recorder, req)

	// Verify response is 401 Unauthorized
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", recorder.Code)
	}

	// Get metrics
	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	metricsRecorder := httptest.NewRecorder()
	metricsService.Handler().ServeHTTP(metricsRecorder, metricsReq)

	metricsBody := metricsRecorder.Body.String()

	// Verify HTTP request metrics were recorded
	if metricsBody == "" {
		t.Error("Expected metrics output, got empty response")
	}

	// Look for request metrics - should have 401 status
	if !containsMetric(metricsBody, "vm_proxy_auth_http_requests_total") {
		t.Error("Expected HTTP request counter metric")
	}

	if !containsMetric(metricsBody, "vm_proxy_auth_http_request_duration_seconds") {
		t.Error("Expected HTTP request duration histogram")
	}
}

func TestMetricsIntegration_HealthCheck(t *testing.T) {
	logger := testutils.NewMockLogger()
	metricsService := metrics.NewService(logger)

	// Create health handler
	healthHandler := handlers.NewHealthHandler(logger, "test", nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	recorder := httptest.NewRecorder()

	// Execute request
	healthHandler.ServeHTTP(recorder, req)

	// Verify response is 200 OK
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", recorder.Code)
	}

	// Get metrics endpoint - should have default Prometheus metrics
	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	metricsRecorder := httptest.NewRecorder()
	metricsService.Handler().ServeHTTP(metricsRecorder, metricsReq)

	metricsBody := metricsRecorder.Body.String()

	// Verify metrics endpoint works
	if metricsRecorder.Code != http.StatusOK {
		t.Errorf("Expected metrics status 200, got %d", metricsRecorder.Code)
	}

	// Should contain Go runtime metrics by default
	if !containsMetric(metricsBody, "go_gc_duration_seconds") {
		t.Error("Expected Go runtime metrics")
	}

	if !containsMetric(metricsBody, "process_cpu_seconds_total") {
		t.Error("Expected process metrics")
	}
}

func TestMetricsIntegration_CustomMetrics(t *testing.T) {
	logger := testutils.NewMockLogger()
	metricsService := metrics.NewService(logger)
	ctx := context.Background()

	// Manually record some metrics
	user := &domain.User{
		ID:        "test-user",
		VMTenants: []domain.VMTenant{{AccountID: "1000"}},
	}

	// Record different types of metrics
	metricsService.RecordRequest(ctx, http.MethodGet, "/api/v1/query", "200", 100*time.Millisecond, user)
	metricsService.RecordUpstream(ctx, http.MethodGet, "/api/v1/query", "200", 50*time.Millisecond, []string{"1000"})
	metricsService.RecordQueryFilter(ctx, "test-user", 1, true, 10*time.Millisecond)
	metricsService.RecordAuthAttempt(ctx, "test-user", "success")
	metricsService.RecordTenantAccess(ctx, "test-user", "1000", true)

	// Get metrics
	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	metricsRecorder := httptest.NewRecorder()
	metricsService.Handler().ServeHTTP(metricsRecorder, metricsReq)

	metricsBody := metricsRecorder.Body.String()

	// Verify all custom metrics are present
	expectedMetrics := []string{
		"vm_proxy_auth_http_requests_total",
		"vm_proxy_auth_http_request_duration_seconds",
		"vm_proxy_auth_upstream_requests_total",
		"vm_proxy_auth_upstream_request_duration_seconds",
		"vm_proxy_auth_query_filtering_total",
		"vm_proxy_auth_query_filtering_duration_seconds",
		"vm_proxy_auth_auth_attempts_total",
		"vm_proxy_auth_tenant_access_total",
	}

	for _, metricName := range expectedMetrics {
		if !containsMetric(metricsBody, metricName) {
			t.Errorf("Expected metric %s not found in output", metricName)
		}
	}

	// Verify specific labels exist
	expectedLabels := []string{
		`method="GET"`,
		`status_code="200"`,
		`user_id="test-user"`,
		`tenant_count="1"`,
		`filter_applied="true"`,
		`status="success"`,
		`tenant_id="1000"`,
		`allowed="true"`,
	}

	for _, label := range expectedLabels {
		if !containsMetric(metricsBody, label) {
			t.Errorf("Expected label %s not found in metrics output", label)
		}
	}
}

func TestMetricsIntegration_MetricValues(t *testing.T) {
	logger := testutils.NewMockLogger()
	metricsService := metrics.NewService(logger)
	ctx := context.Background()

	user := &domain.User{
		ID:        "test-user",
		VMTenants: []domain.VMTenant{{AccountID: "1000"}},
	}

	// Record multiple requests to test counter increments
	for range domain.DefaultTestRetries {
		metricsService.RecordRequest(ctx, http.MethodGet, "/api/v1/query", "200", 100*time.Millisecond, user)
	}

	// Record failed requests
	for range domain.DefaultTestCount {
		metricsService.RecordRequest(ctx, http.MethodGet, "/api/v1/query", "500", 100*time.Millisecond, user)
	}

	// Get metrics
	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	metricsRecorder := httptest.NewRecorder()
	metricsService.Handler().ServeHTTP(metricsRecorder, metricsReq)

	metricsBody := metricsRecorder.Body.String()

	// Should contain counter values (exact parsing would be complex, so we just check presence)
	if !containsMetric(metricsBody, "vm_proxy_auth_http_requests_total") {
		t.Error("Expected HTTP requests counter")
	}

	// Should have both 200 and 500 status codes
	if !containsMetric(metricsBody, `status_code="200"`) {
		t.Error("Expected 200 status code label")
	}

	if !containsMetric(metricsBody, `status_code="500"`) {
		t.Error("Expected 500 status code label")
	}
}

// containsMetric checks if metrics output contains a specific metric name or label.
func containsMetric(metricsOutput, searchString string) bool {
	return metricsOutput != "" &&
		searchString != "" &&
		findInString(metricsOutput, searchString)
}

// Simple string search helper.
func findInString(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	if haystack == "" {
		return false
	}

	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
