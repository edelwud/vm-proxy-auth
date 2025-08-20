package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	loggerPkg "github.com/edelwud/vm-proxy-auth/internal/infrastructure/logger"
)

// GatewayHandler handles all proxy requests using clean architecture
type GatewayHandler struct {
	authService   domain.AuthService
	tenantService domain.TenantService
	accessService domain.AccessControlService
	proxyService  domain.ProxyService
	metricsService domain.MetricsService
	logger        domain.Logger
}

// NewGatewayHandler creates a new gateway handler
func NewGatewayHandler(
	authService domain.AuthService,
	tenantService domain.TenantService,
	accessService domain.AccessControlService,
	proxyService domain.ProxyService,
	metricsService domain.MetricsService,
	logger domain.Logger,
) *GatewayHandler {
	return &GatewayHandler{
		authService:   authService,
		tenantService: tenantService,
		accessService: accessService,
		proxyService:  proxyService,
		metricsService: metricsService,
		logger:        logger,
	}
}

// ServeHTTP implements http.Handler interface
func (h *GatewayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := generateRequestID()
	
	ctx := context.WithValue(r.Context(), "request_id", requestID)
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
		h.writeError(w, domain.ErrUnauthorized, )
		h.recordMetrics(r.Method, r.URL.Path, "401", time.Since(startTime), nil)
		return
	}

	// Authenticate user
	user, err := h.authService.Authenticate(ctx, token)
	if err != nil {
		reqLogger.Warn("Authentication failed", loggerPkg.Error(err))
		if appErr, ok := err.(*domain.AppError); ok {
			h.writeError(w, appErr, )
			h.recordMetrics(r.Method, r.URL.Path, fmt.Sprintf("%d", appErr.HTTPStatus), time.Since(startTime), nil)
		} else {
			h.writeError(w, domain.ErrUnauthorized, )
			h.recordMetrics(r.Method, r.URL.Path, "401", time.Since(startTime), nil)
		}
		return
	}

	userLogger := reqLogger.With(loggerPkg.UserID(user.ID))

	// Check access permissions
	if err := h.accessService.CanAccess(ctx, user, r.URL.Path, r.Method); err != nil {
		userLogger.Warn("Access denied", loggerPkg.Error(err))
		if appErr, ok := err.(*domain.AppError); ok {
			h.writeError(w, appErr, )
			h.recordMetrics(r.Method, r.URL.Path, fmt.Sprintf("%d", appErr.HTTPStatus), time.Since(startTime), user)
		} else {
			h.writeError(w, domain.ErrForbidden, )
			h.recordMetrics(r.Method, r.URL.Path, "403", time.Since(startTime), user)
		}
		return
	}

	// Process the request
	h.processRequest(w, r, user, startTime)
}

func (h *GatewayHandler) processRequest(w http.ResponseWriter, r *http.Request, user *domain.User, startTime time.Time) {
	ctx := r.Context()

	// Determine if this is a query endpoint that needs filtering
	var filteredQuery string
	requestID := ctx.Value("request_id").(string)
	userLogger := h.logger.With(
		loggerPkg.RequestID(requestID),
		loggerPkg.UserID(user.ID),
		loggerPkg.Path(r.URL.Path),
		loggerPkg.Method(r.Method),
	)
	
	userLogger.Info("Processing request",
		domain.Field{Key: "path", Value: r.URL.Path},
		domain.Field{Key: "method", Value: r.Method},
		domain.Field{Key: "is_query_endpoint", Value: h.isQueryEndpoint(r.URL.Path)})
	
	if h.isQueryEndpoint(r.URL.Path) {
		originalQuery := h.extractQuery(r)
		userLogger.Info("Extracted query from request",
			domain.Field{Key: "original_query", Value: originalQuery},
			domain.Field{Key: "query_empty", Value: originalQuery == ""})
		
		if originalQuery != "" {
			userLogger.Info("Calling FilterQuery function",
				domain.Field{Key: "original_query", Value: originalQuery})
			
			var err error
			filteredQuery, err = h.tenantService.FilterQuery(ctx, user, originalQuery)
			if err != nil {
				userLogger.Error("Query filtering failed", loggerPkg.Error(err))
				if appErr, ok := err.(*domain.AppError); ok {
					h.writeError(w, appErr)
					h.recordMetrics(r.Method, r.URL.Path, fmt.Sprintf("%d", appErr.HTTPStatus), time.Since(startTime), user)
				} else {
					h.writeError(w, domain.ErrForbidden)
					h.recordMetrics(r.Method, r.URL.Path, "403", time.Since(startTime), user)
				}
				return
			}
			
			userLogger.Info("Query filtering completed",
				domain.Field{Key: "original_query", Value: originalQuery},
				domain.Field{Key: "filtered_query", Value: filteredQuery},
				domain.Field{Key: "filter_applied", Value: originalQuery != filteredQuery})
		} else {
			userLogger.Warn("No query found in request - cannot apply tenant filtering")
		}
	}

	// Determine target tenant for write operations
	var targetTenant string
	if h.isWriteEndpoint(r.URL.Path, r.Method) {
		var err error
		targetTenant, err = h.tenantService.DetermineTargetTenant(ctx, user, r)
		if err != nil {
			h.logger.Error("Failed to determine target tenant", loggerPkg.Error(err))
			if appErr, ok := err.(*domain.AppError); ok {
				h.writeError(w, appErr)
				h.recordMetrics(r.Method, r.URL.Path, fmt.Sprintf("%d", appErr.HTTPStatus), time.Since(startTime), user)
			} else {
				h.writeError(w, domain.ErrTenantRequired)
				h.recordMetrics(r.Method, r.URL.Path, "400", time.Since(startTime), user)
			}
			return
		}
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
		if appErr, ok := err.(*domain.AppError); ok {
			h.writeError(w, appErr)
			h.recordMetrics(r.Method, r.URL.Path, fmt.Sprintf("%d", appErr.HTTPStatus), time.Since(startTime), user)
		} else {
			h.writeError(w, domain.ErrUpstreamUnavailable)
			h.recordMetrics(r.Method, r.URL.Path, "502", time.Since(startTime), user)
		}
		return
	}

	// Write response
	h.writeResponse(w, response)
	h.recordMetrics(r.Method, r.URL.Path, fmt.Sprintf("%d", response.StatusCode), time.Since(startTime), user)

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
	if r.Method == "POST" && strings.Contains(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
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
	w.Write(response.Body)
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

func (h *GatewayHandler) recordMetrics(method, path, status string, duration time.Duration, user *domain.User) {
	if h.metricsService != nil {
		ctx := context.Background()
		h.metricsService.RecordRequest(ctx, method, path, status, duration, user)
	}
}

func generateRequestID() string {
	// Simple request ID generation - in production, use UUID or similar
	return fmt.Sprintf("%d", time.Now().UnixNano())
}