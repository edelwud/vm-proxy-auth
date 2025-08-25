package auth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/services/auth"
)

func TestJWTVerifier_HS256_ValidToken(t *testing.T) {
	secret := []byte("test-secret-key")
	verifier := auth.NewJWTVerifier(nil, secret, "HS256")

	// Create valid token
	claims := jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(secret)
	require.NoError(t, err)

	// Verify token
	parsedClaims, err := verifier.VerifyToken(tokenString)
	require.NoError(t, err)
	assert.Equal(t, "user@example.com", parsedClaims.UserID)
}

func TestJWTVerifier_HS256_ExpiredToken(t *testing.T) {
	secret := []byte("test-secret-key")
	verifier := auth.NewJWTVerifier(nil, secret, "HS256")

	// Create expired token
	claims := jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(-time.Hour).Unix(), // Expired 1 hour ago
		"iat": time.Now().Add(-2 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(secret)
	require.NoError(t, err)

	// Verify token - should fail
	_, err = verifier.VerifyToken(tokenString)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Token has expired")
}

func TestJWTVerifier_HS256_InvalidSignature(t *testing.T) {
	secret := []byte("test-secret-key")
	wrongSecret := []byte("wrong-secret-key")
	verifier := auth.NewJWTVerifier(nil, secret, "HS256")

	// Create token with wrong secret
	claims := jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(wrongSecret)
	require.NoError(t, err)

	// Verify token - should fail
	_, err = verifier.VerifyToken(tokenString)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature is invalid")
}

func TestJWTVerifier_RS256_ValidToken(t *testing.T) {
	// Generate RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	publicKey := &privateKey.PublicKey

	verifier := auth.NewJWTVerifier(publicKey, nil, "RS256")

	// Create valid token
	claims := jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, err := token.SignedString(privateKey)
	require.NoError(t, err)

	// Verify token
	parsedClaims, err := verifier.VerifyToken(tokenString)
	require.NoError(t, err)
	assert.Equal(t, "user@example.com", parsedClaims.UserID)
}

func TestJWTVerifier_RS256_InvalidPublicKey(t *testing.T) {
	// Generate two different RSA key pairs
	privateKey1, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	privateKey2, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	publicKey2 := &privateKey2.PublicKey

	verifier := auth.NewJWTVerifier(publicKey2, nil, "RS256")

	// Create token with first key
	claims := jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, err := token.SignedString(privateKey1)
	require.NoError(t, err)

	// Verify with second key - should fail
	_, err = verifier.VerifyToken(tokenString)
	require.Error(t, err)
}

func TestJWTVerifier_UnsupportedAlgorithm(t *testing.T) {
	secret := []byte("test-secret-key")

	// Create verifier with unsupported algorithm - should still create but fail on verification
	verifier := auth.NewJWTVerifier(nil, secret, "UNSUPPORTED")

	// Create token
	claims := jwt.MapClaims{
		"sub": "user@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(secret)
	require.NoError(t, err)

	// Verify token should fail with unsupported algorithm
	_, err = verifier.VerifyToken(tokenString)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Unsupported")
}

func TestJWTVerifier_MalformedTokens(t *testing.T) {
	secret := []byte("test-secret-key")
	verifier := auth.NewJWTVerifier(nil, secret, "HS256")

	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "empty token",
			token: "",
		},
		{
			name:  "malformed token - not enough parts",
			token: "header.payload",
		},
		{
			name:  "malformed token - too many parts",
			token: "header.payload.signature.extra",
		},
		{
			name:  "malformed token - invalid base64",
			token: "invalid.base64!.signature",
		},
		{
			name:  "malformed token - invalid JSON",
			token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.invalid_json.signature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := verifier.VerifyToken(tt.token)
			require.Error(t, err)
		})
	}
}

func TestJWTVerifier_MissingRequiredClaims(t *testing.T) {
	secret := []byte("test-secret-key")
	verifier := auth.NewJWTVerifier(nil, secret, "HS256")

	tests := []struct {
		name   string
		claims jwt.MapClaims
	}{
		{
			name: "missing exp claim",
			claims: jwt.MapClaims{
				"sub": "user@example.com",
				"iat": time.Now().Unix(),
				// Missing "exp"
			},
		},
		{
			name: "missing iat claim",
			claims: jwt.MapClaims{
				"sub": "user@example.com",
				"exp": time.Now().Add(time.Hour).Unix(),
				// Missing "iat"
			},
		},
		{
			name: "invalid exp type",
			claims: jwt.MapClaims{
				"sub": "user@example.com",
				"exp": "not-a-number",
				"iat": time.Now().Unix(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, tt.claims)
			tokenString, err := token.SignedString(secret)
			require.NoError(t, err)

			_, err = verifier.VerifyToken(tokenString)
			require.Error(t, err)
		})
	}
}

func TestJWTVerifier_TokenTiming(t *testing.T) {
	secret := []byte("test-secret-key")
	verifier := auth.NewJWTVerifier(nil, secret, "HS256")

	now := time.Now()

	tests := []struct {
		name        string
		iat         int64
		exp         int64
		expectError bool
	}{
		{
			name: "valid timing",
			iat:  now.Add(-time.Minute).Unix(),
			exp:  now.Add(time.Hour).Unix(),
		},
		{
			name:        "token not yet valid (iat in future)",
			iat:         now.Add(time.Hour).Unix(),
			exp:         now.Add(2 * time.Hour).Unix(),
			expectError: true,
		},
		{
			name:        "token expired",
			iat:         now.Add(-2 * time.Hour).Unix(),
			exp:         now.Add(-time.Hour).Unix(),
			expectError: true,
		},
		{
			name: "token issued exactly now",
			iat:  now.Unix(),
			exp:  now.Add(time.Hour).Unix(),
		},
		{
			name: "token expires exactly now",
			iat:  now.Add(-time.Hour).Unix(),
			exp:  now.Unix(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := jwt.MapClaims{
				"sub": "user@example.com",
				"iat": tt.iat,
				"exp": tt.exp,
			}

			token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
			tokenString, err := token.SignedString(secret)
			require.NoError(t, err)

			_, err = verifier.VerifyToken(tokenString)

			if tt.expectError {
				require.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestJWTVerifier_CorrectClaimsExtraction(t *testing.T) {
	secret := []byte("test-secret-key")
	verifier := auth.NewJWTVerifier(nil, secret, "HS256")

	// Create token with various claim types
	claims := jwt.MapClaims{
		"sub":    "user@example.com",
		"aud":    "test-audience",
		"iss":    "test-issuer",
		"groups": []string{"admin", "users"},
		"roles":  []interface{}{"manager", "developer"},
		"level":  5,
		"active": true,
		"exp":    time.Now().Add(time.Hour).Unix(),
		"iat":    time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(secret)
	require.NoError(t, err)

	// Verify and extract claims
	parsedClaims, err := verifier.VerifyToken(tokenString)
	require.NoError(t, err)

	// Verify all claims are correctly extracted
	assert.Equal(t, "user@example.com", parsedClaims.UserID)
	assert.Equal(t, "test-audience", parsedClaims.Audience)
	assert.Equal(t, "test-issuer", parsedClaims.Issuer)
	assert.Equal(t, []string{"admin", "users"}, parsedClaims.Groups)
}
