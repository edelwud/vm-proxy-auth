package middleware

import (
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

type LoggingMiddleware struct {
	logger *logrus.Logger
}

func NewLoggingMiddleware(logger *logrus.Logger) *LoggingMiddleware {
	return &LoggingMiddleware{
		logger: logger,
	}
}

func (m *LoggingMiddleware) LogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		wrapper := &loggingResponseWrapper{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(wrapper, r)

		duration := time.Since(start)

		fields := logrus.Fields{
			"method":      r.Method,
			"path":        r.URL.Path,
			"query":       r.URL.RawQuery,
			"status_code": wrapper.statusCode,
			"duration_ms": duration.Milliseconds(),
			"remote_addr": r.RemoteAddr,
			"user_agent":  r.UserAgent(),
		}

		if userCtx := GetUserContext(r); userCtx != nil {
			fields["user_id"] = userCtx.UserID
			fields["user_groups"] = userCtx.Groups
			fields["allowed_tenants"] = userCtx.AllowedTenants
			fields["read_only"] = userCtx.ReadOnly
		}

		if wrapper.statusCode >= 400 {
			m.logger.WithFields(fields).Warn("Request completed with error")
		} else {
			m.logger.WithFields(fields).Info("Request completed")
		}
	})
}

type loggingResponseWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *loggingResponseWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}