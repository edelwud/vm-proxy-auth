package access

import (
	"context"
	"strings"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// Service implements domain.AccessControlService.
type Service struct {
	logger domain.Logger
}

// NewService creates a new access control service.
func NewService(logger domain.Logger) *Service {
	return &Service{
		logger: logger.With(domain.Field{Key: "component", Value: "access"}),
	}
}

// CanAccess checks if a user can access a specific path with given method.
func (s *Service) CanAccess(_ context.Context, user *domain.User, path, method string) error {
	// Handle nil user
	if user == nil {
		return domain.ErrUnauthorized
	}

	// Check if user is read-only and this is a write operation
	if user.ReadOnly && isWriteOperation(method, path) {
		return domain.ErrReadOnlyAccess
	}

	// Check if path is restricted and user is not admin
	if isRestrictedPath(path) && !isAdmin(user) {
		return domain.ErrAdminRequired
	}

	// All other checks pass
	return nil
}

// isWriteOperation determines if the request is a write operation.
func isWriteOperation(method, path string) bool {
	// POST, PUT, PATCH, DELETE are write operations
	writeHTTPMethods := []string{"POST", "PUT", "PATCH", "DELETE"}
	for _, writeMethod := range writeHTTPMethods {
		if method == writeMethod {
			return true
		}
	}

	// Some paths are write operations even with GET/POST
	writePaths := []string{
		"/api/v1/admin/",
		"/api/v1/write",
		"/write",
		"/receive",
	}

	for _, writePath := range writePaths {
		if strings.HasPrefix(path, writePath) {
			return true
		}
	}

	return false
}

// isRestrictedPath checks if a path is restricted.
func isRestrictedPath(path string) bool {
	restrictedPaths := []string{
		"/api/v1/admin/",
		"/metrics",
		"/debug/",
		"/-/",
	}

	for _, restrictedPath := range restrictedPaths {
		if strings.HasPrefix(path, restrictedPath) {
			return true
		}
	}

	return false
}

// isAdmin checks if a user has admin privileges.
func isAdmin(user *domain.User) bool {
	for _, group := range user.Groups {
		if group == "admin" {
			return true
		}
	}
	return false
}
