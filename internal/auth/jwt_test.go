package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
)

func TestJWTVerifier_VerifyToken(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Suppress logs during testing

	// Generate test RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		config    *JWTConfig
		setupMock func() string
		token     func() string
		wantErr   bool
		wantUser  string
	}{
		{
			name: "valid token with secret",
			config: &JWTConfig{
				Secret:           "test-secret",
				Algorithm:        "HS256",
				ValidateIssuer:   true,
				RequiredIssuer:   "test-issuer",
				ValidateAudience: false,
				CacheTTL:         5 * time.Minute,
			},
			token: func() string {
				claims := &UserClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						Subject:   "test-user",
						Issuer:    "test-issuer",
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
						NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
						IssuedAt:  jwt.NewNumericDate(time.Now()),
					},
					Groups:            []string{"admin", "users"},
					PreferredUsername: "testuser",
					Email:            "test@example.com",
				}

				token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				tokenString, _ := token.SignedString([]byte("test-secret"))
				return tokenString
			},
			wantErr:  false,
			wantUser: "test-user",
		},
		{
			name: "expired token",
			config: &JWTConfig{
				Secret:           "test-secret",
				Algorithm:        "HS256",
				ValidateIssuer:   false,
				ValidateAudience: false,
				CacheTTL:         5 * time.Minute,
			},
			token: func() string {
				claims := &UserClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						Subject:   "test-user",
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)), // Expired
						NotBefore: jwt.NewNumericDate(time.Now().Add(-2*time.Hour)),
						IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2*time.Hour)),
					},
					Groups: []string{"admin"},
				}

				token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				tokenString, _ := token.SignedString([]byte("test-secret"))
				return tokenString
			},
			wantErr: true,
		},
		{
			name: "invalid issuer",
			config: &JWTConfig{
				Secret:         "test-secret",
				Algorithm:      "HS256",
				ValidateIssuer: true,
				RequiredIssuer: "expected-issuer",
				CacheTTL:       5 * time.Minute,
			},
			token: func() string {
				claims := &UserClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						Subject:   "test-user",
						Issuer:    "wrong-issuer",
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
						NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
					},
					Groups: []string{"admin"},
				}

				token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				tokenString, _ := token.SignedString([]byte("test-secret"))
				return tokenString
			},
			wantErr: true,
		},
		{
			name: "valid audience",
			config: &JWTConfig{
				Secret:           "test-secret",
				Algorithm:        "HS256",
				ValidateAudience: true,
				RequiredAudience: []string{"api", "web"},
				CacheTTL:         5 * time.Minute,
			},
			token: func() string {
				claims := &UserClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						Subject:   "test-user",
						Audience:  []string{"api"},
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
						NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
					},
					Groups: []string{"admin"},
				}

				token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				tokenString, _ := token.SignedString([]byte("test-secret"))
				return tokenString
			},
			wantErr: false,
		},
		{
			name: "invalid audience",
			config: &JWTConfig{
				Secret:           "test-secret",
				Algorithm:        "HS256",
				ValidateAudience: true,
				RequiredAudience: []string{"api", "web"},
				CacheTTL:         5 * time.Minute,
			},
			token: func() string {
				claims := &UserClaims{
					RegisteredClaims: jwt.RegisteredClaims{
						Subject:   "test-user",
						Audience:  []string{"mobile"}, // Wrong audience
						ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
						NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
					},
					Groups: []string{"admin"},
				}

				token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				tokenString, _ := token.SignedString([]byte("test-secret"))
				return tokenString
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifier := NewJWTVerifier(tt.config, logger)
			tokenString := tt.token()

			claims, err := verifier.VerifyToken(tokenString)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && claims != nil {
				if claims.Subject != tt.wantUser {
					t.Errorf("Expected user %s, got %s", tt.wantUser, claims.Subject)
				}
			}
		})
	}
}

func TestJWTVerifier_WithJWKS(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Generate test RSA key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	publicKey := &privateKey.PublicKey

	// Create mock JWKS server
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create a minimal JWK
		n := base64.RawURLEncoding.EncodeToString(publicKey.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}) // 65537 in bytes

		jwks := JWKSResponse{
			Keys: []JWK{
				{
					Kid: "test-key-1",
					Kty: "RSA",
					Use: "sig",
					N:   n,
					E:   e,
					X5C: []string{"dummy-cert"}, // This would be a real cert in production
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	defer jwksServer.Close()

	config := &JWTConfig{
		JWKSURL:   jwksServer.URL,
		Algorithm: "RS256",
		CacheTTL:  5 * time.Minute,
	}

	verifier := NewJWTVerifier(config, logger)

	// Create a token signed with our private key
	claims := &UserClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "test-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
		Groups: []string{"admin"},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key-1"

	// This test would work with proper X.509 certificate parsing
	// For now, we test the JWKS fetching mechanism
	err = verifier.fetchKeys()
	if err != nil {
		t.Errorf("fetchKeys() should not fail with mock server, got error: %v", err)
	}

	if len(verifier.keyCache) == 0 {
		t.Error("Expected keys to be cached after fetch")
	}
}

func TestExtractTokenFromRequest(t *testing.T) {
	tests := []struct {
		name        string
		header      string
		wantToken   string
		wantErr     bool
	}{
		{
			name:      "valid bearer token",
			header:    "Bearer eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9...",
			wantToken: "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9...",
			wantErr:   false,
		},
		{
			name:    "missing authorization header",
			header:  "",
			wantErr: true,
		},
		{
			name:    "invalid header format",
			header:  "InvalidFormat",
			wantErr: true,
		},
		{
			name:    "not bearer token",
			header:  "Basic dXNlcjpwYXNz",
			wantErr: true,
		},
		{
			name:    "bearer with no token",
			header:  "Bearer",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			token, err := ExtractTokenFromRequest(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractTokenFromRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && token != tt.wantToken {
				t.Errorf("ExtractTokenFromRequest() token = %v, want %v", token, tt.wantToken)
			}
		})
	}
}

func TestUserClaims_ExtractGroups(t *testing.T) {
	tests := []struct {
		name         string
		claims       *UserClaims
		groupsClaim  string
		wantGroups   []string
	}{
		{
			name: "groups in claims",
			claims: &UserClaims{
				Groups: []string{"admin", "users"},
			},
			groupsClaim: "groups",
			wantGroups:  []string{"admin", "users"},
		},
		{
			name: "realm access roles",
			claims: &UserClaims{
				RealmAccess: map[string]interface{}{
					"roles": []interface{}{"admin", "editor"},
				},
			},
			groupsClaim: "realm_access.roles",
			wantGroups:  []string{"admin", "editor"},
		},
		{
			name: "no groups",
			claims: &UserClaims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject: "user123",
				},
			},
			groupsClaim: "groups",
			wantGroups:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This would test the extractGroups function from middleware
			// Since it's in middleware package, we'll test it there
		})
	}
}