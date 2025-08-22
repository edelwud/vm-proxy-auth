package auth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/services/auth"
)

func TestJWKSFetcher_ValidJWKS(t *testing.T) {
	// Generate test RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	// Create mock JWKS response
	jwks := createMockJWKS(t, &privateKey.PublicKey, "test-kid")
	
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	// Create JWKS fetcher
	fetcher := auth.NewJWKSFetcher(server.URL, 5*time.Minute)

	// Test fetching public key
	publicKey, err := fetcher.GetPublicKey("test-kid")
	require.NoError(t, err)
	require.NotNil(t, publicKey)

	// Verify it's the correct public key
	assert.Equal(t, privateKey.PublicKey.N, publicKey.N)
	assert.Equal(t, privateKey.PublicKey.E, publicKey.E)
}

func TestJWKSFetcher_KeyNotFound(t *testing.T) {
	// Create mock JWKS with different key ID
	jwks := map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"kty": "RSA",
				"kid": "different-kid",
				"use": "sig",
				"n":   "test-modulus",
				"e":   "AQAB",
			},
		},
	}

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	// Create JWKS fetcher
	fetcher := auth.NewJWKSFetcher(server.URL, 5*time.Minute)

	// Test fetching non-existent key
	publicKey, err := fetcher.GetPublicKey("non-existent-kid")
	assert.Error(t, err)
	assert.Nil(t, publicKey)
	assert.Contains(t, err.Error(), "key not found")
}

func TestJWKSFetcher_InvalidJWKSResponse(t *testing.T) {
	tests := []struct {
		name         string
		responseBody interface{}
		statusCode   int
	}{
		{
			name:         "invalid JSON",
			responseBody: "invalid json",
			statusCode:   200,
		},
		{
			name:         "missing keys field",
			responseBody: map[string]interface{}{"not_keys": []interface{}{}},
			statusCode:   200,
		},
		{
			name:         "HTTP error",
			responseBody: map[string]interface{}{"error": "not found"},
			statusCode:   404,
		},
		{
			name:         "server error",
			responseBody: map[string]interface{}{"error": "internal error"},
			statusCode:   500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server with error response
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				if tt.responseBody != nil {
					if str, ok := tt.responseBody.(string); ok {
						w.Write([]byte(str))
					} else {
						json.NewEncoder(w).Encode(tt.responseBody)
					}
				}
			}))
			defer server.Close()

			// Create JWKS fetcher
			fetcher := auth.NewJWKSFetcher(server.URL, 5*time.Minute)

			// Test fetching key - should fail
			publicKey, err := fetcher.GetPublicKey("test-kid")
			assert.Error(t, err)
			assert.Nil(t, publicKey)
		})
	}
}

func TestJWKSFetcher_Caching(t *testing.T) {
	// Generate test RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	requestCount := 0
	
	// Create test server that counts requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		jwks := createMockJWKS(t, &privateKey.PublicKey, "test-kid")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	// Create JWKS fetcher with short cache TTL
	fetcher := auth.NewJWKSFetcher(server.URL, 100*time.Millisecond)

	// First request - should fetch from server
	publicKey1, err := fetcher.GetPublicKey("test-kid")
	require.NoError(t, err)
	require.NotNil(t, publicKey1)
	assert.Equal(t, 1, requestCount)

	// Second request immediately - should use cache
	publicKey2, err := fetcher.GetPublicKey("test-kid")
	require.NoError(t, err)
	require.NotNil(t, publicKey2)
	assert.Equal(t, 1, requestCount) // No additional request

	// Wait for cache expiry
	time.Sleep(150 * time.Millisecond)

	// Third request - should fetch from server again
	publicKey3, err := fetcher.GetPublicKey("test-kid")
	require.NoError(t, err)
	require.NotNil(t, publicKey3)
	assert.Equal(t, 2, requestCount) // New request made
}

func TestJWKSFetcher_InvalidRSAKey(t *testing.T) {
	tests := []struct {
		name string
		jwk  map[string]interface{}
	}{
		{
			name: "missing modulus (n)",
			jwk: map[string]interface{}{
				"kty": "RSA",
				"kid": "test-kid",
				"use": "sig",
				"e":   "AQAB",
				// Missing "n"
			},
		},
		{
			name: "missing exponent (e)",
			jwk: map[string]interface{}{
				"kty": "RSA",
				"kid": "test-kid",
				"use": "sig",
				"n":   "test-modulus",
				// Missing "e"
			},
		},
		{
			name: "invalid modulus encoding",
			jwk: map[string]interface{}{
				"kty": "RSA",
				"kid": "test-kid", 
				"use": "sig",
				"n":   "invalid-base64!",
				"e":   "AQAB",
			},
		},
		{
			name: "invalid exponent encoding", 
			jwk: map[string]interface{}{
				"kty": "RSA",
				"kid": "test-kid",
				"use": "sig",
				"n":   "dGVzdC1tb2R1bHVz", // "test-modulus" in base64
				"e":   "invalid-base64!",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jwks := map[string]interface{}{
				"keys": []interface{}{tt.jwk},
			}

			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(jwks)
			}))
			defer server.Close()

			// Create JWKS fetcher
			fetcher := auth.NewJWKSFetcher(server.URL, 5*time.Minute)

			// Test fetching key - should fail
			publicKey, err := fetcher.GetPublicKey("test-kid")
			assert.Error(t, err)
			assert.Nil(t, publicKey)
		})
	}
}

func TestJWKSFetcher_NetworkErrors(t *testing.T) {
	// Create JWKS fetcher with invalid URL
	fetcher := auth.NewJWKSFetcher("http://invalid-host-that-does-not-exist.local", 5*time.Minute)

	// Test fetching key - should fail with network error
	publicKey, err := fetcher.GetPublicKey("test-kid")
	assert.Error(t, err)
	assert.Nil(t, publicKey)
}

func TestJWKSFetcher_MultipleKeys(t *testing.T) {
	// Generate multiple test keys
	privateKey1, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	
	privateKey2, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	// Create JWKS with multiple keys
	jwks := map[string]interface{}{
		"keys": []interface{}{
			createJWK(t, &privateKey1.PublicKey, "key-1"),
			createJWK(t, &privateKey2.PublicKey, "key-2"),
		},
	}

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	// Create JWKS fetcher
	fetcher := auth.NewJWKSFetcher(server.URL, 5*time.Minute)

	// Test fetching both keys
	publicKey1, err := fetcher.GetPublicKey("key-1")
	require.NoError(t, err)
	require.NotNil(t, publicKey1)

	publicKey2, err := fetcher.GetPublicKey("key-2")
	require.NoError(t, err) 
	require.NotNil(t, publicKey2)

	// Verify they are different keys
	assert.NotEqual(t, publicKey1.N, publicKey2.N)
}

// Helper functions

func createMockJWKS(t *testing.T, publicKey *rsa.PublicKey, kid string) map[string]interface{} {
	return map[string]interface{}{
		"keys": []interface{}{
			createJWK(t, publicKey, kid),
		},
	}
}

func createJWK(t *testing.T, publicKey *rsa.PublicKey, kid string) map[string]interface{} {
	// Convert RSA public key to JWK format
	nBytes := publicKey.N.Bytes()
	eBytes := big.NewInt(int64(publicKey.E)).Bytes()

	return map[string]interface{}{
		"kty": "RSA",
		"kid": kid,
		"use": "sig",
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(nBytes),
		"e":   base64.RawURLEncoding.EncodeToString(eBytes),
	}
}