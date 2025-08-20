package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/finlego/prometheus-oauth-gateway/internal/middleware"
	"github.com/finlego/prometheus-oauth-gateway/internal/proxy"
	"github.com/finlego/prometheus-oauth-gateway/pkg/tenant"
)

// Mock proxy that implements proxy.Proxy interface behavior
type mockProxy struct {
	responseStatus int
	responseBody   string
	shouldError    bool
}

func (m *mockProxy) ForwardRequest(ctx context.Context, req *proxy.ProxyRequest) (*http.Response, error) {
	if m.shouldError {
		return nil, &url.Error{Op: "Get", URL: "http://test", Err: context.DeadlineExceeded}
	}

	// Create a mock response
	resp := &http.Response{
		StatusCode: m.responseStatus,
		Header:     make(http.Header),
		Body:       &mockReadCloser{strings.NewReader(m.responseBody)},
	}

	resp.Header.Set("Content-Type", "application/json")
	return resp, nil
}

type mockReadCloser struct {
	*strings.Reader
}

func (m *mockReadCloser) Close() error {
	return nil
}

func TestHandlers_HealthCheck(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	handlers := NewHandlers(nil, nil, logger)

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	handlers.HealthCheck(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Errorf("Failed to decode response: %v", err)
	}

	if response["status"] != "healthy" {
		t.Errorf("Expected status healthy, got %v", response["status"])
	}

	if response["version"] != "1.0.0" {
		t.Errorf("Expected version 1.0.0, got %v", response["version"])
	}
}

func TestHandlers_ReadinessCheck(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	handlers := NewHandlers(nil, nil, logger)

	req := httptest.NewRequest("GET", "/readiness", nil)
	rr := httptest.NewRecorder()

	handlers.ReadinessCheck(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Errorf("Failed to decode response: %v", err)
	}

	if response["status"] != "ready" {
		t.Errorf("Expected status ready, got %v", response["status"])
	}

	checks, ok := response["checks"].(map[string]interface{})
	if !ok {
		t.Error("Expected checks to be a map")
	}

	if checks["upstream"] != "ok" {
		t.Errorf("Expected upstream check ok, got %v", checks["upstream"])
	}
}

func TestHandlers_PrometheusProxy(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Create tenant mapper
	tenantMappings := []tenant.Mapping{
		{
			Groups:   []string{"admin"},
			Tenants:  []string{"*"},
			ReadOnly: false,
		},
		{
			Groups:   []string{"team-alpha"},
			Tenants:  []string{"alpha"},
			ReadOnly: false,
		},
	}
	tenantMapper := tenant.NewMapper(tenantMappings)

	tests := []struct {
		name           string
		method         string
		path           string
		query          string
		userContext    *middleware.UserContext
		mockProxy      *mockProxy
		wantStatus     int
		wantBodyStr    string
	}{
		{
			name:   "successful query request",
			method: "GET",
			path:   "/api/v1/query",
			query:  "up",
			userContext: &middleware.UserContext{
				UserID:         "testuser",
				Groups:         []string{"team-alpha"},
				AllowedTenants: []string{"alpha"},
				ReadOnly:       false,
			},
			mockProxy: &mockProxy{
				responseStatus: http.StatusOK,
				responseBody:   `{"status":"success","data":{"resultType":"vector","result":[]}}`,
				shouldError:    false,
			},
			wantStatus:  http.StatusOK,
			wantBodyStr: `{"status":"success","data":{"resultType":"vector","result":[]}}`,
		},
		{
			name:   "admin user with wildcard access",
			method: "GET",
			path:   "/api/v1/query",
			query:  "up",
			userContext: &middleware.UserContext{
				UserID:         "admin",
				Groups:         []string{"admin"},
				AllowedTenants: []string{"*"},
				ReadOnly:       false,
			},
			mockProxy: &mockProxy{
				responseStatus: http.StatusOK,
				responseBody:   `{"status":"success"}`,
				shouldError:    false,
			},
			wantStatus:  http.StatusOK,
			wantBodyStr: `{"status":"success"}`,
		},
		{
			name:   "non-query endpoint",
			method: "GET",
			path:   "/api/v1/label/__name__/values",
			userContext: &middleware.UserContext{
				UserID:         "testuser",
				Groups:         []string{"team-alpha"},
				AllowedTenants: []string{"alpha"},
				ReadOnly:       false,
			},
			mockProxy: &mockProxy{
				responseStatus: http.StatusOK,
				responseBody:   `["metric1","metric2"]`,
				shouldError:    false,
			},
			wantStatus:  http.StatusOK,
			wantBodyStr: `["metric1","metric2"]`,
		},
		{
			name:   "POST request with form data",
			method: "POST",
			path:   "/api/v1/query",
			userContext: &middleware.UserContext{
				UserID:         "testuser",
				Groups:         []string{"team-alpha"},
				AllowedTenants: []string{"alpha"},
				ReadOnly:       false,
			},
			mockProxy: &mockProxy{
				responseStatus: http.StatusOK,
				responseBody:   `{"status":"success"}`,
				shouldError:    false,
			},
			wantStatus:  http.StatusOK,
			wantBodyStr: `{"status":"success"}`,
		},
		{
			name:   "missing user context",
			method: "GET",
			path:   "/api/v1/query",
			query:  "up",
			userContext: nil,
			mockProxy: &mockProxy{
				responseStatus: http.StatusOK,
				responseBody:   "",
				shouldError:    false,
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:   "upstream error",
			method: "GET",
			path:   "/api/v1/query",
			query:  "up",
			userContext: &middleware.UserContext{
				UserID:         "testuser",
				Groups:         []string{"team-alpha"},
				AllowedTenants: []string{"alpha"},
				ReadOnly:       false,
			},
			mockProxy: &mockProxy{
				shouldError: true,
			},
			wantStatus: http.StatusBadGateway,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers := NewHandlers(tt.mockProxy, tenantMapper, logger)

			var req *http.Request
			if tt.method == "POST" {
				form := url.Values{}
				form.Set("query", "rate(http_requests_total[5m])")
				req = httptest.NewRequest(tt.method, tt.path, strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			} else {
				url := tt.path
				if tt.query != "" {
					url += "?query=" + tt.query
				}
				req = httptest.NewRequest(tt.method, url, nil)
			}

			// Set user context if provided
			if tt.userContext != nil {
				ctx := context.WithValue(req.Context(), middleware.UserContextKey, tt.userContext)
				req = req.WithContext(ctx)
			}

			rr := httptest.NewRecorder()
			handlers.PrometheusProxy(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, rr.Code)
			}

			if tt.wantBodyStr != "" {
				if strings.TrimSpace(rr.Body.String()) != tt.wantBodyStr {
					t.Errorf("Expected body %s, got %s", tt.wantBodyStr, strings.TrimSpace(rr.Body.String()))
				}
			}
		})
	}
}

func TestHandlers_ExtractQuery(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	handlers := NewHandlers(nil, nil, logger)

	tests := []struct {
		name   string
		req    *http.Request
		want   string
	}{
		{
			name: "GET request with query parameter",
			req:  httptest.NewRequest("GET", "/api/v1/query?query=up", nil),
			want: "up",
		},
		{
			name: "GET request without query parameter",
			req:  httptest.NewRequest("GET", "/api/v1/query", nil),
			want: "",
		},
		{
			name: "POST request with form data",
			req: func() *http.Request {
				form := url.Values{}
				form.Set("query", "rate(http_requests_total[5m])")
				req := httptest.NewRequest("POST", "/api/v1/query", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				return req
			}(),
			want: "rate(http_requests_total[5m])",
		},
		{
			name: "POST request without form data",
			req: func() *http.Request {
				req := httptest.NewRequest("POST", "/api/v1/query", strings.NewReader("{}"))
				req.Header.Set("Content-Type", "application/json")
				return req
			}(),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := handlers.extractQuery(tt.req); got != tt.want {
				t.Errorf("extractQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandlers_IsQueryEndpoint(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	handlers := NewHandlers(nil, nil, logger)

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "query endpoint",
			path: "/api/v1/query",
			want: true,
		},
		{
			name: "query range endpoint",
			path: "/api/v1/query_range",
			want: true,
		},
		{
			name: "query exemplars endpoint",
			path: "/api/v1/query_exemplars",
			want: true,
		},
		{
			name: "query endpoint with parameters",
			path: "/api/v1/query?query=up",
			want: true,
		},
		{
			name: "labels endpoint",
			path: "/api/v1/labels",
			want: false,
		},
		{
			name: "series endpoint",
			path: "/api/v1/series",
			want: false,
		},
		{
			name: "metrics endpoint",
			path: "/metrics",
			want: false,
		},
		{
			name: "health endpoint",
			path: "/health",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := handlers.isQueryEndpoint(tt.path); got != tt.want {
				t.Errorf("isQueryEndpoint() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandlers_WriteErrorResponse(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	handlers := NewHandlers(nil, nil, logger)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handlers.writeErrorResponse(rr, http.StatusNotFound, "Resource not found")

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Errorf("Failed to decode response: %v", err)
	}

	if response["status"] != "error" {
		t.Errorf("Expected status error, got %v", response["status"])
	}

	if response["error"] != "Resource not found" {
		t.Errorf("Expected error message 'Resource not found', got %v", response["error"])
	}

	if response["code"] != float64(404) {
		t.Errorf("Expected code 404, got %v", response["code"])
	}
}