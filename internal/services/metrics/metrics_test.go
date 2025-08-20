package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// mockLogger implements domain.Logger for testing
type mockLogger struct{}

func (m *mockLogger) Debug(msg string, fields ...domain.Field) {}
func (m *mockLogger) Info(msg string, fields ...domain.Field)  {}
func (m *mockLogger) Warn(msg string, fields ...domain.Field)  {}
func (m *mockLogger) Error(msg string, fields ...domain.Field) {}
func (m *mockLogger) With(fields ...domain.Field) domain.Logger { return m }

// TestableMetricsService wraps the real service for better testing
type TestableMetricsService struct {
	*Service
	registry *prometheus.Registry
}

// NewTestableService creates a metrics service with isolated registry for testing
func NewTestableService() *TestableMetricsService {
	logger := &mockLogger{}
	
	// Create isolated registry for test
	registry := prometheus.NewRegistry()
	
	// Create new metric instances for this test
	testHTTPRequestsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vm_proxy_auth_http_requests_total",
			Help: "Total number of HTTP requests processed by vm-proxy-auth",
		},
		[]string{"method", "path", "status_code", "user_id"},
	)

	testHTTPRequestDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vm_proxy_auth_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "status_code"},
	)

	testUpstreamRequestsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vm_proxy_auth_upstream_requests_total",
			Help: "Total number of upstream requests to VictoriaMetrics",
		},
		[]string{"method", "path", "status_code", "tenant_count"},
	)

	testUpstreamRequestDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vm_proxy_auth_upstream_request_duration_seconds",
			Help:    "Upstream request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "status_code"},
	)

	testAuthAttemptsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vm_proxy_auth_auth_attempts_total",
			Help: "Total number of authentication attempts",
		},
		[]string{"status", "user_id"},
	)

	testQueryFilteringTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vm_proxy_auth_query_filtering_total",
			Help: "Total number of PromQL queries processed for tenant filtering",
		},
		[]string{"user_id", "tenant_count", "filter_applied"},
	)

	testQueryFilteringDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vm_proxy_auth_query_filtering_duration_seconds",
			Help:    "Time spent filtering PromQL queries",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
		},
		[]string{"user_id"},
	)

	testTenantAccessTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vm_proxy_auth_tenant_access_total",
			Help: "Total number of tenant access checks",
		},
		[]string{"user_id", "tenant_id", "allowed"},
	)

	// Register test metrics
	registry.MustRegister(
		testHTTPRequestsTotal,
		testHTTPRequestDuration,
		testUpstreamRequestsTotal,
		testUpstreamRequestDuration,
		testAuthAttemptsTotal,
		testQueryFilteringTotal,
		testQueryFilteringDuration,
		testTenantAccessTotal,
	)

	// Override global metrics with test metrics
	httpRequestsTotal = testHTTPRequestsTotal
	httpRequestDuration = testHTTPRequestDuration
	upstreamRequestsTotal = testUpstreamRequestsTotal
	upstreamRequestDuration = testUpstreamRequestDuration
	authAttemptsTotal = testAuthAttemptsTotal
	queryFilteringTotal = testQueryFilteringTotal
	queryFilteringDuration = testQueryFilteringDuration
	tenantAccessTotal = testTenantAccessTotal

	service := &Service{
		logger:   logger,
		registry: registry,
	}

	return &TestableMetricsService{
		Service:  service,
		registry: registry,
	}
}

// GetMetricValue gets the value of a counter metric for testing
func (t *TestableMetricsService) GetMetricValue(metricName string, labels prometheus.Labels) (float64, error) {
	gauge := prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: metricName}, func() float64 {
		// This is a simplified approach - in reality you'd query the actual collector
		return 0
	})
	return testutil.ToFloat64(gauge), nil
}

// GetMetricsOutput returns the metrics in Prometheus text format
func (t *TestableMetricsService) GetMetricsOutput() (string, error) {
	req := httptest.NewRequest("GET", "/metrics", nil)
	recorder := httptest.NewRecorder()
	
	t.Handler().ServeHTTP(recorder, req)
	
	if recorder.Code != http.StatusOK {
		return "", nil
	}
	
	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		return "", err
	}
	
	return string(body), nil
}

func TestMetricsWithTestRegistry(t *testing.T) {
	service := NewTestableService()
	ctx := context.Background()

	user := &domain.User{
		ID:    "test-user",
		Email: "test@example.com",
	}

	// Record a request
	service.RecordRequest(ctx, "GET", "/api/v1/query", "200", 100*time.Millisecond, user)

	// Get metrics output
	output, err := service.GetMetricsOutput()
	if err != nil {
		t.Fatalf("Failed to get metrics output: %v", err)
	}

	// Verify metrics are present
	if !strings.Contains(output, "vm_proxy_auth_http_requests_total") {
		t.Error("Expected HTTP request counter metric")
	}

	if !strings.Contains(output, `method="GET"`) {
		t.Error("Expected method label")
	}

	if !strings.Contains(output, `user_id="test-user"`) {
		t.Error("Expected user_id label")
	}
}

func TestUpstreamMetricsWithTestRegistry(t *testing.T) {
	service := NewTestableService()
	ctx := context.Background()

	tenants := []string{"1000", "1001"}

	// Record upstream request
	service.RecordUpstream(ctx, "GET", "/api/v1/query", "200", 200*time.Millisecond, tenants)

	// Get metrics output
	output, err := service.GetMetricsOutput()
	if err != nil {
		t.Fatalf("Failed to get metrics output: %v", err)
	}

	// Verify upstream metrics
	if !strings.Contains(output, "vm_proxy_auth_upstream_requests_total") {
		t.Error("Expected upstream request counter metric")
	}

	if !strings.Contains(output, `tenant_count="2"`) {
		t.Error("Expected tenant_count label with value 2")
	}
}

func TestAuthAttemptsWithTestRegistry(t *testing.T) {
	service := NewTestableService()
	ctx := context.Background()

	// Record successful and failed auth attempts
	service.RecordAuthAttempt(ctx, "user1", "success")
	service.RecordAuthAttempt(ctx, "user2", "failed")
	service.RecordAuthAttempt(ctx, "user1", "success") // Another success

	// Get metrics output
	output, err := service.GetMetricsOutput()
	if err != nil {
		t.Fatalf("Failed to get metrics output: %v", err)
	}

	// Verify auth metrics
	if !strings.Contains(output, "vm_proxy_auth_auth_attempts_total") {
		t.Error("Expected auth attempts counter metric")
	}

	if !strings.Contains(output, `status="success"`) {
		t.Error("Expected success status label")
	}

	if !strings.Contains(output, `status="failed"`) {
		t.Error("Expected failed status label")
	}
}

func TestQueryFilteringMetricsWithTestRegistry(t *testing.T) {
	service := NewTestableService()
	ctx := context.Background()

	// Record query filtering with different scenarios
	service.RecordQueryFilter(ctx, "user1", 2, true, 10*time.Millisecond)   // Filter applied
	service.RecordQueryFilter(ctx, "user2", 1, false, 5*time.Millisecond)   // No filter needed
	service.RecordQueryFilter(ctx, "user1", 3, true, 15*time.Millisecond)   // Filter applied

	// Get metrics output
	output, err := service.GetMetricsOutput()
	if err != nil {
		t.Fatalf("Failed to get metrics output: %v", err)
	}

	// Verify query filtering metrics
	if !strings.Contains(output, "vm_proxy_auth_query_filtering_total") {
		t.Error("Expected query filtering counter metric")
	}

	if !strings.Contains(output, "vm_proxy_auth_query_filtering_duration_seconds") {
		t.Error("Expected query filtering duration histogram")
	}

	if !strings.Contains(output, `filter_applied="true"`) {
		t.Error("Expected filter_applied=true label")
	}

	if !strings.Contains(output, `filter_applied="false"`) {
		t.Error("Expected filter_applied=false label")
	}
}

func TestTenantAccessMetricsWithTestRegistry(t *testing.T) {
	service := NewTestableService()
	ctx := context.Background()

	// Record tenant access checks
	service.RecordTenantAccess(ctx, "user1", "1000", true)   // Allowed
	service.RecordTenantAccess(ctx, "user1", "2000", false)  // Denied
	service.RecordTenantAccess(ctx, "user2", "1000", true)   // Allowed

	// Get metrics output
	output, err := service.GetMetricsOutput()
	if err != nil {
		t.Fatalf("Failed to get metrics output: %v", err)
	}

	// Verify tenant access metrics
	if !strings.Contains(output, "vm_proxy_auth_tenant_access_total") {
		t.Error("Expected tenant access counter metric")
	}

	if !strings.Contains(output, `allowed="true"`) {
		t.Error("Expected allowed=true label")
	}

	if !strings.Contains(output, `allowed="false"`) {
		t.Error("Expected allowed=false label")
	}

	if !strings.Contains(output, `tenant_id="1000"`) {
		t.Error("Expected tenant_id=1000 label")
	}
}

func TestMetricsEndpointFormat(t *testing.T) {
	service := NewTestableService()
	ctx := context.Background()

	// Record some sample data
	user := &domain.User{ID: "test-user"}
	service.RecordRequest(ctx, "GET", "/api/v1/query", "200", 100*time.Millisecond, user)

	// Get metrics output
	output, err := service.GetMetricsOutput()
	if err != nil {
		t.Fatalf("Failed to get metrics output: %v", err)
	}

	// Check basic Prometheus format
	if !strings.Contains(output, "# HELP") {
		t.Error("Expected Prometheus HELP comments")
	}

	if !strings.Contains(output, "# TYPE") {
		t.Error("Expected Prometheus TYPE comments")
	}

	// Verify it's valid Prometheus exposition format
	lines := strings.Split(output, "\n")
	hasMetricLine := false
	
	for _, line := range lines {
		if strings.HasPrefix(line, "vm_proxy_auth_") && strings.Contains(line, "{") {
			hasMetricLine = true
			break
		}
	}
	
	if !hasMetricLine {
		t.Error("Expected at least one metric line with labels")
	}
}

func TestConcurrentMetricsRecording(t *testing.T) {
	service := NewTestableService()
	ctx := context.Background()

	user := &domain.User{ID: "test-user"}

	// Record metrics concurrently to test thread safety
	done := make(chan bool, 10)
	
	for i := 0; i < 10; i++ {
		go func(id int) {
			service.RecordRequest(ctx, "GET", "/test", "200", time.Duration(id)*time.Millisecond, user)
			service.RecordAuthAttempt(ctx, "user", "success")
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Get metrics output
	output, err := service.GetMetricsOutput()
	if err != nil {
		t.Fatalf("Failed to get metrics output: %v", err)
	}

	// Should have recorded all metrics without race conditions
	if !strings.Contains(output, "vm_proxy_auth_http_requests_total") {
		t.Error("Expected HTTP request metrics after concurrent recording")
	}

	if !strings.Contains(output, "vm_proxy_auth_auth_attempts_total") {
		t.Error("Expected auth attempt metrics after concurrent recording")
	}
}