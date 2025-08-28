package proxy_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/proxy"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestNewService(t *testing.T) {
	logger := &testutils.MockLogger{}
	metrics := &MockMetricsService{}

	service := proxy.NewService(
		"http://upstream.example.com",
		30*time.Second,
		logger,
		metrics,
	)

	require.NotNil(t, service)
}

func TestService_Forward_GET_Request(t *testing.T) {
	// Create mock upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request was forwarded correctly
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/query", r.URL.Path)
		assert.Equal(t, "up{vm_account_id=\"1000\"}", r.URL.Query().Get("query"))
		assert.Equal(t, "test-value", r.Header.Get("X-Test-Header"))

		// Send response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer upstream.Close()

	logger := &testutils.MockLogger{}
	metrics := &MockMetricsService{}
	service := proxy.NewService(upstream.URL, 30*time.Second, logger, metrics)

	// Create test request
	req, err := http.NewRequest(http.MethodGet, "/api/v1/query?query=up", nil)
	require.NoError(t, err)
	req.Header.Set("X-Test-Header", "test-value")

	user := &domain.User{
		ID: "test-user",
		VMTenants: []domain.VMTenant{
			{AccountID: "1000", ProjectID: "test"},
		},
	}

	proxyReq := &domain.ProxyRequest{
		User:            user,
		OriginalRequest: req,
		FilteredQuery:   "up{vm_account_id=\"1000\"}",
		TargetTenant:    "1000:test",
	}

	// Forward request
	resp, err := service.Forward(context.Background(), proxyReq)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Headers.Get("Content-Type"))
	assert.Contains(t, string(resp.Body), "success")
}

func TestService_Forward_POST_Request(t *testing.T) {
	// Create mock upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and headers
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/write", r.URL.Path)
		assert.Equal(t, "1000:test", r.Header.Get("X-Prometheus-Tenant"))

		// Read and verify body
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Contains(t, string(body), "test_metric")

		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	logger := &testutils.MockLogger{}
	metrics := &MockMetricsService{}
	service := proxy.NewService(upstream.URL, 30*time.Second, logger, metrics)

	// Create test POST request with body
	body := strings.NewReader("test_metric{job=\"test\"} 1")
	req, err := http.NewRequest(http.MethodPost, "/api/v1/write", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-protobuf")

	user := &domain.User{
		ID: "test-user",
		VMTenants: []domain.VMTenant{
			{AccountID: "1000", ProjectID: "test"},
		},
	}

	proxyReq := &domain.ProxyRequest{
		User:            user,
		OriginalRequest: req,
		TargetTenant:    "1000:test",
	}

	// Forward request
	resp, err := service.Forward(context.Background(), proxyReq)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestService_Forward_QueryFiltering(t *testing.T) {
	tests := []struct {
		name          string
		originalQuery string
		filteredQuery string
		expectQuery   string
	}{
		{
			name:          "simple query filtering",
			originalQuery: "up",
			filteredQuery: "up{vm_account_id=\"1000\"}",
			expectQuery:   "up{vm_account_id=\"1000\"}",
		},
		{
			name:          "complex query filtering",
			originalQuery: "rate(http_requests_total[5m])",
			filteredQuery: "rate(http_requests_total{vm_account_id=\"1000\"}[5m])",
			expectQuery:   "rate(http_requests_total{vm_account_id=\"1000\"}[5m])",
		},
		{
			name:          "no filtering needed",
			originalQuery: "",
			filteredQuery: "",
			expectQuery:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock upstream that captures the query
			var capturedQuery string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedQuery = r.URL.Query().Get("query")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"status":"success"}`))
			}))
			defer upstream.Close()

			logger := &testutils.MockLogger{}
			metrics := &MockMetricsService{}
			service := proxy.NewService(upstream.URL, 30*time.Second, logger, metrics)

			// Create request URL with query parameter
			requestURL := "/api/v1/query"
			if tt.originalQuery != "" {
				requestURL += "?query=" + url.QueryEscape(tt.originalQuery)
			}

			req, err := http.NewRequest(http.MethodGet, requestURL, nil)
			require.NoError(t, err)

			user := &domain.User{ID: "test-user"}

			proxyReq := &domain.ProxyRequest{
				User:            user,
				OriginalRequest: req,
				FilteredQuery:   tt.filteredQuery,
			}

			// Forward request
			_, err = service.Forward(context.Background(), proxyReq)
			require.NoError(t, err)

			// Verify the query was filtered correctly
			assert.Equal(t, tt.expectQuery, capturedQuery)
		})
	}
}

func TestService_Forward_ErrorHandling(t *testing.T) {
	tests := []struct {
		name           string
		upstreamStatus int
		upstreamBody   string
		expectError    bool
	}{
		{
			name:           "upstream success",
			upstreamStatus: http.StatusOK,
			upstreamBody:   `{"status":"success"}`,
			expectError:    false,
		},
		{
			name:           "upstream bad request",
			upstreamStatus: http.StatusBadRequest,
			upstreamBody:   `{"status":"error","error":"bad query"}`,
			expectError:    false, // Not an error for proxy, just forwarded
		},
		{
			name:           "upstream server error",
			upstreamStatus: http.StatusInternalServerError,
			upstreamBody:   `{"status":"error","error":"internal error"}`,
			expectError:    false, // Not an error for proxy, just forwarded
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.upstreamStatus)
				w.Write([]byte(tt.upstreamBody))
			}))
			defer upstream.Close()

			logger := &testutils.MockLogger{}
			metrics := &MockMetricsService{}
			service := proxy.NewService(upstream.URL, 30*time.Second, logger, metrics)

			req, err := http.NewRequest(http.MethodGet, "/api/v1/query?query=up", nil)
			require.NoError(t, err)

			user := &domain.User{ID: "test-user"}

			proxyReq := &domain.ProxyRequest{
				User:            user,
				OriginalRequest: req,
				FilteredQuery:   "up",
			}

			resp, err := service.Forward(context.Background(), proxyReq)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, tt.upstreamStatus, resp.StatusCode)
				assert.Contains(t, string(resp.Body), tt.upstreamBody)
			}
		})
	}
}

func TestService_Forward_TenantHeaders(t *testing.T) {
	tests := []struct {
		name         string
		targetTenant string
		expectHeader string
	}{
		{
			name:         "account only tenant",
			targetTenant: "1000",
			expectHeader: "1000",
		},
		{
			name:         "account and project tenant",
			targetTenant: "1000:project",
			expectHeader: "1000:project",
		},
		{
			name:         "empty tenant",
			targetTenant: "",
			expectHeader: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedHeader string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedHeader = r.Header.Get("X-Prometheus-Tenant")
				w.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()

			logger := &testutils.MockLogger{}
			metrics := &MockMetricsService{}
			service := proxy.NewService(upstream.URL, 30*time.Second, logger, metrics)

			req, err := http.NewRequest(http.MethodPost, "/api/v1/write", strings.NewReader("test_metric 1"))
			require.NoError(t, err)

			user := &domain.User{ID: "test-user"}

			proxyReq := &domain.ProxyRequest{
				User:            user,
				OriginalRequest: req,
				TargetTenant:    tt.targetTenant,
			}

			_, err = service.Forward(context.Background(), proxyReq)
			require.NoError(t, err)

			assert.Equal(t, tt.expectHeader, capturedHeader)
		})
	}
}

func TestService_Forward_RequestTimeout(t *testing.T) {
	// Create slow upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond) // Longer than client timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	logger := &testutils.MockLogger{}
	metrics := &MockMetricsService{}
	// Short timeout for testing
	service := proxy.NewService(upstream.URL, 100*time.Millisecond, logger, metrics)

	req, err := http.NewRequest(http.MethodGet, "/api/v1/query?query=up", nil)
	require.NoError(t, err)

	user := &domain.User{ID: "test-user"}

	proxyReq := &domain.ProxyRequest{
		User:            user,
		OriginalRequest: req,
		FilteredQuery:   "up",
	}

	_, err = service.Forward(context.Background(), proxyReq)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

func TestService_Forward_FormDataHandling(t *testing.T) {
	// Mock upstream that captures form data
	var capturedQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		assert.NoError(t, err)
		capturedQuery = r.Form.Get("query")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	}))
	defer upstream.Close()

	logger := &testutils.MockLogger{}
	metrics := &MockMetricsService{}
	service := proxy.NewService(upstream.URL, 30*time.Second, logger, metrics)

	// Create POST request with form data
	formData := url.Values{}
	formData.Set("query", "up")
	formData.Set("step", "60s")

	req, err := http.NewRequest(http.MethodPost, "/api/v1/query_range", strings.NewReader(formData.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	user := &domain.User{ID: "test-user"}

	proxyReq := &domain.ProxyRequest{
		User:            user,
		OriginalRequest: req,
		FilteredQuery:   "up{vm_account_id=\"1000\"}",
	}

	_, err = service.Forward(context.Background(), proxyReq)
	require.NoError(t, err)

	// Verify form data query was replaced with filtered query
	assert.Equal(t, "up{vm_account_id=\"1000\"}", capturedQuery)
}

func TestService_Forward_HeaderPropagation(t *testing.T) {
	// Headers that should be forwarded
	expectedHeaders := map[string]string{
		"User-Agent":    "test-client/1.0",
		"Accept":        "application/json",
		"Authorization": "Bearer token", // Should be forwarded
		"X-Custom":      "custom-value",
	}

	// Headers that should be added/modified by proxy
	expectedProxyHeaders := map[string]string{
		"X-Forwarded-For":     "", // Will be set by proxy
		"X-Prometheus-Tenant": "1000:test",
	}

	var capturedHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	logger := &testutils.MockLogger{}
	metrics := &MockMetricsService{}
	service := proxy.NewService(upstream.URL, 30*time.Second, logger, metrics)

	req, err := http.NewRequest(http.MethodGet, "/api/v1/query?query=up", nil)
	require.NoError(t, err)

	// Set test headers
	for k, v := range expectedHeaders {
		req.Header.Set(k, v)
	}

	user := &domain.User{ID: "test-user"}

	proxyReq := &domain.ProxyRequest{
		User:            user,
		OriginalRequest: req,
		FilteredQuery:   "up",
		TargetTenant:    "1000:test",
	}

	_, err = service.Forward(context.Background(), proxyReq)
	require.NoError(t, err)

	// Verify original headers are preserved
	for k, v := range expectedHeaders {
		assert.Equal(t, v, capturedHeaders.Get(k), "Header %s not forwarded correctly", k)
	}

	// Verify proxy-specific headers
	assert.Equal(t, expectedProxyHeaders["X-Prometheus-Tenant"], capturedHeaders.Get("X-Prometheus-Tenant"))
}

func TestService_Forward_NetworkError(t *testing.T) {
	logger := &testutils.MockLogger{}
	metrics := &MockMetricsService{}
	// Invalid upstream URL to trigger network error
	service := proxy.NewService("http://invalid-host-that-does-not-exist.local:9999", 1*time.Second, logger, metrics)

	req, err := http.NewRequest(http.MethodGet, "/api/v1/query?query=up", nil)
	require.NoError(t, err)

	user := &domain.User{ID: "test-user"}

	proxyReq := &domain.ProxyRequest{
		User:            user,
		OriginalRequest: req,
		FilteredQuery:   "up",
	}

	_, err = service.Forward(context.Background(), proxyReq)
	require.Error(t, err)
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

// Backend-specific metrics
func (m *MockMetricsService) RecordUpstreamBackend(context.Context, string, string, string, string, time.Duration, []string) {
}
func (m *MockMetricsService) RecordHealthCheck(context.Context, string, bool, time.Duration) {}
func (m *MockMetricsService) RecordBackendStateChange(context.Context, string, domain.BackendState, domain.BackendState) {
}
func (m *MockMetricsService) RecordCircuitBreakerStateChange(context.Context, string, domain.CircuitBreakerState) {
}
func (m *MockMetricsService) RecordQueueOperation(context.Context, string, time.Duration, int) {}
func (m *MockMetricsService) RecordLoadBalancerSelection(context.Context, domain.LoadBalancingStrategy, string, time.Duration) {
}

func (m *MockMetricsService) Handler() http.Handler { return nil }
