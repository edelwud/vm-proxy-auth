package domain

import (
	"fmt"
	"net/http"
)

// Error codes
const (
	ErrCodeUnauthorized     = "UNAUTHORIZED"
	ErrCodeForbidden        = "FORBIDDEN"
	ErrCodeBadRequest       = "BAD_REQUEST"
	ErrCodeNotFound         = "NOT_FOUND"
	ErrCodeMethodNotAllowed = "METHOD_NOT_ALLOWED"
	ErrCodeUpstreamError    = "UPSTREAM_ERROR"
	ErrCodeInternalError    = "INTERNAL_ERROR"
	ErrCodeRateLimited      = "RATE_LIMITED"
)

// AppError represents application-specific errors
type AppError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"status"`
	Cause      error  `json:"-"`
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

// Predefined errors
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
)

// NewAppError creates a new application error
func NewAppError(code, message string, httpStatus int, cause error) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
		Cause:      cause,
	}
}

// NewUnauthorizedError creates an unauthorized error
func NewUnauthorizedError(message string, cause error) *AppError {
	return NewAppError(ErrCodeUnauthorized, message, http.StatusUnauthorized, cause)
}

// NewForbiddenError creates a forbidden error
func NewForbiddenError(message string, cause error) *AppError {
	return NewAppError(ErrCodeForbidden, message, http.StatusForbidden, cause)
}

// NewBadRequestError creates a bad request error
func NewBadRequestError(message string, cause error) *AppError {
	return NewAppError(ErrCodeBadRequest, message, http.StatusBadRequest, cause)
}

// NewUpstreamError creates an upstream error
func NewUpstreamError(message string, cause error) *AppError {
	return NewAppError(ErrCodeUpstreamError, message, http.StatusBadGateway, cause)
}

// NewInternalError creates an internal error
func NewInternalError(message string, cause error) *AppError {
	return NewAppError(ErrCodeInternalError, message, http.StatusInternalServerError, cause)
}