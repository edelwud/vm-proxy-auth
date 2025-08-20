package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/finlego/prometheus-oauth-gateway/internal/auth"
	"github.com/finlego/prometheus-oauth-gateway/pkg/tenant"
	"github.com/sirupsen/logrus"
)

type AuthMiddleware struct {
	jwtVerifier     *auth.JWTVerifier
	tenantMapper    *tenant.Mapper
	userGroupsClaim string
	logger          *logrus.Logger
}

type UserContext struct {
	UserID         string
	Groups         []string
	AllowedTenants []string
	ReadOnly       bool
	Claims         *auth.UserClaims
}

type contextKey string

const UserContextKey contextKey = "user"

func NewAuthMiddleware(
	jwtVerifier *auth.JWTVerifier,
	tenantMapper *tenant.Mapper,
	userGroupsClaim string,
	logger *logrus.Logger,
) *AuthMiddleware {
	return &AuthMiddleware{
		jwtVerifier:     jwtVerifier,
		tenantMapper:    tenantMapper,
		userGroupsClaim: userGroupsClaim,
		logger:          logger,
	}
}

func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenString, err := auth.ExtractTokenFromRequest(r)
		if err != nil {
			m.logger.WithError(err).Warn("Failed to extract token from request")
			writeErrorResponse(w, http.StatusUnauthorized, "Missing or invalid authorization header")
			return
		}

		claims, err := m.jwtVerifier.VerifyToken(tokenString)
		if err != nil {
			m.logger.WithError(err).Warn("Token verification failed")
			writeErrorResponse(w, http.StatusUnauthorized, "Invalid or expired token")
			return
		}

		userGroups := extractGroups(claims, m.userGroupsClaim)

		access, err := m.tenantMapper.GetUserAccess(userGroups)
		if err != nil {
			m.logger.WithError(err).WithField("user", claims.Subject).Error("Failed to get user access")
			writeErrorResponse(w, http.StatusInternalServerError, "Failed to determine user access")
			return
		}

		userCtx := &UserContext{
			UserID:         getUserID(claims),
			Groups:         userGroups,
			AllowedTenants: access.Tenants,
			ReadOnly:       access.ReadOnly,
			Claims:         claims,
		}

		m.logger.WithFields(logrus.Fields{
			"user_id":         userCtx.UserID,
			"groups":          userCtx.Groups,
			"allowed_tenants": userCtx.AllowedTenants,
			"read_only":       userCtx.ReadOnly,
		}).Debug("User authenticated successfully")

		ctx := context.WithValue(r.Context(), UserContextKey, userCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func extractGroups(claims *auth.UserClaims, groupsClaim string) []string {
	if claims.Groups != nil && len(claims.Groups) > 0 {
		return claims.Groups
	}

	if groupsClaim == "groups" || groupsClaim == "" {
		return claims.Groups
	}

	if claims.RealmAccess != nil {
		if roles, ok := claims.RealmAccess["roles"].([]interface{}); ok {
			var groups []string
			for _, role := range roles {
				if roleStr, ok := role.(string); ok {
					groups = append(groups, roleStr)
				}
			}
			return groups
		}
	}

	var customGroups []string
	if groupsInterface := getClaimValue(claims, groupsClaim); groupsInterface != nil {
		switch v := groupsInterface.(type) {
		case []interface{}:
			for _, group := range v {
				if groupStr, ok := group.(string); ok {
					customGroups = append(customGroups, groupStr)
				}
			}
		case []string:
			customGroups = v
		case string:
			customGroups = strings.Split(v, ",")
		}
	}

	return customGroups
}

func getClaimValue(claims *auth.UserClaims, claimPath string) interface{} {
	claimsBytes, err := json.Marshal(claims)
	if err != nil {
		return nil
	}

	var claimsMap map[string]interface{}
	if err := json.Unmarshal(claimsBytes, &claimsMap); err != nil {
		return nil
	}

	parts := strings.Split(claimPath, ".")
	current := claimsMap

	for i, part := range parts {
		if i == len(parts)-1 {
			return current[part]
		}

		if next, ok := current[part].(map[string]interface{}); ok {
			current = next
		} else {
			return nil
		}
	}

	return nil
}

func getUserID(claims *auth.UserClaims) string {
	if claims.PreferredUsername != "" {
		return claims.PreferredUsername
	}

	if claims.Email != "" {
		return claims.Email
	}

	if claims.Subject != "" {
		return claims.Subject
	}

	return "unknown"
}

func GetUserContext(r *http.Request) *UserContext {
	if userCtx, ok := r.Context().Value(UserContextKey).(*UserContext); ok {
		return userCtx
	}
	return nil
}

func writeErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	errorResp := map[string]interface{}{
		"status": "error",
		"error":  message,
		"code":   statusCode,
	}

	json.NewEncoder(w).Encode(errorResp)
}
