package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/finlego/prometheus-oauth-gateway/internal/domain"
)

// HealthHandler handles health check endpoints
type HealthHandler struct {
	logger domain.Logger
	version string
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(logger domain.Logger, version string) *HealthHandler {
	return &HealthHandler{
		logger:  logger,
		version: version,
	}
}

// ServeHTTP implements http.Handler interface
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Health(w, r)
}

// Health responds to health check requests
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"version":   h.version,
	}

	h.writeJSON(w, http.StatusOK, response)
}

// Readiness responds to readiness check requests
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status": "ready",
		"checks": map[string]string{
			"upstream": "ok",
		},
	}

	h.writeJSON(w, http.StatusOK, response)
}

func (h *HealthHandler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("Failed to encode JSON response", domain.Field{Key: "error", Value: err.Error()})
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}