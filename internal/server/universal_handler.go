package server

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/finlego/prometheus-oauth-gateway/internal/metrics"
	"github.com/finlego/prometheus-oauth-gateway/internal/middleware"
	"github.com/finlego/prometheus-oauth-gateway/internal/proxy"
	"github.com/finlego/prometheus-oauth-gateway/pkg/tenant"
	"github.com/sirupsen/logrus"
)

// UniversalHandler handles all VictoriaMetrics requests universally
type UniversalHandler struct {
	proxy        *proxy.Proxy
	tenantMapper *tenant.Mapper
	logger       *logrus.Logger
}

// NewUniversalHandler creates a new universal handler
func NewUniversalHandler(proxy *proxy.Proxy, tenantMapper *tenant.Mapper, logger *logrus.Logger) *UniversalHandler {
	return &UniversalHandler{
		proxy:        proxy,
		tenantMapper: tenantMapper,
		logger:       logger,
	}
}

// HandleRequest processes all VictoriaMetrics requests
func (h *UniversalHandler) HandleRequest(w http.ResponseWriter, r *http.Request) {
	userCtx := middleware.GetUserContext(r)
	if userCtx == nil {
		h.writeErrorResponse(w, http.StatusUnauthorized, "User context not found")
		return
	}

	path := r.URL.Path
	method := r.Method

	h.logger.WithFields(logrus.Fields{
		"user_id":       userCtx.UserID,
		"path":          path,
		"method":        method,
		"groups":        userCtx.Groups,
		"tenants":       userCtx.AllowedTenants,
		"query_params":  r.URL.RawQuery,
		"content_type":  r.Header.Get("Content-Type"),
		"is_query_range": strings.Contains(path, "query_range"),
	}).Info("Processing universal request")

	// Basic access control checks
	if err := h.checkBasicAccess(userCtx, path, method); err != nil {
		h.logger.WithError(err).WithFields(logrus.Fields{
			"user_id": userCtx.UserID,
			"path":    path,
			"method":  method,
		}).Warn("Access denied")
		
		status := http.StatusForbidden
		if err.Error() == "read-only user cannot perform write operations" {
			status = http.StatusForbidden
		}
		h.writeErrorResponse(w, status, err.Error())
		return
	}

	// Handle query filtering for read endpoints that might contain PromQL
	// For GET requests, we filter here. For POST requests, filtering happens in proxy layer.
	originalQuery := h.extractQuery(r)
	filteredQuery := originalQuery

	if originalQuery != "" && h.isQueryBasedEndpoint(path) {
		var err error
		filteredQuery, err = h.tenantMapper.FilterTenantsInQuery(userCtx.Groups, originalQuery)
		if err != nil {
			h.logger.WithError(err).WithFields(logrus.Fields{
				"user_id":        userCtx.UserID,
				"original_query": originalQuery,
				"path":           path,
			}).Error("Failed to filter query")
			
			h.writeErrorResponse(w, http.StatusForbidden, "Query access denied")
			return
		}

		h.logger.WithFields(logrus.Fields{
			"user_id":        userCtx.UserID,
			"path":           path,
			"original_query": originalQuery,
			"filtered_query": filteredQuery,
			"query_modified": filteredQuery != originalQuery,
			"allowed_tenants": userCtx.AllowedTenants,
		}).Debug("Query filtering applied")

		if filteredQuery != originalQuery {
			metrics.RecordQueryFilter(userCtx.UserID, strings.Join(userCtx.AllowedTenants, ","))
		}
	} else if method == "POST" && h.isQueryBasedEndpoint(path) {
		h.logger.WithFields(logrus.Fields{
			"user_id": userCtx.UserID,
			"path":    path,
			"method":  method,
		}).Debug("POST query endpoint - filtering will be handled in proxy layer")
	}

	// Determine target tenant for write operations
	var targetTenant string
	if h.isPotentialWriteEndpoint(path, method) {
		targetTenant = h.determineTargetTenant(r, userCtx)
		if targetTenant == "" && len(userCtx.AllowedTenants) > 1 && !contains(userCtx.AllowedTenants, "*") {
			h.writeErrorResponse(w, http.StatusBadRequest, 
				"Target tenant must be specified for write operations. Use X-Target-Tenant header or tenant query parameter.")
			return
		}
	}

	// Create proxy request
	proxyReq := &proxy.ProxyRequest{
		OriginalRequest: r,
		ModifiedQuery:   filteredQuery,
		AllowedTenants:  userCtx.AllowedTenants,
		UserID:          userCtx.UserID,
		ReadOnly:        userCtx.ReadOnly,
		TargetTenant:    targetTenant,
		UserGroups:      userCtx.Groups,
	}

	// Forward request to upstream
	start := time.Now()
	resp, err := h.proxy.ForwardRequest(r.Context(), proxyReq)
	duration := time.Since(start)

	if err != nil {
		h.logger.WithError(err).WithFields(logrus.Fields{
			"user_id": userCtx.UserID,
			"path":    path,
			"method":  method,
		}).Error("Failed to forward request to upstream")
		
		h.writeErrorResponse(w, http.StatusBadGateway, "Failed to forward request to upstream")
		
		metrics.RecordUpstreamRequest(
			method, path, "502", 
			strings.Join(userCtx.AllowedTenants, ","), duration,
		)
		return
	}
	defer resp.Body.Close()

	// Record metrics
	metrics.RecordUpstreamRequest(
		method, path, fmt.Sprintf("%d", resp.StatusCode),
		strings.Join(userCtx.AllowedTenants, ","), duration,
	)

	// Copy response back to client
	h.copyResponse(w, resp)
}

// checkBasicAccess performs basic access control checks
func (h *UniversalHandler) checkBasicAccess(userCtx *middleware.UserContext, path, method string) error {
	// Check admin access for admin endpoints
	if h.isAdminEndpoint(path) && !h.hasAdminAccess(userCtx) {
		return fmt.Errorf("admin access required for endpoint: %s", path)
	}

	// Check write access for write operations
	if h.isPotentialWriteEndpoint(path, method) && userCtx.ReadOnly {
		return fmt.Errorf("read-only user cannot perform write operations")
	}

	return nil
}

// isQueryBasedEndpoint checks if endpoint likely contains PromQL queries
func (h *UniversalHandler) isQueryBasedEndpoint(path string) bool {
	queryEndpoints := []string{
		"/api/v1/query",
		"/api/v1/query_range", 
		"/api/v1/query_exemplars",
		"/api/v1/series",
		"/select/", // VictoriaMetrics multi-tenant query endpoints
	}

	for _, endpoint := range queryEndpoints {
		if strings.HasPrefix(path, endpoint) {
			return true
		}
	}

	// Check for multi-tenant query patterns: /select/{tenant}/prometheus/api/v1/query*
	if strings.Contains(path, "/prometheus/api/v1/query") {
		return true
	}

	return false
}

// isPotentialWriteEndpoint checks if endpoint might be a write operation
func (h *UniversalHandler) isPotentialWriteEndpoint(path, method string) bool {
	// Method-based checks
	if method == "POST" || method == "PUT" || method == "DELETE" {
		// Known write endpoints
		writeEndpoints := []string{
			"/api/v1/write",
			"/api/v1/import",
			"/insert/",
			"/api/v1/admin",
		}
		
		for _, endpoint := range writeEndpoints {
			if strings.HasPrefix(path, endpoint) {
				return true
			}
		}
		
		// DELETE operations on series
		if method == "DELETE" && strings.Contains(path, "/api/v1/series") {
			return true
		}
	}

	return false
}

// isAdminEndpoint checks if endpoint requires admin access
func (h *UniversalHandler) isAdminEndpoint(path string) bool {
	adminEndpoints := []string{
		"/api/v1/admin/",
		"/api/v1/status/tsdb",
		"/api/v1/status/flags", 
		"/api/v1/status/config",
	}

	for _, endpoint := range adminEndpoints {
		if strings.HasPrefix(path, endpoint) {
			return true
		}
	}

	return false
}

// hasAdminAccess checks if user has admin permissions
func (h *UniversalHandler) hasAdminAccess(userCtx *middleware.UserContext) bool {
	adminGroups := []string{"admin", "platform-admin", "developers"}
	for _, userGroup := range userCtx.Groups {
		for _, adminGroup := range adminGroups {
			if userGroup == adminGroup {
				return true
			}
		}
	}
	return false
}

// determineTargetTenant determines the target tenant for write operations
func (h *UniversalHandler) determineTargetTenant(r *http.Request, userCtx *middleware.UserContext) string {
	// Check headers first
	if targetTenant := r.Header.Get("X-Target-Tenant"); targetTenant != "" {
		// Verify user has access to this tenant
		if h.userHasTenantAccess(userCtx, targetTenant) {
			return targetTenant
		}
	}

	// Check query parameters
	if targetTenant := r.URL.Query().Get("tenant"); targetTenant != "" {
		if h.userHasTenantAccess(userCtx, targetTenant) {
			return targetTenant
		}
	}

	// Auto-detect from path (for multi-tenant endpoints like /insert/{tenant}/...)
	if targetTenant := h.extractTenantFromPath(r.URL.Path); targetTenant != "" {
		if h.userHasTenantAccess(userCtx, targetTenant) {
			return targetTenant
		}
	}

	// Default to first allowed tenant if user has only one
	if len(userCtx.AllowedTenants) == 1 {
		return userCtx.AllowedTenants[0]
	}

	// Wildcard access
	if contains(userCtx.AllowedTenants, "*") {
		return "*"
	}

	return ""
}

// extractTenantFromPath extracts tenant from URL paths like /insert/{tenant}/... or /select/{tenant}/...
func (h *UniversalHandler) extractTenantFromPath(path string) string {
	// Handle /insert/{tenant}/... pattern
	if strings.HasPrefix(path, "/insert/") {
		parts := strings.Split(path, "/")
		if len(parts) >= 3 {
			return parts[2]
		}
	}

	// Handle /select/{tenant}/... pattern
	if strings.HasPrefix(path, "/select/") {
		parts := strings.Split(path, "/")
		if len(parts) >= 3 {
			return parts[2]
		}
	}

	return ""
}

// userHasTenantAccess checks if user has access to specified tenant
func (h *UniversalHandler) userHasTenantAccess(userCtx *middleware.UserContext, tenantID string) bool {
	if contains(userCtx.AllowedTenants, "*") {
		return true
	}
	return contains(userCtx.AllowedTenants, tenantID)
}

// extractQuery extracts query parameter from request
func (h *UniversalHandler) extractQuery(r *http.Request) string {
	// First try URL query parameters (GET requests)
	if query := r.URL.Query().Get("query"); query != "" {
		h.logger.WithFields(logrus.Fields{
			"method": r.Method,
			"path":   r.URL.Path,
			"source": "url_params",
			"query":  query,
		}).Debug("Extracted query from URL parameters")
		return query
	}

	// For POST requests, we need to be careful not to consume the body
	// since it will be read again in the proxy layer.
	// We'll extract the query there during proxy processing.
	if r.Method == "POST" {
		h.logger.WithFields(logrus.Fields{
			"method":       r.Method,
			"path":         r.URL.Path,
			"content_type": r.Header.Get("Content-Type"),
		}).Debug("POST request detected - query will be extracted during proxy processing")
		
		// Return empty string for POST - query will be handled in proxy layer
		return ""
	}

	h.logger.WithFields(logrus.Fields{
		"method":       r.Method,
		"path":         r.URL.Path,
		"content_type": r.Header.Get("Content-Type"),
		"url_query":    r.URL.RawQuery,
	}).Debug("No query parameter found in request")

	return ""
}

// Helper functions
func (h *UniversalHandler) copyResponse(w http.ResponseWriter, resp *http.Response) {
	// Copy headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Set status code
	w.WriteHeader(resp.StatusCode)

	// Copy body
	if _, err := io.Copy(w, resp.Body); err != nil {
		h.logger.WithError(err).Error("Failed to copy response body")
	}
}

func (h *UniversalHandler) writeErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	fmt.Fprintf(w, `{"status":"error","error":"%s","code":%d}`, message, statusCode)
}

