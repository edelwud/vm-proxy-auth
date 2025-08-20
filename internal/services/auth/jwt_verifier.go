package auth

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// JWTVerifier handles JWT token verification
type JWTVerifier struct {
	publicKey    *rsa.PublicKey
	secret       []byte
	algorithm    string
	jwksFetcher  *JWKSFetcher
}

// NewJWTVerifier creates a new JWT verifier
func NewJWTVerifier(publicKey *rsa.PublicKey, secret []byte, algorithm string) *JWTVerifier {
	return &JWTVerifier{
		publicKey: publicKey,
		secret:    secret,
		algorithm: algorithm,
	}
}

// NewJWKSVerifier creates a new JWT verifier using JWKS
func NewJWKSVerifier(jwksURL string, algorithm string, cacheTTL time.Duration) *JWTVerifier {
	return &JWTVerifier{
		algorithm:   algorithm,
		jwksFetcher: NewJWKSFetcher(jwksURL, cacheTTL),
	}
}

// Claims represents JWT claims structure
type Claims struct {
	Email        string   `json:"email"`
	Groups       []string `json:"groups"`
	UserID       string   `json:"sub"`
	ExpiresAt    int64    `json:"exp"`
	IssuedAt     int64    `json:"iat"`
	NotBefore    int64    `json:"nbf"`
	Issuer       string   `json:"iss"`
	Audience     string   `json:"aud"`
	jwt.RegisteredClaims
}

// VerifyToken verifies a JWT token and returns the claims
func (v *JWTVerifier) VerifyToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		switch v.algorithm {
		case "RS256":
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			
			// If we have a static public key, use it
			if v.publicKey != nil {
				return v.publicKey, nil
			}
			
			// Otherwise, use JWKS to get the key
			if v.jwksFetcher == nil {
				return nil, fmt.Errorf("no public key or JWKS URL configured for RS256")
			}
			
			// Get kid from token header
			kid, ok := token.Header["kid"].(string)
			if !ok {
				return nil, fmt.Errorf("token header missing kid claim")
			}
			
			// Fetch public key from JWKS
			publicKey, err := v.jwksFetcher.GetPublicKey(kid)
			if err != nil {
				return nil, fmt.Errorf("failed to get public key from JWKS: %w", err)
			}
			
			return publicKey, nil
			
		case "HS256":
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return v.secret, nil
			
		default:
			return nil, fmt.Errorf("unsupported signing method: %s", v.algorithm)
		}
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	// Check expiration
	now := time.Now().Unix()
	if claims.ExpiresAt < now {
		return nil, errors.New("token has expired")
	}

	// Check not before
	if claims.NotBefore > now {
		return nil, errors.New("token used before valid")
	}

	return claims, nil
}