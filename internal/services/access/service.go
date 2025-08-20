package access

import (
	"context"
	"net/http"
	"strings"

	"github.com/finlego/vm-proxy-auth/internal/domain"
)

// Service implements domain.AccessControlService
type Service struct {
	logger domain.Logger
}

// NewService creates a new access control service
func NewService(logger domain.Logger) *Service {
	return &Service{
		logger: logger,
	}
}

// CanAccess checks if a user can access a specific path with given method
func (s *Service) CanAccess(ctx context.Context, user *domain.User, path string, method string) error {
	// Check if user is read-only and this is a write operation
	if user.ReadOnly && isWriteOperation(method, path) {
		return &domain.AppError{
			Code:       "read_only_access",
			Message:    "User has read-only access",
			HTTPStatus: http.StatusForbidden,
		}
	}

	// Check if path is restricted
	if isRestrictedPath(path) {
		return &domain.AppError{
			Code:       "restricted_path",
			Message:    "Access to this path is restricted",
			HTTPStatus: http.StatusForbidden,
		}
	}

	// All other checks pass
	return nil
}

// isWriteOperation determines if the request is a write operation
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

// isRestrictedPath checks if a path is restricted
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