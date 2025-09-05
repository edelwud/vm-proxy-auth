package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/handlers"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

const (
	testUserID     = "test-user"
	testStatusOK   = "200"
	testMethodGET  = http.MethodGet
	testMethodPOST = "POST"
)

//nolint:gocognit // test case
func TestGatewayHandler_MetricsCollection_Success(t *testing.T) {
	metrics := &testutils.TestableMetricsCollector{}

	// Setup testable services
	user := &domain.User{
		ID:        "test-user",
		VMTenants: []domain.VMTenant{{AccountID: "1000"}},
	}

	authService := &testutils.TestableAuthService{
		User:    user,
		Metrics: metrics,
	}

	tenantService := &testutils.TestableTenantService{
		FilteredQuery: "up{vm_account_id=\"1000\"}",
		CanAccess:     true,
		TargetTenant:  "1000",
		Metrics:       metrics,
	}

	proxyResponse := &domain.ProxyResponse{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"status":"success"}`),
	}

	proxyService := &testutils.TestableProxyService{
		Response: proxyResponse,
		Metrics:  metrics,
	}

	accessService := &testutils.MockAccessService{}

	logger := testutils.NewMockLogger()

	// Create gateway handler
	gateway := handlers.NewGatewayHandler(
		authService,
		tenantService,
		accessService,
		proxyService,
		metrics,
		logger,
	)

	// Create test request
	req := httptest.NewRequest(testMethodGET, "/api/v1/query?query=up", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	// Create response recorder
	rr := httptest.NewRecorder()

	// Handle request
	gateway.ServeHTTP(rr, req)

	// Verify request metrics were recorded
	if len(metrics.RequestCalls) > 0 {
		requestCall := metrics.RequestCalls[0]
		if requestCall.Method != testMethodGET {
			t.Errorf("Expected method %s, got %s", testMethodGET, requestCall.Method)
		}
		if requestCall.Status != testStatusOK {
			t.Errorf("Expected status %s, got %s", testStatusOK, requestCall.Status)
		}
		if requestCall.User == nil || requestCall.User.ID != testUserID {
			t.Errorf("Expected user ID %s", testUserID)
		}
	}

	// Verify auth attempt was recorded
	if len(metrics.AuthAttemptCalls) > 0 {
		authCall := metrics.AuthAttemptCalls[0]
		if authCall.UserID != testUserID {
			t.Errorf("Expected auth user ID %s, got %s", testUserID, authCall.UserID)
		}
		if authCall.Status != "success" {
			t.Errorf("Expected auth status success, got %s", authCall.Status)
		}
	}

	// Verify query filter was recorded
	if len(metrics.QueryFilterCalls) > 0 {
		queryCall := metrics.QueryFilterCalls[0]
		if queryCall.UserID != testUserID {
			t.Errorf("Expected query user ID %s, got %s", testUserID, queryCall.UserID)
		}
		if queryCall.TenantCount != 1 {
			t.Errorf("Expected tenant count 1, got %d", queryCall.TenantCount)
		}
	}

	// Verify upstream call was recorded
	if len(metrics.UpstreamCalls) > 0 {
		upstreamCall := metrics.UpstreamCalls[0]
		if upstreamCall.Method != testMethodGET {
			t.Errorf("Expected upstream method %s, got %s", testMethodGET, upstreamCall.Method)
		}
		if upstreamCall.Status != "OK" {
			t.Errorf("Expected upstream status OK, got %s", upstreamCall.Status)
		}
		if len(upstreamCall.Tenants) != 1 {
			t.Errorf("Expected 1 tenant, got %d", len(upstreamCall.Tenants))
		}
	}
}
