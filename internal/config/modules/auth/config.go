package auth

import (
	"errors"
	"fmt"
	"time"
)

// Config represents authentication configuration.
type Config struct {
	JWT JWTConfig `mapstructure:"jwt"`
}

// JWTConfig represents JWT authentication configuration.
type JWTConfig struct {
	Algorithm  string           `mapstructure:"algorithm"`
	JwksURL    string           `mapstructure:"jwksUrl"`
	Secret     string           `mapstructure:"secret"`
	Validation ValidationConfig `mapstructure:"validation"`
	Claims     ClaimsConfig     `mapstructure:"claims"`
	Cache      CacheConfig      `mapstructure:"cache"`
}

// ValidationConfig represents JWT validation settings.
type ValidationConfig struct {
	Audience         bool     `mapstructure:"audience"`
	Issuer           bool     `mapstructure:"issuer"`
	RequiredIssuer   string   `mapstructure:"requiredIssuer"`
	RequiredAudience []string `mapstructure:"requiredAudience"`
}

// ClaimsConfig represents JWT claims configuration.
type ClaimsConfig struct {
	UserGroups string `mapstructure:"userGroups"`
}

// CacheConfig represents JWT caching configuration.
type CacheConfig struct {
	TokenTTL time.Duration `mapstructure:"tokenTTL"`
	JwksTTL  time.Duration `mapstructure:"jwksTTL"`
}

// Validate validates authentication configuration.
func (c *Config) Validate() error {
	return c.JWT.Validate()
}

// Validate validates JWT configuration.
func (j *JWTConfig) Validate() error {
	// Either JWKS URL or secret must be provided
	if j.JwksURL == "" && j.Secret == "" {
		return errors.New("either jwksUrl or secret is required for JWT authentication")
	}

	// Validate algorithm
	switch j.Algorithm {
	case "RS256", "HS256":
		// Valid algorithms
	default:
		return fmt.Errorf("unsupported JWT algorithm: %s (supported: RS256, HS256)", j.Algorithm)
	}

	// RS256 requires JWKS URL
	if j.Algorithm == "RS256" && j.JwksURL == "" {
		return errors.New("RS256 algorithm requires jwksUrl")
	}

	// HS256 requires secret
	if j.Algorithm == "HS256" && j.Secret == "" {
		return errors.New("HS256 algorithm requires secret")
	}

	// Validate required issuer if validation is enabled
	if j.Validation.Issuer && j.Validation.RequiredIssuer == "" {
		return errors.New("requiredIssuer must be specified when issuer validation is enabled")
	}

	// Validate required audience if validation is enabled
	if j.Validation.Audience && len(j.Validation.RequiredAudience) == 0 {
		return errors.New("requiredAudience must be specified when audience validation is enabled")
	}

	// Validate cache TTLs
	if j.Cache.TokenTTL <= 0 {
		return fmt.Errorf("token TTL must be positive, got %v", j.Cache.TokenTTL)
	}

	if j.Cache.JwksTTL <= 0 {
		return fmt.Errorf("JWKS TTL must be positive, got %v", j.Cache.JwksTTL)
	}

	return nil
}
