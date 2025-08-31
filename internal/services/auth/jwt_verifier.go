package auth

import (
	"crypto/rsa"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// JWTVerifier handles JWT token verification.
type JWTVerifier struct {
	publicKey   *rsa.PublicKey
	secret      []byte
	algorithm   string
	jwksFetcher *JWKSFetcher
}

// NewJWTVerifier creates a new JWT verifier.
func NewJWTVerifier(publicKey *rsa.PublicKey, secret []byte, algorithm string) *JWTVerifier {
	return &JWTVerifier{
		publicKey: publicKey,
		secret:    secret,
		algorithm: algorithm,
	}
}

// NewJWKSVerifier creates a new JWT verifier using JWKS.
func NewJWKSVerifier(jwksURL, algorithm string, cacheTTL time.Duration) *JWTVerifier {
	return &JWTVerifier{
		algorithm:   algorithm,
		jwksFetcher: NewJWKSFetcher(jwksURL, cacheTTL),
	}
}

// Claims represents JWT claims structure.
type Claims struct {
	jwt.RegisteredClaims

	Email     string   `json:"email"`
	Groups    []string `json:"groups"`
	UserID    string   `json:"sub"`
	ExpiresAt int64    `json:"exp"`
	IssuedAt  int64    `json:"iat"`
	NotBefore int64    `json:"nbf"`
	Issuer    string   `json:"iss"`
	Audience  string   `json:"aud"`
}

// VerifyToken verifies a JWT token and returns the claims.
func (v *JWTVerifier) VerifyToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, v.keyFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, domain.ErrInvalidToken
	}

	if validateErr := v.validateClaims(claims); validateErr != nil {
		return nil, validateErr
	}

	return claims, nil
}

// keyFunc returns the key for verifying the JWT token signature.
func (v *JWTVerifier) keyFunc(token *jwt.Token) (any, error) {
	switch v.algorithm {
	case "RS256":
		return v.handleRSAKey(token)
	case "HS256":
		return v.handleHMACKey(token)
	default:
		return nil, fmt.Errorf("%w: %s", domain.ErrUnsupportedSigningMethod, v.algorithm)
	}
}

// handleRSAKey handles RSA key verification for RS256 algorithm.
func (v *JWTVerifier) handleRSAKey(token *jwt.Token) (any, error) {
	if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
		return nil, fmt.Errorf("%w: %v", domain.ErrUnexpectedSigningMethod, token.Header["alg"])
	}

	// If we have a static public key, use it
	if v.publicKey != nil {
		return v.publicKey, nil
	}

	// Otherwise, use JWKS to get the key
	return v.getKeyFromJWKS(token)
}

// handleHMACKey handles HMAC key verification for HS256 algorithm.
func (v *JWTVerifier) handleHMACKey(token *jwt.Token) (any, error) {
	if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
		return nil, fmt.Errorf("%w: %v", domain.ErrUnexpectedSigningMethod, token.Header["alg"])
	}
	return v.secret, nil
}

// getKeyFromJWKS fetches the public key from JWKS endpoint.
func (v *JWTVerifier) getKeyFromJWKS(token *jwt.Token) (any, error) {
	if v.jwksFetcher == nil {
		return nil, domain.ErrNoPublicKeyConfigured
	}

	// Get kid from token header
	kid, ok := token.Header["kid"].(string)
	if !ok {
		return nil, domain.ErrTokenMissingKid
	}

	// Fetch public key from JWKS
	publicKey, err := v.jwksFetcher.GetPublicKey(kid)
	if err != nil {
		return nil, fmt.Errorf("failed to get public key from JWKS: %w", err)
	}

	return publicKey, nil
}

// validateClaims validates the time-based claims in the JWT token.
func (v *JWTVerifier) validateClaims(claims *Claims) error {
	now := time.Now().Unix()

	// Check required claims
	if claims.ExpiresAt == 0 {
		return domain.ErrInvalidTokenClaims
	}

	if claims.IssuedAt == 0 {
		return domain.ErrInvalidTokenClaims
	}

	// Check expiration
	if claims.ExpiresAt < now {
		return domain.ErrTokenExpiredClaims
	}

	// Check not before
	if claims.NotBefore > 0 && claims.NotBefore > now {
		return domain.ErrTokenNotValid
	}

	// Check if token is issued in the future
	if claims.IssuedAt > now {
		return domain.ErrTokenNotValid
	}

	return nil
}
