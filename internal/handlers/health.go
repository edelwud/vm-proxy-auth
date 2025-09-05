package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// HealthHandler handles health check endpoints.
type HealthHandler struct {
	logger       domain.Logger
	version      string
	proxyService domain.ProxyService
}

// NewHealthHandler creates a new health handler.
func NewHealthHandler(logger domain.Logger, version string, proxyService domain.ProxyService) *HealthHandler {
	return &HealthHandler{
		logger:       logger.With(domain.Field{Key: "component", Value: "health"}),
		version:      version,
		proxyService: proxyService,
	}
}

// ServeHTTP implements http.Handler interface.
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Health(w, r)
}

// Health responds to health check requests.
func (h *HealthHandler) Health(w http.ResponseWriter, _ *http.Request) {
	response := map[string]any{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"version":   h.version,
	}

	h.writeJSON(w, http.StatusOK, response)
}

// Readiness responds to readiness check requests.
func (h *HealthHandler) Readiness(w http.ResponseWriter, _ *http.Request) {
	checks := make(map[string]string)

	status, statusCode := h.checkBackendHealth(checks)

	response := map[string]any{
		"status": status,
		"checks": checks,
	}

	h.writeJSON(w, statusCode, response)
}

// checkBackendHealth checks backend health and returns status information.
func (h *HealthHandler) checkBackendHealth(checks map[string]string) (string, int) {
	if h.proxyService == nil {
		checks["backends"] = "proxy service not configured"
		return string(domain.HealthStatusDegraded), http.StatusServiceUnavailable
	}

	backendStatuses := h.proxyService.GetBackendsStatus()
	healthyBackends := h.countHealthyBackends(backendStatuses)
	totalBackends := len(backendStatuses)

	switch {
	case totalBackends == 0:
		checks["backends"] = "no backends configured"
		return string(domain.HealthStatusDegraded), http.StatusServiceUnavailable
	case healthyBackends == 0:
		checks["backends"] = "all backends unhealthy"
		return string(domain.HealthStatusDegraded), http.StatusServiceUnavailable
	case healthyBackends < totalBackends:
		checks["backends"] = fmt.Sprintf("%d/%d backends healthy", healthyBackends, totalBackends)
		return "ready", http.StatusOK
	default:
		checks["backends"] = "all backends healthy"
		return "ready", http.StatusOK
	}
}

// countHealthyBackends counts the number of healthy backends.
func (h *HealthHandler) countHealthyBackends(statuses []*domain.BackendStatus) int {
	healthyCount := 0
	for _, status := range statuses {
		if status.IsHealthy {
			healthyCount++
		}
	}
	return healthyCount
}

func (h *HealthHandler) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("Failed to encode JSON response", domain.Field{Key: "error", Value: err.Error()})
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
