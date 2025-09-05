package metrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/metrics"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestMetricsService_Creation(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	service := metrics.NewService(logger)

	if service == nil {
		t.Fatal("Expected service to be created, got nil")
	}
}

func TestMetricsService_Handler(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	service := metrics.NewService(logger)

	handler := service.Handler()
	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}

	// Test that handler responds to requests
	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", recorder.Code)
	}

	// Should have some metrics content
	if len(recorder.Body.String()) == 0 {
		t.Error("Expected metrics output, got empty response")
	}
}

func TestMetricsService_RecordRequest(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	service := metrics.NewService(logger)

	ctx := context.Background()
	user := &domain.User{ID: "test-user"}
	duration := 100 * time.Millisecond

	// This should not panic
	service.RecordRequest(ctx, "GET", "/api/v1/query", "200", duration, user)
	service.RecordUpstream(ctx, "GET", "/api/v1/query", "200", duration, []string{"tenant1"})
	service.RecordAuthAttempt(ctx, user.ID, "success")
	service.RecordQueryFilter(ctx, user.ID, 1, true, duration)
	service.RecordTenantAccess(ctx, user.ID, "tenant1", true)
}

func TestMetricsService_MultipleCalls(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	service := metrics.NewService(logger)

	ctx := context.Background()
	user := &domain.User{ID: "test-user"}
	duration := 50 * time.Millisecond

	// Record multiple metrics calls
	for range 5 {
		service.RecordRequest(ctx, "POST", "/api/v1/query", "200", duration, user)
		service.RecordUpstream(ctx, "POST", "/api/v1/query", "200", duration, []string{"tenant1"})
		service.RecordAuthAttempt(ctx, user.ID, "success")
	}

	// Should be able to get metrics without error
	handler := service.Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", recorder.Code)
	}
}
