package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	loggerPkg "github.com/edelwud/vm-proxy-auth/internal/infrastructure/logger"
)

// Define custom type for context keys to avoid collisions.
type contextKey string

const requestIDKey contextKey = "request_id"

// GatewayHandler handles all proxy requests using clean architecture.
type GatewayHandler struct {
	authService    domain.AuthService
	tenantService  domain.TenantService
	accessService  domain.AccessControlService
	proxyService   domain.ProxyService
	metricsService domain.MetricsService
	logger         domain.Logger
}

// NewGatewayHandler creates a new gateway handler.
func NewGatewayHandler(
	authService domain.AuthService,
	tenantService domain.TenantService,
	accessService domain.AccessControlService,
	proxyService domain.ProxyService,
	metricsService domain.MetricsService,
	logger domain.Logger,
) *GatewayHandler {
	return &GatewayHandler{
		authService:    authService,
		tenantService:  tenantService,
		accessService:  accessService,
		proxyService:   proxyService,
		metricsService: metricsService,
		logger:         logger.With(domain.Field{Key: "component", Value: "gateway"}),
	}
}

// ServeHTTP implements http.Handler interface.
func (h *GatewayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := generateRequestID()

	ctx := context.WithValue(r.Context(), requestIDKey, requestID)
	r = r.WithContext(ctx)

	reqLogger := h.logger.With(
		loggerPkg.RequestID(requestID),
		loggerPkg.Path(r.URL.Path),
		loggerPkg.Method(r.Method),
	)

	reqLogger.Info("Request started")

	// Extract token from request
	token := h.extractToken(r)
	if token == "" {
		h.writeError(w, domain.ErrUnauthorized)
		h.recordMetrics(ctx, r.Method, r.URL.Path, "401", time.Since(startTime), nil)

		return
	}

	// Authenticate user
	user, err := h.authService.Authenticate(ctx, token)
	if err != nil {
		reqLogger.Warn("Authentication failed", loggerPkg.Error(err))
		var appErr *domain.AppError
		if errors.As(err, &appErr) {
			h.writeError(w, appErr)
			h.recordMetrics(ctx, r.Method, r.URL.Path, strconv.Itoa(appErr.HTTPStatus), time.Since(startTime), nil)
		} else {
			h.writeError(w, domain.ErrUnauthorized)
			h.recordMetrics(ctx, r.Method, r.URL.Path, "401", time.Since(startTime), nil)
		}

		return
	}

	userLogger := reqLogger.With(loggerPkg.UserID(user.ID))

	// Check access permissions
	if accessErr := h.accessService.CanAccess(ctx, user, r.URL.Path, r.Method); accessErr != nil {
		userLogger.Warn("Access denied", loggerPkg.Error(accessErr))
		var appErr *domain.AppError
		if errors.As(accessErr, &appErr) {
			h.writeError(w, appErr)
			h.recordMetrics(ctx, r.Method, r.URL.Path, strconv.Itoa(appErr.HTTPStatus), time.Since(startTime), user)
		} else {
			h.writeError(w, domain.ErrForbidden)
			h.recordMetrics(ctx, r.Method, r.URL.Path, "403", time.Since(startTime), user)
		}

		return
	}

	// Process the request
	h.processRequest(w, r, user, startTime)
}

func (h *GatewayHandler) processRequest(
	w http.ResponseWriter,
	r *http.Request,
	user *domain.User,
	startTime time.Time,
) {
	ctx := r.Context()

	// Process query filtering if needed
	filteredQuery, err := h.processQueryFiltering(ctx, r, user)
	if err != nil {
		h.handleProcessingError(w, r, err, startTime, user)
		return
	}

	// Determine target tenant for write operations
	targetTenant, err := h.processTargetTenant(ctx, r, user)
	if err != nil {
		h.handleProcessingError(w, r, err, startTime, user)
		return
	}

	// Create proxy request
	proxyReq := &domain.ProxyRequest{
		User:            user,
		OriginalRequest: r,
		FilteredQuery:   filteredQuery,
		TargetTenant:    targetTenant,
	}

	// Forward to upstream
	response, err := h.proxyService.Forward(ctx, proxyReq)
	if err != nil {
		h.logger.Error("Upstream request failed", loggerPkg.Error(err))
		var appErr *domain.AppError
		if errors.As(err, &appErr) {
			h.writeError(w, appErr)
			h.recordMetrics(ctx, r.Method, r.URL.Path, strconv.Itoa(appErr.HTTPStatus), time.Since(startTime), user)
		} else {
			h.writeError(w, domain.ErrUpstreamUnavailable)
			h.recordMetrics(ctx, r.Method, r.URL.Path, "502", time.Since(startTime), user)
		}

		return
	}

	// Write response
	h.writeResponse(w, response)
	h.recordMetrics(ctx, r.Method, r.URL.Path, strconv.Itoa(response.StatusCode), time.Since(startTime), user)

	h.logger.Info("Request completed",
		loggerPkg.StatusCode(response.StatusCode),
		loggerPkg.Duration("duration", time.Since(startTime)),
	)
}

func (h *GatewayHandler) extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}

	return auth[len(prefix):]
}

func (h *GatewayHandler) extractQuery(r *http.Request) string {
	// Try URL parameters first
	if query := r.URL.Query().Get("query"); query != "" {
		return query
	}

	// For POST requests with form data, parse the body
	if r.Method == http.MethodPost &&
		strings.Contains(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		// We need to read the body, but we must restore it for the proxy
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			h.logger.Error("Failed to read request body for query extraction",
				domain.Field{Key: "error", Value: err.Error()})

			return ""
		}

		// Restore the body for later use
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		// Parse form data
		bodyValues, err := url.ParseQuery(string(bodyBytes))
		if err != nil {
			h.logger.Debug("Failed to parse form data",
				domain.Field{Key: "error", Value: err.Error()})

			return ""
		}

		return bodyValues.Get("query")
	}

	return ""
}

func (h *GatewayHandler) isQueryEndpoint(path string) bool {
	queryEndpoints := []string{
		"/api/v1/query",
		"/api/v1/query_range",
		"/api/v1/query_exemplars",
		"/api/v1/series",
	}

	for _, endpoint := range queryEndpoints {
		if strings.HasPrefix(path, endpoint) {
			return true
		}
	}

	// Multi-tenant query endpoints
	return strings.Contains(path, "/prometheus/api/v1/query")
}

func (h *GatewayHandler) isWriteEndpoint(path, method string) bool {
	if method != "POST" && method != "PUT" && method != "DELETE" {
		return false
	}

	writeEndpoints := []string{
		"/api/v1/write",
		"/api/v1/import",
		"/insert/",
	}

	for _, endpoint := range writeEndpoints {
		if strings.HasPrefix(path, endpoint) {
			return true
		}
	}

	return false
}

func (h *GatewayHandler) writeResponse(w http.ResponseWriter, response *domain.ProxyResponse) {
	// Copy headers
	for key, values := range response.Headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(response.StatusCode)
	if _, err := w.Write(response.Body); err != nil {
		h.logger.With(loggerPkg.Error(err)).Error("Failed to write response body")
	}
}

func (h *GatewayHandler) writeError(w http.ResponseWriter, err *domain.AppError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.HTTPStatus)

	response := map[string]interface{}{
		"status": "error",
		"error":  err.Message,
		"code":   err.Code,
	}

	if jsonErr := json.NewEncoder(w).Encode(response); jsonErr != nil {
		h.logger.Error("Failed to encode error response", loggerPkg.Error(jsonErr))
	}
}

func (h *GatewayHandler) recordMetrics(
	ctx context.Context,
	method, path, status string,
	duration time.Duration,
	user *domain.User,
) {
	if h.metricsService != nil {
		h.metricsService.RecordRequest(ctx, method, path, status, duration, user)
	}
}

func generateRequestID() string {
	// Simple request ID generation - in production, use UUID or similar
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

// processQueryFiltering handles query filtering for query endpoints.
func (h *GatewayHandler) processQueryFiltering(
	ctx context.Context,
	r *http.Request,
	user *domain.User,
) (string, error) {
	if !h.isQueryEndpoint(r.URL.Path) {
		return "", nil
	}

	originalQuery := h.extractQuery(r)
	if originalQuery == "" {
		h.logger.Warn("No query found in request - cannot apply tenant filtering")
		return "", nil
	}

	userLogger := h.getUserLogger(ctx, user, r)
	userLogger.Info("Calling FilterQuery function", domain.Field{Key: "original_query", Value: originalQuery})

	filteredQuery, err := h.tenantService.FilterQuery(ctx, user, originalQuery)
	if err != nil {
		userLogger.Error("Query filtering failed", loggerPkg.Error(err))
		return "", err
	}

	userLogger.Info("Query filtering completed",
		domain.Field{Key: "original_query", Value: originalQuery},
		domain.Field{Key: "filtered_query", Value: filteredQuery},
		domain.Field{Key: "filter_applied", Value: originalQuery != filteredQuery})

	return filteredQuery, nil
}

// processTargetTenant determines the target tenant for write operations.
func (h *GatewayHandler) processTargetTenant(ctx context.Context, r *http.Request, user *domain.User) (string, error) {
	if !h.isWriteEndpoint(r.URL.Path, r.Method) {
		return "", nil
	}

	targetTenant, err := h.tenantService.DetermineTargetTenant(ctx, user, r)
	if err != nil {
		h.logger.Error("Failed to determine target tenant", loggerPkg.Error(err))
		return "", err
	}

	return targetTenant, nil
}

// handleProcessingError handles errors during request processing.
func (h *GatewayHandler) handleProcessingError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
	startTime time.Time,
	user *domain.User,
) {
	ctx := r.Context()
	var appErr *domain.AppError

	if errors.As(err, &appErr) {
		h.writeError(w, appErr)
		h.recordMetrics(ctx, r.Method, r.URL.Path, strconv.Itoa(appErr.HTTPStatus), time.Since(startTime), user)
	} else {
		// Default to forbidden for unknown errors
		h.writeError(w, domain.ErrForbidden)
		h.recordMetrics(ctx, r.Method, r.URL.Path, "403", time.Since(startTime), user)
	}
}

// getUserLogger creates a logger with user context.
func (h *GatewayHandler) getUserLogger(ctx context.Context, user *domain.User, r *http.Request) domain.Logger {
	requestID, ok := ctx.Value(requestIDKey).(string)
	if !ok {
		requestID = "unknown"
	}

	return h.logger.With(
		loggerPkg.RequestID(requestID),
		loggerPkg.UserID(user.ID),
		loggerPkg.Path(r.URL.Path),
		loggerPkg.Method(r.Method),
	)
}
