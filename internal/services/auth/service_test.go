package auth_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/auth"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestNewService_WithSecretAuth(t *testing.T) {
	logger := &testutils.MockLogger{}
	metrics := &MockMetricsService{}

	cfg := config.AuthSettings{
		JWT: config.JWTSettings{
			Algorithm: "HS256",
			Secret:    "test-secret-key",
			TokenTTL:  time.Hour,
			CacheTTL:  5 * time.Minute,
		},
	}

	tenantMaps := []config.TenantMap{
		{
			Groups: []string{"admin"},
			VMTenants: []config.VMTenantInfo{
				{AccountID: "1000", ProjectID: "admin"},
			},
		},
	}

	service := auth.NewService(cfg, tenantMaps, logger, metrics)
	require.NotNil(t, service)
}

func TestNewService_WithJWKSAuth(t *testing.T) {
	logger := &testutils.MockLogger{}
	metrics := &MockMetricsService{}

	cfg := config.AuthSettings{
		JWT: config.JWTSettings{
			Algorithm: "RS256",
			JwksURL:   "https://example.com/.well-known/jwks.json",
			TokenTTL:  time.Hour,
			CacheTTL:  5 * time.Minute,
		},
	}

	tenantMaps := []config.TenantMap{}

	service := auth.NewService(cfg, tenantMaps, logger, metrics)
	require.NotNil(t, service)
}

func TestNewService_PanicsWithoutAuthConfig(t *testing.T) {
	logger := &testutils.MockLogger{}
	metrics := &MockMetricsService{}

	cfg := config.AuthSettings{
		JWT: config.JWTSettings{
			Algorithm: "HS256",
			// No Secret or JwksURL - should panic
		},
	}

	tenantMaps := []config.TenantMap{}

	assert.Panics(t, func() {
		auth.NewService(cfg, tenantMaps, logger, metrics)
	})
}

func TestService_Authenticate_ValidToken(t *testing.T) {
	tests := []struct {
		name            string
		tokenClaims     jwt.MapClaims
		tenantMappings  []config.TenantMap
		expectGroups    []string
		expectVMTenants []domain.VMTenant
		expectError     bool
	}{
		{
			name: "valid token with groups",
			tokenClaims: jwt.MapClaims{
				"sub":    "user@example.com",
				"groups": []interface{}{"admin", "users"},
				"exp":    time.Now().Add(time.Hour).Unix(),
				"iat":    time.Now().Unix(),
			},
			tenantMappings: []config.TenantMap{
				{
					Groups: []string{"admin"},
					VMTenants: []config.VMTenantInfo{
						{AccountID: "1000", ProjectID: "admin"},
					},
				},
			},
			expectGroups: []string{"admin", "users"},
			expectVMTenants: []domain.VMTenant{
				{AccountID: "1000", ProjectID: "admin"},
			},
		},
		{
			name: "valid token without groups",
			tokenClaims: jwt.MapClaims{
				"sub": "user@example.com",
				"exp": time.Now().Add(time.Hour).Unix(),
				"iat": time.Now().Unix(),
			},
			tenantMappings:  []config.TenantMap{},
			expectGroups:    []string{},
			expectVMTenants: []domain.VMTenant{},
		},
		{
			name: "expired token",
			tokenClaims: jwt.MapClaims{
				"sub": "user@example.com",
				"exp": time.Now().Add(-time.Hour).Unix(), // Expired
				"iat": time.Now().Add(-2 * time.Hour).Unix(),
			},
			expectError: true,
		},
		{
			name: "token without sub claim",
			tokenClaims: jwt.MapClaims{
				"exp": time.Now().Add(time.Hour).Unix(),
				"iat": time.Now().Unix(),
				// Missing 'sub' claim
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &testutils.MockLogger{}
			metrics := &MockMetricsService{}

			cfg := config.AuthSettings{
				JWT: config.JWTSettings{
					Algorithm: "HS256",
					Secret:    "test-secret-key",
					TokenTTL:  time.Hour,
					CacheTTL:  5 * time.Minute,
				},
			}

			service := auth.NewService(cfg, tt.tenantMappings, logger, metrics)

			// Create test JWT token
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, tt.tokenClaims)
			tokenString, err := token.SignedString([]byte("test-secret-key"))
			require.NoError(t, err)

			// Test authentication
			user, err := service.Authenticate(context.Background(), tokenString)

			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, user)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, user)

			assert.Equal(t, tt.tokenClaims["sub"], user.ID)
			assert.Equal(t, tt.expectGroups, user.Groups)
			assert.Equal(t, tt.expectVMTenants, user.VMTenants)
		})
	}
}

func TestService_Authenticate_InvalidToken(t *testing.T) {
	logger := &testutils.MockLogger{}
	metrics := &MockMetricsService{}

	cfg := config.AuthSettings{
		JWT: config.JWTSettings{
			Algorithm: "HS256",
			Secret:    "test-secret-key",
			TokenTTL:  time.Hour,
			CacheTTL:  5 * time.Minute,
		},
	}

	service := auth.NewService(cfg, []config.TenantMap{}, logger, metrics)

	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "malformed token",
			token: "invalid.token.format",
		},
		{
			name:  "empty token",
			token: "",
		},
		{
			name:  "token with wrong signature",
			token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyQGV4YW1wbGUuY29tIiwiZXhwIjoxNjk5OTk5OTk5fQ.wrong_signature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := service.Authenticate(context.Background(), tt.token)
			require.Error(t, err)
			assert.Nil(t, user)
		})
	}
}

func TestService_TenantMapping_ComplexScenarios(t *testing.T) {
	tests := []struct {
		name             string
		userGroups       []string
		tenantMappings   []config.TenantMap
		expectedTenants  []domain.VMTenant
		expectedReadOnly bool
	}{
		{
			name:       "multiple group mappings",
			userGroups: []string{"dev", "qa"},
			tenantMappings: []config.TenantMap{
				{
					Groups: []string{"dev"},
					VMTenants: []config.VMTenantInfo{
						{AccountID: "1000", ProjectID: "dev"},
					},
				},
				{
					Groups: []string{"qa"},
					VMTenants: []config.VMTenantInfo{
						{AccountID: "2000", ProjectID: "qa"},
					},
				},
			},
			expectedTenants: []domain.VMTenant{
				{AccountID: "1000", ProjectID: "dev"},
				{AccountID: "2000", ProjectID: "qa"},
			},
		},
		{
			name:       "read-only mapping",
			userGroups: []string{"viewers"},
			tenantMappings: []config.TenantMap{
				{
					Groups:   []string{"viewers"},
					ReadOnly: true,
					VMTenants: []config.VMTenantInfo{
						{AccountID: "3000", ProjectID: "readonly"},
					},
				},
			},
			expectedTenants: []domain.VMTenant{
				{AccountID: "3000", ProjectID: "readonly"},
			},
			expectedReadOnly: true,
		},
		{
			name:            "no matching groups",
			userGroups:      []string{"unknown"},
			tenantMappings:  []config.TenantMap{},
			expectedTenants: []domain.VMTenant{},
		},
		{
			name:       "duplicate tenant deduplication",
			userGroups: []string{"group1", "group2"},
			tenantMappings: []config.TenantMap{
				{
					Groups: []string{"group1"},
					VMTenants: []config.VMTenantInfo{
						{AccountID: "1000", ProjectID: "shared"},
					},
				},
				{
					Groups: []string{"group2"},
					VMTenants: []config.VMTenantInfo{
						{AccountID: "1000", ProjectID: "shared"}, // Same tenant
					},
				},
			},
			expectedTenants: []domain.VMTenant{
				{AccountID: "1000", ProjectID: "shared"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &testutils.MockLogger{}
			metrics := &MockMetricsService{}

			cfg := config.AuthSettings{
				JWT: config.JWTSettings{
					Algorithm: "HS256",
					Secret:    "test-secret-key",
					TokenTTL:  time.Hour,
					CacheTTL:  5 * time.Minute,
					Claims: config.JWTClaimsSettings{
						UserGroupsClaim: "groups",
					},
				},
			}

			service := auth.NewService(cfg, tt.tenantMappings, logger, metrics)

			// Create token with specified groups
			tokenClaims := jwt.MapClaims{
				"sub":    "user@example.com",
				"groups": tt.userGroups,
				"exp":    time.Now().Add(time.Hour).Unix(),
				"iat":    time.Now().Unix(),
			}

			token := jwt.NewWithClaims(jwt.SigningMethodHS256, tokenClaims)
			tokenString, err := token.SignedString([]byte("test-secret-key"))
			require.NoError(t, err)

			// Test authentication
			user, err := service.Authenticate(context.Background(), tokenString)
			require.NoError(t, err)
			require.NotNil(t, user)

			assert.Equal(t, tt.expectedTenants, user.VMTenants)
			assert.Equal(t, tt.expectedReadOnly, user.ReadOnly)
		})
	}
}

func TestService_UserCaching(t *testing.T) {
	logger := &testutils.MockLogger{}
	metrics := &MockMetricsService{}

	cfg := config.AuthSettings{
		JWT: config.JWTSettings{
			Algorithm: "HS256",
			Secret:    "test-secret-key",
			TokenTTL:  time.Hour,
			CacheTTL:  100 * time.Millisecond, // Short TTL for testing
		},
	}

	service := auth.NewService(cfg, []config.TenantMap{}, logger, metrics)

	// Create test token
	tokenClaims := jwt.MapClaims{
		"sub": "cached-user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, tokenClaims)
	tokenString, err := token.SignedString([]byte("test-secret-key"))
	require.NoError(t, err)

	// First authentication - should cache
	user1, err := service.Authenticate(context.Background(), tokenString)
	require.NoError(t, err)
	assert.Equal(t, "cached-user@example.com", user1.ID)

	// Second authentication - should use cache
	user2, err := service.Authenticate(context.Background(), tokenString)
	require.NoError(t, err)
	assert.Equal(t, "cached-user@example.com", user2.ID)

	// Wait for cache expiry
	time.Sleep(150 * time.Millisecond)

	// Third authentication - cache expired, should re-authenticate
	user3, err := service.Authenticate(context.Background(), tokenString)
	require.NoError(t, err)
	assert.Equal(t, "cached-user@example.com", user3.ID)
}

// MockMetricsService implements domain.MetricsService for testing.
type MockMetricsService struct{}

func (m *MockMetricsService) RecordRequest(context.Context, string, string, string, time.Duration, *domain.User) {
}

func (m *MockMetricsService) RecordUpstream(context.Context, string, string, string, time.Duration, []string) {
}
func (m *MockMetricsService) RecordQueryFilter(context.Context, string, int, bool, time.Duration) {}
func (m *MockMetricsService) RecordAuthAttempt(_ context.Context, _, _ string)                    {}
func (m *MockMetricsService) RecordTenantAccess(context.Context, string, string, bool)            {}

// Backend-specific metrics.
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
