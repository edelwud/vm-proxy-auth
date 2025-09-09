package auth_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config/modules/auth"
)

func TestAuthConfig_Validate_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config auth.Config
	}{
		{
			name: "valid HS256 config",
			config: auth.Config{
				JWT: auth.JWTConfig{
					Algorithm: "HS256",
					Secret:    "test-secret-key-32-chars-long",
					Cache: auth.CacheConfig{
						TokenTTL: 15 * time.Minute,
						JwksTTL:  1 * time.Hour,
					},
					Validation: auth.ValidationConfig{
						Audience: false,
						Issuer:   false,
					},
					Claims: auth.ClaimsConfig{
						UserGroups: "groups",
					},
				},
			},
		},
		{
			name: "valid RS256 config",
			config: auth.Config{
				JWT: auth.JWTConfig{
					Algorithm: "RS256",
					JwksURL:   "https://auth.example.com/.well-known/jwks.json",
					Cache: auth.CacheConfig{
						TokenTTL: 15 * time.Minute,
						JwksTTL:  1 * time.Hour,
					},
					Validation: auth.ValidationConfig{
						Audience: false,
						Issuer:   false,
					},
					Claims: auth.ClaimsConfig{
						UserGroups: "groups",
					},
				},
			},
		},
		{
			name: "minimal valid config",
			config: auth.Config{
				JWT: auth.JWTConfig{
					Algorithm: "HS256",
					Secret:    "secret",
					Cache: auth.CacheConfig{
						TokenTTL: 1 * time.Minute,
						JwksTTL:  1 * time.Minute,
					},
					Validation: auth.ValidationConfig{
						Audience: false,
						Issuer:   false,
					},
					Claims: auth.ClaimsConfig{
						UserGroups: "groups",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestAuthConfig_Validate_Failure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      auth.Config
		expectedErr string
	}{
		{
			name: "missing secret and JWKS URL",
			config: auth.Config{
				JWT: auth.JWTConfig{
					Algorithm: "HS256",
					Secret:    "",
					JwksURL:   "",
				},
			},
			expectedErr: "either jwksUrl or secret is required",
		},
		{
			name: "unsupported algorithm",
			config: auth.Config{
				JWT: auth.JWTConfig{
					Algorithm: "PS256",
					Secret:    "test-secret",
				},
			},
			expectedErr: "unsupported JWT algorithm",
		},
		{
			name: "HS256 without secret",
			config: auth.Config{
				JWT: auth.JWTConfig{
					Algorithm: "HS256",
					Secret:    "",
					JwksURL:   "https://example.com", // Has JWKS but needs secret for HS256
				},
			},
			expectedErr: "HS256 algorithm requires secret",
		},
		{
			name: "RS256 without JWKS URL",
			config: auth.Config{
				JWT: auth.JWTConfig{
					Algorithm: "RS256",
					JwksURL:   "",
					Secret:    "some-secret", // Has secret but needs JWKS for RS256
				},
			},
			expectedErr: "RS256 algorithm requires jwksUrl",
		},
		{
			name: "zero token TTL",
			config: auth.Config{
				JWT: auth.JWTConfig{
					Algorithm: "HS256",
					Secret:    "test-secret-key-32-chars-long",
					Cache: auth.CacheConfig{
						TokenTTL: 0,
						JwksTTL:  1 * time.Hour,
					},
				},
			},
			expectedErr: "token TTL must be positive",
		},
		{
			name: "zero JWKS TTL",
			config: auth.Config{
				JWT: auth.JWTConfig{
					Algorithm: "HS256",
					Secret:    "test-secret-key-32-chars-long",
					Cache: auth.CacheConfig{
						TokenTTL: 15 * time.Minute,
						JwksTTL:  0,
					},
				},
			},
			expectedErr: "JWKS TTL must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestAuthConfig_Defaults(t *testing.T) {
	t.Parallel()

	defaults := auth.GetDefaults()

	assert.Equal(t, "RS256", defaults.JWT.Algorithm)
	assert.Equal(t, 1*time.Hour, defaults.JWT.Cache.TokenTTL)
	assert.Equal(t, 15*time.Minute, defaults.JWT.Cache.JwksTTL)
	assert.Equal(t, "groups", defaults.JWT.Claims.UserGroups)
	assert.False(t, defaults.JWT.Validation.Audience)
	assert.False(t, defaults.JWT.Validation.Issuer)
}

func TestJWTConfig_AlgorithmValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		algorithm string
		secret    string
		jwksURL   string
		valid     bool
	}{
		{"HS256 with secret", "HS256", "test-secret", "", true},
		{"RS256 with JWKS", "RS256", "", "https://example.com/.well-known/jwks.json", true},
		{"HS384 unsupported", "HS384", "test-secret", "", false},
		{"HS512 unsupported", "HS512", "test-secret", "", false},
		{"RS384 unsupported", "RS384", "", "https://example.com/.well-known/jwks.json", false},
		{"RS512 unsupported", "RS512", "", "https://example.com/.well-known/jwks.json", false},
		{"ES256 unsupported", "ES256", "", "https://example.com/.well-known/jwks.json", false},
		{"unknown algorithm", "UNKNOWN", "test-secret", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config := auth.JWTConfig{
				Algorithm: tt.algorithm,
				Secret:    tt.secret,
				JwksURL:   tt.jwksURL,
				Cache: auth.CacheConfig{
					TokenTTL: 15 * time.Minute,
					JwksTTL:  1 * time.Hour,
				},
			}
			err := config.Validate()
			if tt.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
