package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestHandlers_HealthCheck(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	handlers := NewHandlers(nil, nil, logger)

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	handlers.HealthCheck(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}
}

func TestHandlers_ReadinessCheck(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	handlers := NewHandlers(nil, nil, logger)

	req := httptest.NewRequest("GET", "/readiness", nil)
	rr := httptest.NewRecorder()

	handlers.ReadinessCheck(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestHandlers_WriteErrorResponse(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	handlers := NewHandlers(nil, nil, logger)

	rr := httptest.NewRecorder()

	handlers.writeErrorResponse(rr, http.StatusNotFound, "Resource not found")

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}
}