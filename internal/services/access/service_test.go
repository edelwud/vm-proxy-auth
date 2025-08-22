package access_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/access"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestNewService(t *testing.T) {
	logger := &testutils.MockLogger{}
	service := access.NewService(logger)
	require.NotNil(t, service)
}

func TestService_CanAccess_ReadOperations(t *testing.T) {
	tests := []struct {
		name          string
		user          *domain.User
		path          string
		method        string
		expectAllowed bool
		expectError   bool
	}{
		{
			name: "admin user - query endpoint",
			user: &domain.User{
				ID:       "admin@example.com",
				Groups:   []string{"admin"},
				ReadOnly: false,
			},
			path:          "/api/v1/query",
			method:        "GET",
			expectAllowed: true,
		},
		{
			name: "read-only user - query endpoint",
			user: &domain.User{
				ID:       "reader@example.com",
				Groups:   []string{"readers"},
				ReadOnly: true,
			},
			path:          "/api/v1/query",
			method:        "GET",
			expectAllowed: true,
		},
		{
			name: "regular user - query endpoint",
			user: &domain.User{
				ID:       "user@example.com",
				Groups:   []string{"users"},
				ReadOnly: false,
			},
			path:          "/api/v1/query",
			method:        "GET",
			expectAllowed: true,
		},
		{
			name: "admin user - query_range endpoint",
			user: &domain.User{
				ID:       "admin@example.com",
				Groups:   []string{"admin"},
				ReadOnly: false,
			},
			path:          "/api/v1/query_range",
			method:        "GET",
			expectAllowed: true,
		},
		{
			name: "user - series endpoint",
			user: &domain.User{
				ID:       "user@example.com",
				Groups:   []string{"users"},
				ReadOnly: false,
			},
			path:          "/api/v1/series",
			method:        "GET",
			expectAllowed: true,
		},
		{
			name: "user - labels endpoint",
			user: &domain.User{
				ID:       "user@example.com",
				Groups:   []string{"users"},
				ReadOnly: false,
			},
			path:          "/api/v1/labels",
			method:        "GET",
			expectAllowed: true,
		},
		{
			name: "user - label values endpoint",
			user: &domain.User{
				ID:       "user@example.com",
				Groups:   []string{"users"},
				ReadOnly: false,
			},
			path:          "/api/v1/label/job/values",
			method:        "GET",
			expectAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &testutils.MockLogger{}
			service := access.NewService(logger)

			err := service.CanAccess(context.Background(), tt.user, tt.path, tt.method)

			switch {
			case tt.expectError:
				require.Error(t, err)
			case tt.expectAllowed:
				require.NoError(t, err)
			default:
				require.Error(t, err)
				// Should be a forbidden error
				var appErr *domain.AppError
				require.ErrorAs(t, err, &appErr)
				assert.Equal(t, domain.ErrCodeForbidden, appErr.Code)
			}
		})
	}
}

func TestService_CanAccess_WriteOperations(t *testing.T) {
	tests := []struct {
		name          string
		user          *domain.User
		path          string
		method        string
		expectAllowed bool
		expectError   bool
	}{
		{
			name: "admin user - write endpoint",
			user: &domain.User{
				ID:       "admin@example.com",
				Groups:   []string{"admin"},
				ReadOnly: false,
			},
			path:          "/api/v1/write",
			method:        "POST",
			expectAllowed: true,
		},
		{
			name: "regular user - write endpoint",
			user: &domain.User{
				ID:       "user@example.com",
				Groups:   []string{"users"},
				ReadOnly: false,
			},
			path:          "/api/v1/write",
			method:        "POST",
			expectAllowed: true,
		},
		{
			name: "read-only user - write endpoint",
			user: &domain.User{
				ID:       "reader@example.com",
				Groups:   []string{"readers"},
				ReadOnly: true,
			},
			path:          "/api/v1/write",
			method:        "POST",
			expectAllowed: false,
		},
		{
			name: "read-only user - import endpoint",
			user: &domain.User{
				ID:       "reader@example.com",
				Groups:   []string{"readers"},
				ReadOnly: true,
			},
			path:          "/api/v1/import",
			method:        "POST",
			expectAllowed: false,
		},
		{
			name: "admin user - import endpoint",
			user: &domain.User{
				ID:       "admin@example.com",
				Groups:   []string{"admin"},
				ReadOnly: false,
			},
			path:          "/api/v1/import",
			method:        "POST",
			expectAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &testutils.MockLogger{}
			service := access.NewService(logger)

			err := service.CanAccess(context.Background(), tt.user, tt.path, tt.method)

			switch {
			case tt.expectError:
				require.Error(t, err)
			case tt.expectAllowed:
				require.NoError(t, err)
			default:
				require.Error(t, err)
				// Should be a read-only error
				require.ErrorIs(t, err, domain.ErrReadOnlyAccess)
			}
		})
	}
}

func TestService_CanAccess_RestrictedPaths(t *testing.T) {
	tests := []struct {
		name          string
		user          *domain.User
		path          string
		method        string
		expectAllowed bool
	}{
		{
			name: "admin user - admin endpoint",
			user: &domain.User{
				ID:     "admin@example.com",
				Groups: []string{"admin"},
			},
			path:          "/api/v1/admin/config",
			method:        "GET",
			expectAllowed: true,
		},
		{
			name: "regular user - admin endpoint",
			user: &domain.User{
				ID:     "user@example.com",
				Groups: []string{"users"},
			},
			path:          "/api/v1/admin/config",
			method:        "GET",
			expectAllowed: false,
		},
		{
			name: "admin user - debug endpoint",
			user: &domain.User{
				ID:     "admin@example.com",
				Groups: []string{"admin"},
			},
			path:          "/debug/pprof/heap",
			method:        "GET",
			expectAllowed: true,
		},
		{
			name: "regular user - debug endpoint",
			user: &domain.User{
				ID:     "user@example.com",
				Groups: []string{"users"},
			},
			path:          "/debug/pprof/heap",
			method:        "GET",
			expectAllowed: false,
		},
		{
			name: "admin user - metrics endpoint",
			user: &domain.User{
				ID:     "admin@example.com",
				Groups: []string{"admin"},
			},
			path:          "/metrics",
			method:        "GET",
			expectAllowed: true,
		},
		{
			name: "regular user - metrics endpoint",
			user: &domain.User{
				ID:     "user@example.com",
				Groups: []string{"users"},
			},
			path:          "/metrics",
			method:        "GET",
			expectAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &testutils.MockLogger{}
			service := access.NewService(logger)

			err := service.CanAccess(context.Background(), tt.user, tt.path, tt.method)

			if tt.expectAllowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				// Should be an admin required error for restricted paths
				require.ErrorIs(t, err, domain.ErrAdminRequired)
			}
		})
	}
}

func TestService_CanAccess_HealthEndpoints(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{
			name: "health endpoint",
			path: "/health",
		},
		{
			name: "readiness endpoint",
			path: "/ready",
		},
		{
			name: "root health check",
			path: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &testutils.MockLogger{}
			service := access.NewService(logger)

			// Even users without groups should access health endpoints
			user := &domain.User{
				ID:     "user@example.com",
				Groups: []string{}, // No groups
			}

			err := service.CanAccess(context.Background(), user, tt.path, "GET")
			assert.NoError(t, err)
		})
	}
}

func TestService_CanAccess_HTTPMethods(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		method      string
		expectWrite bool
	}{
		{
			name:        "GET is read operation",
			path:        "/api/v1/query",
			method:      "GET",
			expectWrite: false,
		},
		{
			name:        "HEAD is read operation",
			path:        "/api/v1/query",
			method:      "HEAD",
			expectWrite: false,
		},
		{
			name:        "OPTIONS is read operation",
			path:        "/api/v1/query",
			method:      "OPTIONS",
			expectWrite: false,
		},
		{
			name:        "POST is write operation",
			path:        "/api/v1/write",
			method:      "POST",
			expectWrite: true,
		},
		{
			name:        "PUT is write operation",
			path:        "/api/v1/import",
			method:      "PUT",
			expectWrite: true,
		},
		{
			name:        "PATCH is write operation",
			path:        "/api/v1/config",
			method:      "PATCH",
			expectWrite: true,
		},
		{
			name:        "DELETE is write operation",
			path:        "/api/v1/series",
			method:      "DELETE",
			expectWrite: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &testutils.MockLogger{}
			service := access.NewService(logger)

			// Test with read-only user to verify write operation detection
			readOnlyUser := &domain.User{
				ID:       "reader@example.com",
				Groups:   []string{"readers"},
				ReadOnly: true,
			}

			err := service.CanAccess(context.Background(), readOnlyUser, tt.path, tt.method)

			if tt.expectWrite {
				// Write operations should be denied for read-only users
				require.Error(t, err)
				require.ErrorIs(t, err, domain.ErrReadOnlyAccess)
			} else {
				// Read operations should be allowed for read-only users
				require.NoError(t, err)
			}
		})
	}
}

func TestService_CanAccess_NilUser(t *testing.T) {
	logger := &testutils.MockLogger{}
	service := access.NewService(logger)

	err := service.CanAccess(context.Background(), nil, "/api/v1/query", "GET")
	require.Error(t, err)

	var appErr *domain.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, domain.ErrCodeUnauthorized, appErr.Code)
}

func TestService_CanAccess_EdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		method       string
		expectResult bool
	}{
		{
			name:         "empty path",
			path:         "",
			method:       "GET",
			expectResult: true, // Should allow empty path
		},
		{
			name:         "root path",
			path:         "/",
			method:       "GET",
			expectResult: true, // Health check path
		},
		{
			name:         "case sensitive path",
			path:         "/API/V1/QUERY",
			method:       "GET",
			expectResult: true, // Should handle case differences
		},
		{
			name:         "path with query parameters",
			path:         "/api/v1/query?query=up",
			method:       "GET",
			expectResult: true, // Query params should not affect access control
		},
		{
			name:         "unknown path",
			path:         "/unknown/endpoint",
			method:       "GET",
			expectResult: true, // Unknown paths should be allowed (will be handled by upstream)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &testutils.MockLogger{}
			service := access.NewService(logger)

			user := &domain.User{
				ID:     "user@example.com",
				Groups: []string{"users"},
			}

			err := service.CanAccess(context.Background(), user, tt.path, tt.method)

			if tt.expectResult {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
