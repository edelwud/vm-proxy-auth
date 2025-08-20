package server

import (
	"encoding/json"
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

type Handlers struct {
	proxy        *proxy.Proxy
	tenantMapper *tenant.Mapper
	logger       *logrus.Logger
}

func NewHandlers(proxy *proxy.Proxy, tenantMapper *tenant.Mapper, logger *logrus.Logger) *Handlers {
	return &Handlers{
		proxy:        proxy,
		tenantMapper: tenantMapper,
		logger:       logger,
	}
}

func (h *Handlers) HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	
	response := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"version":   "1.0.0",
	}
	
	json.NewEncoder(w).Encode(response)
}

func (h *Handlers) ReadinessCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	
	response := map[string]interface{}{
		"status": "ready",
		"checks": map[string]string{
			"upstream": "ok",
		},
	}
	
	json.NewEncoder(w).Encode(response)
}

func (h *Handlers) VictoriaMetricsProxy(w http.ResponseWriter, r *http.Request) {
	userCtx := middleware.GetUserContext(r)
	if userCtx == nil {
		h.writeErrorResponse(w, http.StatusUnauthorized, "User context not found")
		return
	}

	// Check access permissions for this endpoint
	if err := CheckAccess(userCtx, r.URL.Path, r.Method); err != nil {
		h.logger.WithError(err).WithFields(logrus.Fields{
			"user_id": userCtx.UserID,
			"path":    r.URL.Path,
			"method":  r.Method,
		}).Warn("Access denied")
		
		status := http.StatusForbidden
		if accessErr, ok := err.(*AccessError); ok && accessErr.Type == "method" {
			status = http.StatusMethodNotAllowed
		}
		h.writeErrorResponse(w, status, err.Error())
		return
	}

	// Handle query filtering for read endpoints
	originalQuery := h.extractQuery(r)
	filteredQuery := originalQuery

	if originalQuery != "" && RequiresTenantFiltering(r.URL.Path) {
		var err error
		filteredQuery, err = h.tenantMapper.FilterTenantsInQuery(userCtx.Groups, originalQuery)
		if err != nil {
			h.logger.WithError(err).WithFields(logrus.Fields{
				"user_id":        userCtx.UserID,
				"original_query": originalQuery,
			}).Error("Failed to filter query")
			
			h.writeErrorResponse(w, http.StatusForbidden, "Query access denied")
			return
		}

		if filteredQuery != originalQuery {
			metrics.RecordQueryFilter(userCtx.UserID, strings.Join(userCtx.AllowedTenants, ","))
		}
	}

	// Handle write operations with tenant injection
	if RequiresWriteAccess(r.URL.Path) {
		h.handleWriteOperation(w, r, userCtx)
		return
	}

	proxyReq := &proxy.ProxyRequest{
		OriginalRequest: r,
		ModifiedQuery:   filteredQuery,
		AllowedTenants:  userCtx.AllowedTenants,
		UserID:          userCtx.UserID,
		ReadOnly:        userCtx.ReadOnly,
	}

	start := time.Now()
	resp, err := h.proxy.ForwardRequest(r.Context(), proxyReq)
	duration := time.Since(start)

	if err != nil {
		h.logger.WithError(err).WithField("user_id", userCtx.UserID).Error("Failed to forward request to upstream")
		h.writeErrorResponse(w, http.StatusBadGateway, "Failed to forward request")
		
		metrics.RecordUpstreamRequest(
			r.Method, r.URL.Path, "502", 
			strings.Join(userCtx.AllowedTenants, ","), duration,
		)
		return
	}
	defer resp.Body.Close()

	metrics.RecordUpstreamRequest(
		r.Method, r.URL.Path, fmt.Sprintf("%d", resp.StatusCode),
		strings.Join(userCtx.AllowedTenants, ","), duration,
	)

	h.copyResponse(w, resp)
}

func (h *Handlers) extractQuery(r *http.Request) string {
	if query := r.URL.Query().Get("query"); query != "" {
		return query
	}

	if r.Method == "POST" {
		contentType := r.Header.Get("Content-Type")
		if strings.Contains(contentType, "application/x-www-form-urlencoded") {
			if err := r.ParseForm(); err == nil {
				return r.PostForm.Get("query")
			}
		}
	}

	return ""
}

// handleWriteOperation handles write operations with tenant injection
func (h *Handlers) handleWriteOperation(w http.ResponseWriter, r *http.Request, userCtx *middleware.UserContext) {
	// For write operations, we need to inject tenant information
	// This ensures data is written to the correct tenant
	
	if len(userCtx.AllowedTenants) == 0 {
		h.writeErrorResponse(w, http.StatusForbidden, "No tenant access configured for write operations")
		return
	}

	// Determine target tenant for write operation
	var targetTenant string

	// If user has access to multiple tenants, they must specify which one
	if len(userCtx.AllowedTenants) > 1 && !contains(userCtx.AllowedTenants, "*") {
		// Check if tenant is specified in request headers or query params
		targetTenant = r.Header.Get("X-Target-Tenant")
		if targetTenant == "" {
			targetTenant = r.URL.Query().Get("tenant")
		}
		
		if targetTenant == "" {
			h.writeErrorResponse(w, http.StatusBadRequest, "Target tenant must be specified for write operations when user has access to multiple tenants")
			return
		}
		
		// Verify user has access to specified tenant
		allowed := false
		for _, allowedTenant := range userCtx.AllowedTenants {
			if allowedTenant == targetTenant {
				allowed = true
				break
			}
		}
		
		if !allowed {
			h.writeErrorResponse(w, http.StatusForbidden, fmt.Sprintf("Access denied to tenant: %s", targetTenant))
			return
		}
	} else if len(userCtx.AllowedTenants) == 1 {
		// Single tenant access - use it
		targetTenant = userCtx.AllowedTenants[0]
	}

	// Proceed with proxying the write request
	proxyReq := &proxy.ProxyRequest{
		OriginalRequest: r,
		AllowedTenants:  userCtx.AllowedTenants,
		UserID:          userCtx.UserID,
		ReadOnly:        userCtx.ReadOnly,
		TargetTenant:    targetTenant,
	}

	start := time.Now()
	resp, err := h.proxy.ForwardRequest(r.Context(), proxyReq)
	duration := time.Since(start)

	if err != nil {
		h.logger.WithError(err).WithField("user_id", userCtx.UserID).Error("Failed to forward write request")
		h.writeErrorResponse(w, http.StatusBadGateway, "Failed to forward request")
		
		metrics.RecordUpstreamRequest(
			r.Method, r.URL.Path, "502", 
			strings.Join(userCtx.AllowedTenants, ","), duration,
		)
		return
	}
	defer resp.Body.Close()

	metrics.RecordUpstreamRequest(
		r.Method, r.URL.Path, fmt.Sprintf("%d", resp.StatusCode),
		strings.Join(userCtx.AllowedTenants, ","), duration,
	)

	h.copyResponse(w, resp)
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (h *Handlers) copyResponse(w http.ResponseWriter, resp *http.Response) {
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)

	if _, err := io.Copy(w, resp.Body); err != nil {
		h.logger.WithError(err).Error("Failed to copy response body")
	}
}

func (h *Handlers) writeErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	errorResp := map[string]interface{}{
		"status": "error",
		"error":  message,
		"code":   statusCode,
	}
	
	json.NewEncoder(w).Encode(errorResp)
}