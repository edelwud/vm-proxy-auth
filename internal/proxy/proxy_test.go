package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestNew(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Suppress logs during testing

	tests := []struct {
		name      string
		config    *Config
		wantErr   bool
		wantURL   string
	}{
		{
			name: "valid config",
			config: &Config{
				UpstreamURL:  "http://localhost:8428",
				Timeout:      30 * time.Second,
				MaxRetries:   3,
				RetryDelay:   time.Second,
				TenantHeader: "X-Prometheus-Tenant",
			},
			wantErr: false,
			wantURL: "http://localhost:8428",
		},
		{
			name: "invalid upstream URL",
			config: &Config{
				UpstreamURL: "://invalid-url",
			},
			wantErr: true,
		},
		{
			name: "empty upstream URL",
			config: &Config{
				UpstreamURL: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy, err := New(tt.config, logger)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && proxy != nil {
				if proxy.upstreamURL.String() != tt.wantURL {
					t.Errorf("Expected upstream URL %s, got %s", tt.wantURL, proxy.upstreamURL.String())
				}
			}
		})
	}
}

func TestProxy_ForwardRequest(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Create mock upstream server
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check headers
		if tenant := r.Header.Get("X-Prometheus-Tenant"); tenant != "" {
			w.Header().Set("X-Received-Tenant", tenant)
		}

		if userID := r.Header.Get("X-Forwarded-User"); userID != "" {
			w.Header().Set("X-Received-User", userID)
		}

		// Check query parameters
		if query := r.URL.Query().Get("query"); query != "" {
			w.Header().Set("X-Received-Query", query)
		}

		// Return success response
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))
	defer upstreamServer.Close()

	config := &Config{
		UpstreamURL:  upstreamServer.URL,
		Timeout:      30 * time.Second,
		MaxRetries:   3,
		RetryDelay:   100 * time.Millisecond,
		TenantHeader: "X-Prometheus-Tenant",
	}

	proxy, err := New(config, logger)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		proxyReq     *ProxyRequest
		wantStatus   int
		wantTenant   string
		wantUser     string
		wantQuery    string
	}{
		{
			name: "simple GET request",
			proxyReq: &ProxyRequest{
				OriginalRequest: func() *http.Request {
					req := httptest.NewRequest("GET", "/api/v1/query?query=up", nil)
					return req
				}(),
				AllowedTenants: []string{"alpha", "beta"},
				UserID:         "testuser",
				ReadOnly:       false,
			},
			wantStatus: http.StatusOK,
			wantTenant: "alpha,beta",
			wantUser:   "testuser",
			wantQuery:  "up",
		},
		{
			name: "request with modified query",
			proxyReq: &ProxyRequest{
				OriginalRequest: func() *http.Request {
					req := httptest.NewRequest("GET", "/api/v1/query", nil)
					return req
				}(),
				ModifiedQuery:  "up{tenant_id=\"alpha\"}",
				AllowedTenants: []string{"alpha"},
				UserID:         "testuser",
				ReadOnly:       false,
			},
			wantStatus: http.StatusOK,
			wantTenant: "alpha",
			wantUser:   "testuser",
			wantQuery:  "up{tenant_id=\"alpha\"}",
		},
		{
			name: "admin user with wildcard access",
			proxyReq: &ProxyRequest{
				OriginalRequest: func() *http.Request {
					req := httptest.NewRequest("GET", "/api/v1/query?query=up", nil)
					return req
				}(),
				AllowedTenants: []string{"*"},
				UserID:         "admin",
				ReadOnly:       false,
			},
			wantStatus: http.StatusOK,
			wantTenant: "", // No tenant header for wildcard access
			wantUser:   "admin",
			wantQuery:  "up",
		},
		{
			name: "POST request with form data",
			proxyReq: &ProxyRequest{
				OriginalRequest: func() *http.Request {
					form := url.Values{}
					form.Set("query", "rate(http_requests_total[5m])")
					req := httptest.NewRequest("POST", "/api/v1/query", strings.NewReader(form.Encode()))
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					return req
				}(),
				AllowedTenants: []string{"beta"},
				UserID:         "postuser",
				ReadOnly:       true,
			},
			wantStatus: http.StatusOK,
			wantTenant: "beta",
			wantUser:   "postuser",
			wantQuery:  "rate(http_requests_total[5m])",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			resp, err := proxy.ForwardRequest(ctx, tt.proxyReq)
			if err != nil {
				t.Errorf("ForwardRequest() error = %v", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}

			if tt.wantTenant != "" {
				if got := resp.Header.Get("X-Received-Tenant"); got != tt.wantTenant {
					t.Errorf("Expected tenant header %s, got %s", tt.wantTenant, got)
				}
			}

			if tt.wantUser != "" {
				if got := resp.Header.Get("X-Received-User"); got != tt.wantUser {
					t.Errorf("Expected user header %s, got %s", tt.wantUser, got)
				}
			}

			if tt.wantQuery != "" {
				if got := resp.Header.Get("X-Received-Query"); got != tt.wantQuery {
					t.Errorf("Expected query %s, got %s", tt.wantQuery, got)
				}
			}
		})
	}
}

func TestProxy_ForwardRequest_Retry(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	attempts := 0
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			// Fail first 2 attempts
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Succeed on 3rd attempt
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))
	defer upstreamServer.Close()

	config := &Config{
		UpstreamURL:  upstreamServer.URL,
		Timeout:      30 * time.Second,
		MaxRetries:   3,
		RetryDelay:   10 * time.Millisecond,
		TenantHeader: "X-Prometheus-Tenant",
	}

	proxy, err := New(config, logger)
	if err != nil {
		t.Fatal(err)
	}

	proxyReq := &ProxyRequest{
		OriginalRequest: httptest.NewRequest("GET", "/test", nil),
		UserID:          "testuser",
	}

	ctx := context.Background()
	resp, err := proxy.ForwardRequest(ctx, proxyReq)
	if err != nil {
		t.Errorf("ForwardRequest() should succeed after retries, got error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestProxy_ForwardRequest_MaxRetriesExceeded(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Server that always fails
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "always fails", http.StatusInternalServerError)
	}))
	defer upstreamServer.Close()

	config := &Config{
		UpstreamURL:  upstreamServer.URL,
		Timeout:      30 * time.Second,
		MaxRetries:   2,
		RetryDelay:   1 * time.Millisecond,
		TenantHeader: "X-Prometheus-Tenant",
	}

	proxy, err := New(config, logger)
	if err != nil {
		t.Fatal(err)
	}

	proxyReq := &ProxyRequest{
		OriginalRequest: httptest.NewRequest("GET", "/test", nil),
		UserID:          "testuser",
	}

	ctx := context.Background()
	_, err = proxy.ForwardRequest(ctx, proxyReq)
	if err == nil {
		t.Error("ForwardRequest() should fail when max retries exceeded")
	}
}

func TestCopyHeaders(t *testing.T) {
	src := http.Header{}
	src.Set("Authorization", "Bearer token")
	src.Set("Content-Type", "application/json")
	src.Set("Connection", "keep-alive") // Should be skipped
	src.Set("Custom-Header", "value")

	dst := http.Header{}
	copyHeaders(src, dst)

	tests := []struct {
		header      string
		shouldExist bool
		expected    string
	}{
		{"Authorization", true, "Bearer token"},
		{"Content-Type", true, "application/json"},
		{"Connection", false, ""},
		{"Custom-Header", true, "value"},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			value := dst.Get(tt.header)
			if tt.shouldExist {
				if value != tt.expected {
					t.Errorf("Expected header %s = %s, got %s", tt.header, tt.expected, value)
				}
			} else {
				if value != "" {
					t.Errorf("Header %s should be skipped, but found %s", tt.header, value)
				}
			}
		})
	}
}

func TestShouldCopyHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   bool
	}{
		{"normal header", "Authorization", true},
		{"content type", "Content-Type", true},
		{"custom header", "X-Custom-Header", true},
		{"connection - should skip", "Connection", false},
		{"proxy connection - should skip", "Proxy-Connection", false},
		{"transfer encoding - should skip", "Transfer-Encoding", false},
		{"upgrade - should skip", "Upgrade", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldCopyHeader(tt.header); got != tt.want {
				t.Errorf("shouldCopyHeader() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModifyFormQuery(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		newQuery string
		want     string
	}{
		{
			name:     "simple form with query",
			body:     "query=up&other=value",
			newQuery: "up{tenant_id=\"alpha\"}",
			want:     "other=value&query=up%7Btenant_id%3D%22alpha%22%7D",
		},
		{
			name:     "form without query",
			body:     "other=value",
			newQuery: "new_metric",
			want:     "other=value&query=new_metric",
		},
		{
			name:     "invalid form data",
			body:     "invalid%form%data",
			newQuery: "new_query",
			want:     "invalid%form%data", // Should return original body
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := modifyFormQuery(tt.body, tt.newQuery)
			if got != tt.want {
				t.Errorf("modifyFormQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContains(t *testing.T) {
	slice := []string{"alpha", "beta", "gamma"}

	tests := []struct {
		name string
		item string
		want bool
	}{
		{"found item", "beta", true},
		{"not found item", "delta", false},
		{"first item", "alpha", true},
		{"last item", "gamma", true},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contains(slice, tt.item); got != tt.want {
				t.Errorf("contains() = %v, want %v", got, tt.want)
			}
		})
	}
}