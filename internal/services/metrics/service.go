package metrics

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

var (
	// HTTP request metrics
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vm_proxy_auth_http_requests_total",
			Help: "Total number of HTTP requests processed by vm-proxy-auth",
		},
		[]string{"method", "path", "status_code", "user_id"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vm_proxy_auth_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "status_code"},
	)

	// Upstream (VictoriaMetrics) request metrics
	upstreamRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vm_proxy_auth_upstream_requests_total",
			Help: "Total number of upstream requests to VictoriaMetrics",
		},
		[]string{"method", "path", "status_code", "tenant_count"},
	)

	upstreamRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vm_proxy_auth_upstream_request_duration_seconds",
			Help:    "Upstream request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "status_code"},
	)

	// Authentication metrics
	authAttemptsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vm_proxy_auth_auth_attempts_total",
			Help: "Total number of authentication attempts",
		},
		[]string{"status", "user_id"},
	)

	// Query filtering metrics
	queryFilteringTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vm_proxy_auth_query_filtering_total",
			Help: "Total number of PromQL queries processed for tenant filtering",
		},
		[]string{"user_id", "tenant_count", "filter_applied"},
	)

	queryFilteringDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vm_proxy_auth_query_filtering_duration_seconds",
			Help:    "Time spent filtering PromQL queries",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
		},
		[]string{"user_id"},
	)

	// Tenant access metrics
	tenantAccessTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vm_proxy_auth_tenant_access_total",
			Help: "Total number of tenant access checks",
		},
		[]string{"user_id", "tenant_id", "allowed"},
	)
)

// Service implements domain.MetricsService
type Service struct {
	logger   domain.Logger
	registry *prometheus.Registry
}

// NewService creates a new metrics service
func NewService(logger domain.Logger) *Service {
	registry := prometheus.NewRegistry()
	
	// Register all metrics
	registry.MustRegister(
		httpRequestsTotal,
		httpRequestDuration,
		upstreamRequestsTotal,
		upstreamRequestDuration,
		authAttemptsTotal,
		queryFilteringTotal,
		queryFilteringDuration,
		tenantAccessTotal,
	)

	// Also register Go runtime metrics
	registry.MustRegister(prometheus.NewGoCollector())
	registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))

	return &Service{
		logger:   logger,
		registry: registry,
	}
}

// Handler returns HTTP handler for metrics endpoint
func (s *Service) Handler() http.Handler {
	return promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// RecordRequest records metrics for incoming requests
func (s *Service) RecordRequest(ctx context.Context, method, path, status string, duration time.Duration, user *domain.User) {
	statusCode := status
	userID := "anonymous"
	if user != nil {
		userID = user.ID
	}

	// Record HTTP request metrics
	httpRequestsTotal.WithLabelValues(method, path, statusCode, userID).Inc()
	httpRequestDuration.WithLabelValues(method, path, statusCode).Observe(duration.Seconds())

	s.logger.Debug("HTTP request metrics recorded",
		domain.Field{Key: "method", Value: method},
		domain.Field{Key: "path", Value: path},
		domain.Field{Key: "status", Value: status},
		domain.Field{Key: "duration_ms", Value: duration.Milliseconds()},
		domain.Field{Key: "user_id", Value: userID},
	)
}

// RecordUpstream records metrics for upstream requests
func (s *Service) RecordUpstream(ctx context.Context, method, path, status string, duration time.Duration, tenants []string) {
	statusCode := status
	tenantCount := strconv.Itoa(len(tenants))

	// Record upstream request metrics
	upstreamRequestsTotal.WithLabelValues(method, path, statusCode, tenantCount).Inc()
	upstreamRequestDuration.WithLabelValues(method, path, statusCode).Observe(duration.Seconds())

	s.logger.Debug("Upstream request metrics recorded",
		domain.Field{Key: "method", Value: method},
		domain.Field{Key: "path", Value: path},
		domain.Field{Key: "status", Value: status},
		domain.Field{Key: "duration_ms", Value: duration.Milliseconds()},
		domain.Field{Key: "tenant_count", Value: len(tenants)},
	)
}

// RecordQueryFilter records metrics for query filtering operations
func (s *Service) RecordQueryFilter(ctx context.Context, userID string, tenantCount int, filterApplied bool, duration time.Duration) {
	tenantCountStr := strconv.Itoa(tenantCount)
	filterAppliedStr := strconv.FormatBool(filterApplied)

	// Record query filtering metrics
	queryFilteringTotal.WithLabelValues(userID, tenantCountStr, filterAppliedStr).Inc()
	queryFilteringDuration.WithLabelValues(userID).Observe(duration.Seconds())

	s.logger.Debug("Query filtering metrics recorded",
		domain.Field{Key: "user_id", Value: userID},
		domain.Field{Key: "tenant_count", Value: tenantCount},
		domain.Field{Key: "filter_applied", Value: filterApplied},
		domain.Field{Key: "duration_ms", Value: duration.Milliseconds()},
	)
}

// RecordAuthAttempt records authentication attempt metrics
func (s *Service) RecordAuthAttempt(ctx context.Context, userID, status string) {
	authAttemptsTotal.WithLabelValues(status, userID).Inc()

	s.logger.Debug("Auth attempt metrics recorded",
		domain.Field{Key: "user_id", Value: userID},
		domain.Field{Key: "status", Value: status},
	)
}

// RecordTenantAccess records tenant access check metrics
func (s *Service) RecordTenantAccess(ctx context.Context, userID, tenantID string, allowed bool) {
	allowedStr := strconv.FormatBool(allowed)
	tenantAccessTotal.WithLabelValues(userID, tenantID, allowedStr).Inc()

	s.logger.Debug("Tenant access metrics recorded",
		domain.Field{Key: "user_id", Value: userID},
		domain.Field{Key: "tenant_id", Value: tenantID},
		domain.Field{Key: "allowed", Value: allowed},
	)
}