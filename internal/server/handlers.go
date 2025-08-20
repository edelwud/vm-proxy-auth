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

func (h *Handlers) PrometheusProxy(w http.ResponseWriter, r *http.Request) {
	userCtx := middleware.GetUserContext(r)
	if userCtx == nil {
		h.writeErrorResponse(w, http.StatusUnauthorized, "User context not found")
		return
	}

	originalQuery := h.extractQuery(r)
	filteredQuery := originalQuery

	if originalQuery != "" && h.isQueryEndpoint(r.URL.Path) {
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

func (h *Handlers) isQueryEndpoint(path string) bool {
	queryEndpoints := []string{
		"/api/v1/query",
		"/api/v1/query_range",
		"/api/v1/query_exemplars",
	}

	for _, endpoint := range queryEndpoints {
		if strings.HasPrefix(path, endpoint) {
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