package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/finlego/prometheus-oauth-gateway/internal/metrics"
)

type MetricsMiddleware struct{}

func NewMetricsMiddleware() *MetricsMiddleware {
	return &MetricsMiddleware{}
}

func (m *MetricsMiddleware) RecordMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		metrics.RecordActiveConnection()
		defer metrics.RecordConnectionClosed()

		wrapper := &responseWrapper{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(wrapper, r)

		duration := time.Since(start)
		statusCode := strconv.Itoa(wrapper.statusCode)
		
		tenant := "unknown"
		if userCtx := GetUserContext(r); userCtx != nil {
			if len(userCtx.AllowedTenants) > 0 {
				tenant = userCtx.AllowedTenants[0]
			}
		}

		metrics.RecordRequest(r.Method, r.URL.Path, statusCode, tenant, duration)
	})
}

type responseWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}