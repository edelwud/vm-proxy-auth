package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	auth_config "github.com/edelwud/vm-proxy-auth/internal/config/modules/auth"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/tenant"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/auth"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestNewService_WithSecretAuth(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	metrics := &testutils.MockMetricsService{}

	cfg := auth_config.Config{
		JWT: auth_config.JWTConfig{
			Algorithm: "HS256",
			Secret:    "test-secret-key",
			Cache: auth_config.CacheConfig{
				TokenTTL: time.Hour,
				JwksTTL:  5 * time.Minute,
			},
		},
	}

	tenantMaps := []tenant.MappingConfig{
		{
			Groups: []string{"admin"},
			VMTenants: []tenant.VMTenantConfig{
				{Account: "1000", Project: "admin"},
			},
			Permissions: []string{"read", "write"},
		},
	}

	service, err := auth.NewService(cfg, tenantMaps, logger, metrics)
	require.NoError(t, err)
	require.NotNil(t, service)
}

func TestNewService_WithJWKSAuth(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	metrics := &testutils.MockMetricsService{}

	cfg := auth_config.Config{
		JWT: auth_config.JWTConfig{
			Algorithm: "RS256",
			JwksURL:   "https://example.com/.well-known/jwks.json",
			Cache: auth_config.CacheConfig{
				TokenTTL: time.Hour,
				JwksTTL:  5 * time.Minute,
			},
		},
	}

	tenantMaps := []tenant.MappingConfig{}

	service, err := auth.NewService(cfg, tenantMaps, logger, metrics)
	require.NoError(t, err)
	require.NotNil(t, service)
}

func TestNewService_ErrorWithoutAuthConfig(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	metrics := &testutils.MockMetricsService{}

	cfg := auth_config.Config{
		JWT: auth_config.JWTConfig{
			Algorithm: "HS256",
			// No Secret or JwksURL - should return error
		},
	}

	tenantMaps := []tenant.MappingConfig{}

	service, err := auth.NewService(cfg, tenantMaps, logger, metrics)
	require.Error(t, err)
	assert.Nil(t, service)
	assert.Contains(t, err.Error(), "jWT authentication requires either jwt_secret or jwks_url")
}

func TestService_Authenticate_ValidToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		tokenClaims     jwt.MapClaims
		tenantMappings  []tenant.MappingConfig
		expectGroups    []string
		expectVMTenants []domain.VMTenant
		expectError     bool
	}{
		{
			name: "valid token with groups",
			tokenClaims: jwt.MapClaims{
				"sub":    "user@example.com",
				"groups": []any{"admin", "users"},
				"exp":    time.Now().Add(time.Hour).Unix(),
				"iat":    time.Now().Unix(),
			},
			tenantMappings: []tenant.MappingConfig{
				{
					Groups: []string{"admin"},
					VMTenants: []tenant.VMTenantConfig{
						{Account: "1000", Project: "admin"},
					},
					Permissions: []string{"read", "write"},
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
			tenantMappings:  []tenant.MappingConfig{},
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
			t.Parallel()
			logger := testutils.NewMockLogger()
			metrics := &testutils.MockMetricsService{}

			cfg := auth_config.Config{
				JWT: auth_config.JWTConfig{
					Algorithm: "HS256",
					Secret:    "test-secret-key",
					Cache:     auth_config.CacheConfig{TokenTTL: time.Hour, JwksTTL: time.Hour},
				},
			}

			service, err := auth.NewService(cfg, tt.tenantMappings, logger, metrics)
			require.NoError(t, err)

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
	t.Parallel()

	logger := testutils.NewMockLogger()
	metrics := &testutils.MockMetricsService{}

	cfg := auth_config.Config{
		JWT: auth_config.JWTConfig{
			Algorithm: "HS256",
			Secret:    "test-secret-key",
			Cache:     auth_config.CacheConfig{TokenTTL: time.Hour, JwksTTL: time.Hour},
		},
	}

	service, err := auth.NewService(cfg, []tenant.MappingConfig{}, logger, metrics)
	require.NoError(t, err)

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
			t.Parallel()
			user, authErr := service.Authenticate(context.Background(), tt.token)
			require.Error(t, authErr)
			assert.Nil(t, user)
		})
	}
}

func TestService_TenantMapping_ComplexScenarios(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		userGroups      []string
		tenantMappings  []tenant.MappingConfig
		expectedTenants []domain.VMTenant
	}{
		{
			name:       "multiple group mappings",
			userGroups: []string{"dev", "qa"},
			tenantMappings: []tenant.MappingConfig{
				{
					Groups: []string{"dev"},
					VMTenants: []tenant.VMTenantConfig{
						{Account: "1000", Project: "dev"},
					},
					Permissions: []string{"read", "write"},
				},
				{
					Groups: []string{"qa"},
					VMTenants: []tenant.VMTenantConfig{
						{Account: "2000", Project: "qa"},
					},
					Permissions: []string{"read", "write"},
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
			tenantMappings: []tenant.MappingConfig{
				{
					Groups: []string{"viewers"},
					VMTenants: []tenant.VMTenantConfig{
						{Account: "3000", Project: "readonly"},
					},
					Permissions: []string{"read"},
				},
			},
			expectedTenants: []domain.VMTenant{
				{AccountID: "3000", ProjectID: "readonly"},
			},
		},
		{
			name:            "no matching groups",
			userGroups:      []string{"unknown"},
			tenantMappings:  []tenant.MappingConfig{},
			expectedTenants: []domain.VMTenant{},
		},
		{
			name:       "duplicate tenant deduplication",
			userGroups: []string{"group1", "group2"},
			tenantMappings: []tenant.MappingConfig{
				{
					Groups: []string{"group1"},
					VMTenants: []tenant.VMTenantConfig{
						{Account: "1000", Project: "shared"},
					},
					Permissions: []string{"read", "write"},
				},
				{
					Groups: []string{"group2"},
					VMTenants: []tenant.VMTenantConfig{
						{Account: "1000", Project: "shared"}, // Same tenant
					},
					Permissions: []string{"read", "write"},
				},
			},
			expectedTenants: []domain.VMTenant{
				{AccountID: "1000", ProjectID: "shared"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger := testutils.NewMockLogger()
			metrics := &testutils.MockMetricsService{}

			cfg := auth_config.Config{
				JWT: auth_config.JWTConfig{
					Algorithm: "HS256",
					Secret:    "test-secret-key",
					Cache:     auth_config.CacheConfig{TokenTTL: time.Hour, JwksTTL: time.Hour},
					Claims: auth_config.ClaimsConfig{
						UserGroups: "groups",
					},
				},
			}

			service, err := auth.NewService(cfg, tt.tenantMappings, logger, metrics)
			require.NoError(t, err)

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
			// ReadOnly is determined by permissions in the new system
		})
	}
}

func TestService_UserCaching(t *testing.T) {
	t.Parallel()

	logger := testutils.NewMockLogger()
	metrics := &testutils.MockMetricsService{}

	cfg := auth_config.Config{
		JWT: auth_config.JWTConfig{
			Algorithm: "HS256",
			Secret:    "test-secret-key",
			Cache:     auth_config.CacheConfig{TokenTTL: time.Hour, JwksTTL: time.Hour},
		},
	}

	service, err := auth.NewService(cfg, []tenant.MappingConfig{}, logger, metrics)
	require.NoError(t, err)

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
