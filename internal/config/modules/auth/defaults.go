package auth

import (
	"time"
)

// Default authentication configuration values.
const (
	DefaultAlgorithm       = "RS256"
	DefaultUserGroupsClaim = "groups"
	DefaultTokenTTL        = 1 * time.Hour
	DefaultJwksTTL         = 15 * time.Minute
	DefaultCacheTTL        = 5 * time.Minute
	DefaultMaxRetries      = 3
	DefaultRetryDelay      = time.Second
	DefaultRetryBackoffMs  = 100
)

// GetDefaults returns default authentication configuration.
func GetDefaults() Config {
	return Config{
		JWT: JWTConfig{
			Algorithm: DefaultAlgorithm,
			Validation: ValidationConfig{
				Audience: false,
				Issuer:   false,
			},
			Claims: ClaimsConfig{
				UserGroups: DefaultUserGroupsClaim,
			},
			Cache: CacheConfig{
				TokenTTL: DefaultTokenTTL,
				JwksTTL:  DefaultJwksTTL,
			},
		},
	}
}
