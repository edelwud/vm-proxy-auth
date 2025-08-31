package domain

import (
	"fmt"
	"net/http"
)

// Error codes.
const (
	ErrCodeUnauthorized     = "UNAUTHORIZED"
	ErrCodeForbidden        = "FORBIDDEN"
	ErrCodeBadRequest       = "BAD_REQUEST"
	ErrCodeNotFound         = "NOT_FOUND"
	ErrCodeMethodNotAllowed = "METHOD_NOT_ALLOWED"
	ErrCodeUpstreamError    = "UPSTREAM_ERROR"
	ErrCodeInternalError    = "INTERNAL_ERROR"
	ErrCodeRateLimited      = "RATE_LIMITED"
	ErrCodeValidation       = "VALIDATION_ERROR"
	ErrCodeTenantError      = "TENANT_ERROR"
	ErrCodePromQLError      = "PROMQL_ERROR"
)

// AppError represents application-specific errors.
type AppError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"status"`
	Cause      error  `json:"-"`
}

// NewAppError creates a new application error.
func NewAppError(code, message string, httpStatus int, cause error) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
		Cause:      cause,
	}
}

// NewUnauthorizedError creates an unauthorized error.
func NewUnauthorizedError(message string, cause error) *AppError {
	return NewAppError(ErrCodeUnauthorized, message, http.StatusUnauthorized, cause)
}

// NewForbiddenError creates a forbidden error.
func NewForbiddenError(message string, cause error) *AppError {
	return NewAppError(ErrCodeForbidden, message, http.StatusForbidden, cause)
}

// NewBadRequestError creates a bad request error.
func NewBadRequestError(message string, cause error) *AppError {
	return NewAppError(ErrCodeBadRequest, message, http.StatusBadRequest, cause)
}

// NewUpstreamError creates an upstream error.
func NewUpstreamError(message string, cause error) *AppError {
	return NewAppError(ErrCodeUpstreamError, message, http.StatusBadGateway, cause)
}

// NewInternalError creates an internal error.
func NewInternalError(message string, cause error) *AppError {
	return NewAppError(ErrCodeInternalError, message, http.StatusInternalServerError, cause)
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (caused by: %v)", e.Code, e.Message, e.Cause)
	}

	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Cause
}

// Predefined errors.
var (
	ErrUnauthorized = &AppError{
		Code:       ErrCodeUnauthorized,
		Message:    "Authentication required",
		HTTPStatus: http.StatusUnauthorized,
	}

	ErrTokenExpired = &AppError{
		Code:       ErrCodeUnauthorized,
		Message:    "Token has expired",
		HTTPStatus: http.StatusUnauthorized,
	}

	ErrInvalidToken = &AppError{
		Code:       ErrCodeUnauthorized,
		Message:    "Invalid token",
		HTTPStatus: http.StatusUnauthorized,
	}

	ErrForbidden = &AppError{
		Code:       ErrCodeForbidden,
		Message:    "Access denied",
		HTTPStatus: http.StatusForbidden,
	}

	ErrReadOnlyAccess = &AppError{
		Code:       ErrCodeForbidden,
		Message:    "Read-only user cannot perform write operations",
		HTTPStatus: http.StatusForbidden,
	}

	ErrAdminRequired = &AppError{
		Code:       ErrCodeForbidden,
		Message:    "Admin access required",
		HTTPStatus: http.StatusForbidden,
	}

	ErrTenantRequired = &AppError{
		Code:       ErrCodeBadRequest,
		Message:    "Target tenant must be specified for write operations",
		HTTPStatus: http.StatusBadRequest,
	}

	ErrMethodNotAllowed = &AppError{
		Code:       ErrCodeMethodNotAllowed,
		Message:    "Method not allowed for this endpoint",
		HTTPStatus: http.StatusMethodNotAllowed,
	}

	ErrUpstreamUnavailable = &AppError{
		Code:       ErrCodeUpstreamError,
		Message:    "Upstream service unavailable",
		HTTPStatus: http.StatusBadGateway,
	}

	// Config errors.
	ErrUpstreamURLRequired = &AppError{
		Code:       ErrCodeUpstreamError,
		Message:    "upstream.url is required",
		HTTPStatus: http.StatusBadGateway,
	}
	ErrAuthConfigRequired = &AppError{
		Code:       ErrCodeUpstreamError,
		Message:    "either auth.jwks_url or auth.jwt_secret must be provided for JWT authentication",
		HTTPStatus: http.StatusBadGateway,
	}

	// Tenant errors.
	ErrNoVMTenants = &AppError{
		Code:       ErrCodeTenantError,
		Message:    "User has no VM tenants configured",
		HTTPStatus: http.StatusForbidden,
	}

	ErrTenantAccessDenied = &AppError{
		Code:       ErrCodeForbidden,
		Message:    "Access to tenant denied",
		HTTPStatus: http.StatusForbidden,
	}

	// PromQL errors.
	ErrPromQLParsing = &AppError{
		Code:       ErrCodePromQLError,
		Message:    "Failed to parse PromQL query for VM tenant filtering",
		HTTPStatus: http.StatusBadRequest,
	}

	// Validation errors.
	ErrInvalidTokenClaims = &AppError{
		Code:       ErrCodeUnauthorized,
		Message:    "Invalid token claims",
		HTTPStatus: http.StatusUnauthorized,
	}

	ErrTokenExpiredClaims = &AppError{
		Code:       ErrCodeUnauthorized,
		Message:    "Token has expired",
		HTTPStatus: http.StatusUnauthorized,
	}

	ErrTokenNotValid = &AppError{
		Code:       ErrCodeUnauthorized,
		Message:    "Token used before valid",
		HTTPStatus: http.StatusUnauthorized,
	}

	// JWT/JWKS errors.
	ErrJWKSKeyNotFound = &AppError{
		Code:       ErrCodeUnauthorized,
		Message:    "Key not found in JWKS",
		HTTPStatus: http.StatusUnauthorized,
	}

	ErrJWKSEndpointError = &AppError{
		Code:       ErrCodeInternalError,
		Message:    "JWKS endpoint error",
		HTTPStatus: http.StatusInternalServerError,
	}

	ErrUnexpectedSigningMethod = &AppError{
		Code:       ErrCodeUnauthorized,
		Message:    "Unexpected JWT signing method",
		HTTPStatus: http.StatusUnauthorized,
	}

	ErrNoPublicKeyConfigured = &AppError{
		Code:       ErrCodeInternalError,
		Message:    "No public key or JWKS URL configured for RS256",
		HTTPStatus: http.StatusInternalServerError,
	}

	ErrTokenMissingKid = &AppError{
		Code:       ErrCodeUnauthorized,
		Message:    "Token header missing kid claim",
		HTTPStatus: http.StatusUnauthorized,
	}

	ErrUnsupportedSigningMethod = &AppError{
		Code:       ErrCodeUnauthorized,
		Message:    "Unsupported signing method",
		HTTPStatus: http.StatusUnauthorized,
	}

	ErrNoVMTenantsForFiltering = &AppError{
		Code:       ErrCodeTenantError,
		Message:    "No VM tenants available for filtering",
		HTTPStatus: http.StatusBadRequest,
	}

	// Load balancer errors.
	ErrNoHealthyBackends = &AppError{
		Code:       ErrCodeUpstreamError,
		Message:    "No healthy backends available",
		HTTPStatus: http.StatusServiceUnavailable,
	}

	ErrNoAvailableBackends = &AppError{
		Code:       ErrCodeUpstreamError,
		Message:    "No backends available (including fallbacks)",
		HTTPStatus: http.StatusServiceUnavailable,
	}

	ErrAllBackendsCircuitOpen = &AppError{
		Code:       ErrCodeUpstreamError,
		Message:    "All backend circuits are open",
		HTTPStatus: http.StatusServiceUnavailable,
	}

	ErrBackendNotFound = &AppError{
		Code:       ErrCodeUpstreamError,
		Message:    "Backend not found",
		HTTPStatus: http.StatusNotFound,
	}

	// State storage errors.
	ErrKeyNotFound = &AppError{
		Code:       ErrCodeNotFound,
		Message:    "Key not found in storage",
		HTTPStatus: http.StatusNotFound,
	}

	ErrStorageUnavailable = &AppError{
		Code:       ErrCodeInternalError,
		Message:    "State storage is unavailable",
		HTTPStatus: http.StatusServiceUnavailable,
	}

	ErrStorageTimeout = &AppError{
		Code:       ErrCodeInternalError,
		Message:    "State storage operation timeout",
		HTTPStatus: http.StatusRequestTimeout,
	}

	ErrNotLeader = &AppError{
		Code:       ErrCodeInternalError,
		Message:    "Not the leader node for this operation",
		HTTPStatus: http.StatusServiceUnavailable,
	}

	// Request queue errors.
	ErrQueueFull = &AppError{
		Code:       ErrCodeRateLimited,
		Message:    "Request queue is full",
		HTTPStatus: http.StatusTooManyRequests,
	}

	ErrQueueEmpty = &AppError{
		Code:       ErrCodeInternalError,
		Message:    "Request queue is empty",
		HTTPStatus: http.StatusNoContent,
	}

	ErrQueueTimeout = &AppError{
		Code:       ErrCodeInternalError,
		Message:    "Request timeout in queue",
		HTTPStatus: http.StatusRequestTimeout,
	}

	ErrQueueClosed = &AppError{
		Code:       ErrCodeInternalError,
		Message:    "Request queue is closed",
		HTTPStatus: http.StatusServiceUnavailable,
	}

	// Circuit breaker errors.
	ErrCircuitOpen = &AppError{
		Code:       ErrCodeUpstreamError,
		Message:    "Circuit breaker is open",
		HTTPStatus: http.StatusServiceUnavailable,
	}

	// Distributed lock errors.
	ErrLockAlreadyHeld = &AppError{
		Code:       ErrCodeRateLimited,
		Message:    "Lock is already held by another node",
		HTTPStatus: http.StatusConflict,
	}

	ErrNotLockOwner = &AppError{
		Code:       ErrCodeForbidden,
		Message:    "Not the owner of the lock",
		HTTPStatus: http.StatusForbidden,
	}
)
