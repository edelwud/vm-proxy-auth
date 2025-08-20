package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// Service implements domain.AuthService using clean architecture.
type Service struct {
	config     config.AuthConfig
	tenantMaps []config.TenantMapping
	verifier   *JWTVerifier
	logger     domain.Logger
	metrics    domain.MetricsService
	userCache  sync.Map
	cacheTTL   time.Duration
}

type cachedUser struct {
	user      *domain.User
	expiresAt time.Time
}

// NewService creates a new auth service.
func NewService(
	cfg config.AuthConfig,
	tenantMaps []config.TenantMapping,
	logger domain.Logger,
	metrics domain.MetricsService,
) domain.AuthService {
	// Initialize JWT verifier based on configuration
	var verifier *JWTVerifier

	if cfg.JWTSecret != "" {
		// Use secret-based verification (typically HS256)
		verifier = NewJWTVerifier(nil, []byte(cfg.JWTSecret), cfg.JWTAlgorithm)
	} else if cfg.JWKSURL != "" {
		// Use JWKS-based verification (typically RS256)
		verifier = NewJWKSVerifier(cfg.JWKSURL, cfg.JWTAlgorithm, cfg.CacheTTL)
	} else {
		// Error: must have either secret or JWKS URL
		panic("JWT authentication requires either jwt_secret or jwks_url to be configured")
	}

	return &Service{
		config:     cfg,
		tenantMaps: tenantMaps,
		verifier:   verifier,
		logger:     logger,
		metrics:    metrics,
		cacheTTL:   cfg.CacheTTL,
	}
}

// Authenticate validates a token and returns user information.
func (s *Service) Authenticate(ctx context.Context, token string) (*domain.User, error) {
	if token == "" {
		return nil, &domain.AppError{
			Code:       "missing_token",
			Message:    "Authorization token is required",
			HTTPStatus: http.StatusUnauthorized,
		}
	}

	// Remove "Bearer " prefix if present
	token = strings.TrimPrefix(token, "Bearer ")

	// Check cache first
	if cached, ok := s.userCache.Load(token); ok {
		cachedUser, ok := cached.(cachedUser)
		if !ok {
			s.userCache.Delete(token) // Remove invalid cache entry
		} else if time.Now().Before(cachedUser.expiresAt) {
			return cachedUser.user, nil
		} else {
			// Remove expired entry
			s.userCache.Delete(token)
		}
	}

	// Verify token
	claims, err := s.verifier.VerifyToken(token)
	if err != nil {
		// Record failed authentication attempt
		s.metrics.RecordAuthAttempt(ctx, "unknown", "failed")

		s.logger.Warn("Token verification failed", domain.Field{Key: "error", Value: err.Error()})

		return nil, &domain.AppError{
			Code:       "invalid_token",
			Message:    "Invalid or expired token",
			HTTPStatus: http.StatusUnauthorized,
		}
	}

	// Extract user groups from claims
	groups := s.extractGroups(claims)

	// Determine user permissions based on tenant mappings
	allowedTenants, vmTenants, readOnly := s.determineUserPermissions(groups)

	user := &domain.User{
		ID:             claims.UserID,
		Email:          claims.Email,
		Groups:         groups,
		AllowedTenants: allowedTenants,
		VMTenants:      vmTenants,
		ReadOnly:       readOnly,
		ExpiresAt:      time.Unix(claims.ExpiresAt, 0),
	}

	// Cache the user
	s.userCache.Store(token, cachedUser{
		user:      user,
		expiresAt: time.Now().Add(s.cacheTTL),
	})

	// Record successful authentication attempt
	s.metrics.RecordAuthAttempt(ctx, user.ID, "success")

	s.logger.Info("User authenticated successfully",
		domain.Field{Key: "user_id", Value: user.ID},
		domain.Field{Key: "groups", Value: fmt.Sprintf("%v", user.Groups)},
		domain.Field{Key: "tenants", Value: fmt.Sprintf("%v", user.AllowedTenants)},
		domain.Field{Key: "read_only", Value: user.ReadOnly},
	)

	return user, nil
}

// extractGroups extracts group information from JWT claims.
func (s *Service) extractGroups(claims *Claims) []string {
	if len(claims.Groups) > 0 {
		return claims.Groups
	}
	// Default group if none specified
	return []string{"default"}
}

// determineUserPermissions maps user groups to tenant permissions.
func (s *Service) determineUserPermissions(
	userGroups []string,
) ([]string, []domain.VMTenant, bool) {
	var allowedTenants []string
	var vmTenants []domain.VMTenant
	readOnly := true // Default to read-only

	for _, mapping := range s.tenantMaps {
		if s.hasGroupMatch(userGroups, mapping.Groups) {
			// Add legacy tenants from this mapping
			allowedTenants = append(allowedTenants, mapping.Tenants...)

			// Add VictoriaMetrics tenants if specified
			for _, vmMapping := range mapping.VMTenants {
				vmTenants = append(vmTenants, domain.VMTenant{
					AccountID: vmMapping.AccountID,
					ProjectID: vmMapping.ProjectID,
				})
			}

			// If any mapping allows write access, user gets write access
			if !mapping.ReadOnly {
				readOnly = false
			}
		}
	}

	// If no specific mappings found, give default access
	if len(allowedTenants) == 0 && len(vmTenants) == 0 {
		allowedTenants = []string{"default"}
		vmTenants = []domain.VMTenant{{AccountID: "0"}} // Default VM tenant
	}

	return s.removeDuplicates(allowedTenants), s.removeDuplicateVMTenants(vmTenants), readOnly
}

// hasGroupMatch checks if user has any of the required groups.
func (s *Service) hasGroupMatch(userGroups, requiredGroups []string) bool {
	for _, userGroup := range userGroups {
		for _, requiredGroup := range requiredGroups {
			if userGroup == requiredGroup {
				return true
			}
		}
	}

	return false
}

// removeDuplicates removes duplicate tenant IDs.
func (s *Service) removeDuplicates(slice []string) []string {
	keys := make(map[string]bool)
	result := []string{}
	for _, item := range slice {
		if !keys[item] {
			keys[item] = true
			result = append(result, item)
		}
	}

	return result
}

// removeDuplicateVMTenants removes duplicate VM tenants.
func (s *Service) removeDuplicateVMTenants(tenants []domain.VMTenant) []domain.VMTenant {
	keys := make(map[string]bool)
	result := []domain.VMTenant{}
	for _, tenant := range tenants {
		key := tenant.String()
		if !keys[key] {
			keys[key] = true
			result = append(result, tenant)
		}
	}

	return result
}

// CleanupCache removes expired entries from the cache.
func (s *Service) CleanupCache() {
	now := time.Now()
	s.userCache.Range(func(key, value interface{}) bool {
		cached, ok := value.(cachedUser)
		if !ok {
			s.userCache.Delete(key) // Remove invalid cache entry

			return true
		}
		if now.After(cached.expiresAt) {
			s.userCache.Delete(key)
		}

		return true
	})
}
