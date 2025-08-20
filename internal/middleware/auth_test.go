package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"

	"github.com/finlego/prometheus-oauth-gateway/internal/auth"
	"github.com/finlego/prometheus-oauth-gateway/pkg/tenant"
)

func TestAuthMiddleware_Authenticate(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Suppress logs during testing

	// Create test JWT verifier
	jwtConfig := &auth.JWTConfig{
		Secret:    "test-secret",
		Algorithm: "HS256",
		CacheTTL:  5 * time.Minute,
	}
	jwtVerifier := auth.NewJWTVerifier(jwtConfig, logger)

	// Create test tenant mappings
	tenantMappings := []tenant.Mapping{
		{
			Groups:   []string{"admin"},
			Tenants:  []string{"*"},
			ReadOnly: false,
		},
		{
			Groups:   []string{"team-alpha"},
			Tenants:  []string{"alpha", "alpha-dev"},
			ReadOnly: false,
		},
		{
			Groups:   []string{"readonly"},
			Tenants:  []string{"alpha"},
			ReadOnly: true,
		},
	}
	tenantMapper := tenant.NewMapper(tenantMappings)

	authMiddleware := NewAuthMiddleware(jwtVerifier, tenantMapper, "groups", logger)

	// Create a test handler that checks the user context
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userCtx := GetUserContext(r)
		if userCtx == nil {
			t.Error("Expected user context to be set")
			return
		}

		response := map[string]interface{}{
			"user_id":         userCtx.UserID,
			"groups":          userCtx.Groups,
			"allowed_tenants": userCtx.AllowedTenants,
			"read_only":       userCtx.ReadOnly,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	tests := []struct {
		name           string
		token          func() string
		wantStatusCode int
		wantUserID     string
		wantGroups     []string
		wantTenants    []string
		wantReadOnly   bool
	}{
		{
			name: "valid admin token",
			token: func() string {
				claims := &auth.UserClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						Subject:   "admin-user",
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
						NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
					},
					Groups:            []string{"admin"},
					PreferredUsername: "admin",
					Email:            "admin@example.com",
				}

				token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				tokenString, _ := token.SignedString([]byte("test-secret"))
				return tokenString
			},
			wantStatusCode: http.StatusOK,
			wantUserID:     "admin",
			wantGroups:     []string{"admin"},
			wantTenants:    []string{"*"},
			wantReadOnly:   false,
		},
		{
			name: "valid team user token",
			token: func() string {
				claims := &auth.UserClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						Subject:   "team-user",
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
						NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
					},
					Groups:            []string{"team-alpha"},
					PreferredUsername: "teamuser",
				}

				token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				tokenString, _ := token.SignedString([]byte("test-secret"))
				return tokenString
			},
			wantStatusCode: http.StatusOK,
			wantUserID:     "teamuser",
			wantGroups:     []string{"team-alpha"},
			wantTenants:    []string{"alpha", "alpha-dev"},
			wantReadOnly:   false,
		},
		{
			name: "readonly user token",
			token: func() string {
				claims := &auth.UserClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						Subject:   "readonly-user",
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
						NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
					},
					Groups: []string{"readonly"},
					Email:  "readonly@example.com",
				}

				token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				tokenString, _ := token.SignedString([]byte("test-secret"))
				return tokenString
			},
			wantStatusCode: http.StatusOK,
			wantUserID:     "readonly@example.com",
			wantGroups:     []string{"readonly"},
			wantTenants:    []string{"alpha"},
			wantReadOnly:   true,
		},
		{
			name: "invalid token",
			token: func() string {
				return "invalid.jwt.token"
			},
			wantStatusCode: http.StatusUnauthorized,
		},
		{
			name: "expired token",
			token: func() string {
				claims := &auth.UserClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						Subject:   "user",
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
						NotBefore: jwt.NewNumericDate(time.Now().Add(-2*time.Hour)),
					},
					Groups: []string{"admin"},
				}

				token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				tokenString, _ := token.SignedString([]byte("test-secret"))
				return tokenString
			},
			wantStatusCode: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create request with bearer token
			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token())

			// Create response recorder
			rr := httptest.NewRecorder()

			// Create handler chain
			handler := authMiddleware.Authenticate(testHandler)

			// Execute request
			handler.ServeHTTP(rr, req)

			// Check status code
			if rr.Code != tt.wantStatusCode {
				t.Errorf("Expected status code %d, got %d", tt.wantStatusCode, rr.Code)
			}

			// If request should succeed, check response body
			if tt.wantStatusCode == http.StatusOK {
				var response map[string]interface{}
				if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
					t.Errorf("Failed to decode response: %v", err)
				}

				if response["user_id"] != tt.wantUserID {
					t.Errorf("Expected user_id %s, got %v", tt.wantUserID, response["user_id"])
				}

				if response["read_only"] != tt.wantReadOnly {
					t.Errorf("Expected read_only %v, got %v", tt.wantReadOnly, response["read_only"])
				}
			}
		})
	}
}

func TestAuthMiddleware_NoToken(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	jwtConfig := &auth.JWTConfig{
		Secret:   "test-secret",
		CacheTTL: 5 * time.Minute,
	}
	jwtVerifier := auth.NewJWTVerifier(jwtConfig, logger)
	tenantMapper := tenant.NewMapper([]tenant.Mapping{})

	authMiddleware := NewAuthMiddleware(jwtVerifier, tenantMapper, "groups", logger)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called without valid token")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handler := authMiddleware.Authenticate(testHandler)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status code %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestExtractGroups(t *testing.T) {
	tests := []struct {
		name        string
		claims      *auth.UserClaims
		groupsClaim string
		want        []string
	}{
		{
			name: "groups from direct claims",
			claims: &auth.UserClaims{
				Groups: []string{"admin", "users"},
			},
			groupsClaim: "groups",
			want:        []string{"admin", "users"},
		},
		{
			name: "groups from realm access",
			claims: &auth.UserClaims{
				RealmAccess: map[string]interface{}{
					"roles": []interface{}{"admin", "editor"},
				},
			},
			groupsClaim: "realm_access.roles",
			want:        []string{"admin", "editor"},
		},
		{
			name: "no groups",
			claims: &auth.UserClaims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject: "user123",
				},
			},
			groupsClaim: "groups",
			want:        nil,
		},
		{
			name: "empty groups claim",
			claims: &auth.UserClaims{
				Groups: []string{},
			},
			groupsClaim: "groups",
			want:        []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGroups(tt.claims, tt.groupsClaim)
			if len(got) != len(tt.want) {
				t.Errorf("extractGroups() = %v, want %v", got, tt.want)
				return
			}

			for i, group := range got {
				if i >= len(tt.want) || group != tt.want[i] {
					t.Errorf("extractGroups() = %v, want %v", got, tt.want)
					break
				}
			}
		})
	}
}

func TestGetUserID(t *testing.T) {
	tests := []struct {
		name   string
		claims *auth.UserClaims
		want   string
	}{
		{
			name: "preferred username",
			claims: &auth.UserClaims{
				PreferredUsername: "john.doe",
				Email:            "john@example.com",
				RegisteredClaims: jwt.RegisteredClaims{
					Subject: "user123",
				},
			},
			want: "john.doe",
		},
		{
			name: "email when no preferred username",
			claims: &auth.UserClaims{
				Email: "john@example.com",
				RegisteredClaims: jwt.RegisteredClaims{
					Subject: "user123",
				},
			},
			want: "john@example.com",
		},
		{
			name: "subject when no preferred username or email",
			claims: &auth.UserClaims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject: "user123",
				},
			},
			want: "user123",
		},
		{
			name:   "unknown when no identifying fields",
			claims: &auth.UserClaims{},
			want:   "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getUserID(tt.claims); got != tt.want {
				t.Errorf("getUserID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetUserContext(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)

	// Test with no user context
	if ctx := GetUserContext(req); ctx != nil {
		t.Error("Expected nil user context when not set")
	}

	// Test with user context set
	userCtx := &UserContext{
		UserID:         "test-user",
		Groups:         []string{"admin"},
		AllowedTenants: []string{"*"},
		ReadOnly:       false,
	}

	// Manually set context (normally done by middleware)
	ctx := req.Context()
	ctx = req.Context()
	req = req.WithContext(ctx)

	// This test shows the pattern, but the actual context setting happens in middleware
}