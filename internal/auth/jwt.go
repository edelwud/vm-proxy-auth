package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
)

type JWTVerifier struct {
	config    *JWTConfig
	logger    *logrus.Logger
	keyCache  map[string]interface{}
	cacheMu   sync.RWMutex
	lastFetch time.Time
}

type JWTConfig struct {
	JWKSURL          string
	Secret           string
	Algorithm        string
	ValidateAudience bool
	ValidateIssuer   bool
	RequiredIssuer   string
	RequiredAudience []string
	CacheTTL         time.Duration
}

type UserClaims struct {
	jwt.RegisteredClaims
	Groups            []string               `json:"groups,omitempty"`
	RealmAccess       map[string]interface{} `json:"realm_access,omitempty"`
	ResourceAccess    map[string]interface{} `json:"resource_access,omitempty"`
	PreferredUsername string                 `json:"preferred_username,omitempty"`
	Email             string                 `json:"email,omitempty"`
	Name              string                 `json:"name,omitempty"`
}

type JWKSResponse struct {
	Keys []JWK `json:"keys"`
}

type JWK struct {
	Kid string   `json:"kid"`
	Kty string   `json:"kty"`
	Use string   `json:"use"`
	N   string   `json:"n"`
	E   string   `json:"e"`
	X5C []string `json:"x5c"`
}

func NewJWTVerifier(config *JWTConfig, logger *logrus.Logger) *JWTVerifier {
	return &JWTVerifier{
		config:   config,
		logger:   logger,
		keyCache: make(map[string]interface{}),
	}
}

func (v *JWTVerifier) VerifyToken(tokenString string) (*UserClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &UserClaims{}, v.keyFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JWT: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid JWT token")
	}

	claims, ok := token.Claims.(*UserClaims)
	if !ok {
		return nil, fmt.Errorf("invalid JWT claims")
	}

	if err := v.validateClaims(claims); err != nil {
		return nil, fmt.Errorf("claims validation failed: %w", err)
	}

	return claims, nil
}

func (v *JWTVerifier) keyFunc(token *jwt.Token) (interface{}, error) {
	if v.config.Secret != "" {
		return []byte(v.config.Secret), nil
	}

	if v.config.JWKSURL == "" {
		return nil, fmt.Errorf("no JWT verification method configured")
	}

	kid, ok := token.Header["kid"].(string)
	if !ok {
		return nil, fmt.Errorf("no kid header found in token")
	}

	key, err := v.getPublicKey(kid)
	if err != nil {
		return nil, fmt.Errorf("failed to get public key: %w", err)
	}

	return key, nil
}

func (v *JWTVerifier) getPublicKey(kid string) (interface{}, error) {
	v.cacheMu.RLock()
	if key, exists := v.keyCache[kid]; exists && time.Since(v.lastFetch) < v.config.CacheTTL {
		v.cacheMu.RUnlock()
		return key, nil
	}
	v.cacheMu.RUnlock()

	v.cacheMu.Lock()
	defer v.cacheMu.Unlock()

	if key, exists := v.keyCache[kid]; exists && time.Since(v.lastFetch) < v.config.CacheTTL {
		return key, nil
	}

	if err := v.fetchKeys(); err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	key, exists := v.keyCache[kid]
	if !exists {
		return nil, fmt.Errorf("key with kid '%s' not found", kid)
	}

	return key, nil
}

func (v *JWTVerifier) fetchKeys() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", v.config.JWKSURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	var jwksResponse JWKSResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwksResponse); err != nil {
		return fmt.Errorf("failed to decode JWKS response: %w", err)
	}

	newKeyCache := make(map[string]interface{})

	for _, key := range jwksResponse.Keys {
		if key.Kty == "RSA" && (key.Use == "sig" || key.Use == "") {
			publicKey, err := v.parseRSAKey(key)
			if err != nil {
				v.logger.WithError(err).WithField("kid", key.Kid).Warn("Failed to parse RSA key")
				continue
			}
			newKeyCache[key.Kid] = publicKey
		}
	}

	v.keyCache = newKeyCache
	v.lastFetch = time.Now()

	v.logger.WithField("keys_count", len(newKeyCache)).Debug("Updated JWKS key cache")

	return nil
}

func (v *JWTVerifier) parseRSAKey(key JWK) (interface{}, error) {
	return jwt.ParseRSAPublicKeyFromPEM([]byte(fmt.Sprintf(
		"-----BEGIN CERTIFICATE-----\n%s\n-----END CERTIFICATE-----",
		key.X5C[0],
	)))
}

func (v *JWTVerifier) validateClaims(claims *UserClaims) error {
	if v.config.ValidateIssuer && claims.Issuer != v.config.RequiredIssuer {
		return fmt.Errorf("invalid issuer: expected %s, got %s", v.config.RequiredIssuer, claims.Issuer)
	}

	if v.config.ValidateAudience && len(v.config.RequiredAudience) > 0 {
		found := false
		for _, aud := range v.config.RequiredAudience {
			for _, claimAud := range claims.Audience {
				if aud == claimAud {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return fmt.Errorf("invalid audience: required one of %v, got %v", v.config.RequiredAudience, claims.Audience)
		}
	}

	if time.Now().After(claims.ExpiresAt.Time) {
		return fmt.Errorf("token expired")
	}

	// if time.Now().Before(claims.NotBefore.Time) {
	// 	return fmt.Errorf("token not yet valid")
	// }

	return nil
}

func ExtractTokenFromRequest(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("no authorization header found")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return "", fmt.Errorf("invalid authorization header format")
	}

	return parts[1], nil
}
