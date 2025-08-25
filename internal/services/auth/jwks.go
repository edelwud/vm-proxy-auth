package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// JWKS represents the JSON Web Key Set structure.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWK represents a JSON Web Key.
type JWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
	Alg string `json:"alg"`
}

// JWKSFetcher handles fetching and caching JWKS.
type JWKSFetcher struct {
	client    *http.Client
	jwksURL   string
	cache     map[string]*rsa.PublicKey
	cacheTime time.Time
	cacheTTL  time.Duration
}

// NewJWKSFetcher creates a new JWKS fetcher.
func NewJWKSFetcher(jwksURL string, cacheTTL time.Duration) *JWKSFetcher {
	return &JWKSFetcher{
		client: &http.Client{
			Timeout: domain.DefaultTimeout,
		},
		jwksURL:  jwksURL,
		cache:    make(map[string]*rsa.PublicKey),
		cacheTTL: cacheTTL,
	}
}

// GetPublicKey fetches and returns the public key for the given kid.
func (f *JWKSFetcher) GetPublicKey(kid string) (*rsa.PublicKey, error) {
	// Check cache first
	if key, exists := f.cache[kid]; exists && time.Since(f.cacheTime) < f.cacheTTL {
		return key, nil
	}

	// Fetch JWKS
	if err := f.fetchJWKS(); err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	// Get key from cache after fetch
	if key, exists := f.cache[kid]; exists {
		return key, nil
	}

	return nil, fmt.Errorf("%w: kid '%s'", domain.ErrJWKSKeyNotFound, kid)
}

// fetchJWKS fetches the JWKS and updates the cache.
func (f *JWKSFetcher) fetchJWKS() error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, f.jwksURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch JWKS from %s: %w", f.jwksURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", domain.ErrJWKSEndpointError, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read JWKS response: %w", err)
	}

	var jwks JWKS
	if unmarshalErr := json.Unmarshal(body, &jwks); unmarshalErr != nil {
		return fmt.Errorf("failed to parse JWKS: %w", unmarshalErr)
	}

	// Clear cache and update with new keys
	f.cache = make(map[string]*rsa.PublicKey)
	f.cacheTime = time.Now()

	for _, jwk := range jwks.Keys {
		if jwk.Kty == "RSA" && (jwk.Use == "sig" || jwk.Use == "") {
			key, keyErr := f.jwkToRSAPublicKey(jwk)
			if keyErr != nil {
				continue // Skip invalid keys
			}
			f.cache[jwk.Kid] = key
		}
	}

	return nil
}

// jwkToRSAPublicKey converts a JWK to an RSA public key.
func (f *JWKSFetcher) jwkToRSAPublicKey(jwk JWK) (*rsa.PublicKey, error) {
	// Check required fields
	if jwk.N == "" {
		return nil, errors.New("missing required field 'n' (modulus) in JWK")
	}
	if jwk.E == "" {
		return nil, errors.New("missing required field 'e' (exponent) in JWK")
	}

	// Decode base64url encoded n and e
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode n: %w", err)
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode e: %w", err)
	}

	// Convert to big.Int
	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	// Create RSA public key
	publicKey := &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}

	return publicKey, nil
}
