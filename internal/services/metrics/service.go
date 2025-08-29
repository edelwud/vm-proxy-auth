package metrics

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// metricsSet holds all Prometheus metrics to avoid global variables.
type metricsSet struct {
	httpRequestsTotal       *prometheus.CounterVec
	httpRequestDuration     *prometheus.HistogramVec
	upstreamRequestsTotal   *prometheus.CounterVec
	upstreamRequestDuration *prometheus.HistogramVec
	authAttemptsTotal       *prometheus.CounterVec
	queryFilteringTotal     *prometheus.CounterVec
	queryFilteringDuration  *prometheus.HistogramVec
	tenantAccessTotal       *prometheus.CounterVec
	// Backend-specific metrics.
	upstreamBackendRequestsTotal   *prometheus.CounterVec
	upstreamBackendRequestDuration *prometheus.HistogramVec
	healthCheckTotal               *prometheus.CounterVec
	healthCheckDuration            *prometheus.HistogramVec
	backendStateChangesTotal       *prometheus.CounterVec
	circuitBreakerStateTotal       *prometheus.CounterVec
	queueOperationsTotal           *prometheus.CounterVec
	queueOperationsDuration        *prometheus.HistogramVec
	loadBalancerSelectionsTotal    *prometheus.CounterVec
	loadBalancerSelectionDuration  *prometheus.HistogramVec
}

// newMetricsSet creates a new set of metrics with proper initialization.
func newMetricsSet() *metricsSet {
	return &metricsSet{
		httpRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "vm_proxy_auth_http_requests_total",
				Help: "Total number of HTTP requests processed by vm-proxy-auth",
			},
			[]string{"method", "path", "status_code", "user_id"},
		),
		httpRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "vm_proxy_auth_http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path", "status_code"},
		),
		upstreamRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "vm_proxy_auth_upstream_requests_total",
				Help: "Total number of upstream requests to VictoriaMetrics",
			},
			[]string{"method", "path", "status_code", "tenant_count"},
		),
		upstreamRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "vm_proxy_auth_upstream_request_duration_seconds",
				Help:    "Upstream request duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path", "status_code"},
		),
		authAttemptsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "vm_proxy_auth_auth_attempts_total",
				Help: "Total number of authentication attempts",
			},
			[]string{"status", "user_id"},
		),
		queryFilteringTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "vm_proxy_auth_query_filtering_total",
				Help: "Total number of PromQL queries processed for tenant filtering",
			},
			[]string{"user_id", "tenant_count", "filter_applied"},
		),
		queryFilteringDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "vm_proxy_auth_query_filtering_duration_seconds",
				Help:    "Time spent filtering PromQL queries",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
			},
			[]string{"user_id"},
		),
		tenantAccessTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "vm_proxy_auth_tenant_access_total",
				Help: "Total number of tenant access checks",
			},
			[]string{"user_id", "tenant_id", "allowed"},
		),
		// Backend-specific metrics.
		upstreamBackendRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "vm_proxy_auth_upstream_backend_requests_total",
				Help: "Total number of requests to specific upstream backends",
			},
			[]string{"backend_url", "method", "path", "status_code"},
		),
		upstreamBackendRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "vm_proxy_auth_upstream_backend_request_duration_seconds",
				Help:    "Duration of requests to specific upstream backends",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"backend_url", "method", "path", "status_code"},
		),
		healthCheckTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "vm_proxy_auth_health_check_total",
				Help: "Total number of health checks performed",
			},
			[]string{"backend_url", "result"},
		),
		healthCheckDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "vm_proxy_auth_health_check_duration_seconds",
				Help:    "Duration of health checks",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"backend_url"},
		),
		backendStateChangesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "vm_proxy_auth_backend_state_changes_total",
				Help: "Total number of backend state changes",
			},
			[]string{"backend_url", "old_state", "new_state"},
		),
		circuitBreakerStateTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "vm_proxy_auth_circuit_breaker_state_total",
				Help: "Total number of circuit breaker state changes",
			},
			[]string{"backend_url", "state"},
		),
		queueOperationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "vm_proxy_auth_queue_operations_total",
				Help: "Total number of queue operations",
			},
			[]string{"operation"},
		),
		queueOperationsDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "vm_proxy_auth_queue_operations_duration_seconds",
				Help:    "Duration of queue operations",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"operation"},
		),
		loadBalancerSelectionsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "vm_proxy_auth_load_balancer_selections_total",
				Help: "Total number of load balancer selections",
			},
			[]string{"strategy"},
		),
		loadBalancerSelectionDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "vm_proxy_auth_load_balancer_selection_duration_seconds",
				Help:    "Duration of load balancer selections",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"strategy"},
		),
	}
}

// Service implements domain.MetricsService.
type Service struct {
	logger   domain.Logger
	registry *prometheus.Registry
	metrics  *metricsSet
}

// NewService creates a new metrics service.
func NewService(logger domain.Logger) *Service {
	registry := prometheus.NewRegistry()
	metrics := newMetricsSet()

	// Register all metrics
	registry.MustRegister(
		metrics.httpRequestsTotal,
		metrics.httpRequestDuration,
		metrics.upstreamRequestsTotal,
		metrics.upstreamRequestDuration,
		metrics.authAttemptsTotal,
		metrics.queryFilteringTotal,
		metrics.queryFilteringDuration,
		metrics.tenantAccessTotal,
		// Backend-specific metrics.
		metrics.upstreamBackendRequestsTotal,
		metrics.upstreamBackendRequestDuration,
		metrics.healthCheckTotal,
		metrics.healthCheckDuration,
		metrics.backendStateChangesTotal,
		metrics.circuitBreakerStateTotal,
		metrics.queueOperationsTotal,
		metrics.queueOperationsDuration,
		metrics.loadBalancerSelectionsTotal,
		metrics.loadBalancerSelectionDuration,
	)

	// Also register Go runtime metrics.
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	return &Service{
		logger:   logger.With(domain.Field{Key: "component", Value: "metrics"}),
		registry: registry,
		metrics:  metrics,
	}
}

// Handler returns HTTP handler for metrics endpoint.
func (s *Service) Handler() http.Handler {
	return promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// RecordRequest records metrics for incoming requests.
func (s *Service) RecordRequest(
	_ context.Context,
	method,
	path,
	status string,
	duration time.Duration,
	user *domain.User,
) {
	statusCode := status
	userID := "anonymous"
	if user != nil {
		userID = user.ID
	}

	// Record HTTP request metrics.
	s.metrics.httpRequestsTotal.WithLabelValues(method, path, statusCode, userID).Inc()
	s.metrics.httpRequestDuration.WithLabelValues(method, path, statusCode).Observe(duration.Seconds())

	s.logger.Debug("HTTP request metrics recorded",
		domain.Field{Key: "method", Value: method},
		domain.Field{Key: "path", Value: path},
		domain.Field{Key: "status", Value: status},
		domain.Field{Key: "duration_ms", Value: duration.Milliseconds()},
		domain.Field{Key: "user_id", Value: userID},
	)
}

// RecordUpstream records metrics for upstream requests.
func (s *Service) RecordUpstream(
	_ context.Context,
	method, path, status string,
	duration time.Duration,
	tenants []string,
) {
	statusCode := status
	tenantCount := strconv.Itoa(len(tenants))

	// Record upstream request metrics.
	s.metrics.upstreamRequestsTotal.WithLabelValues(method, path, statusCode, tenantCount).Inc()
	s.metrics.upstreamRequestDuration.WithLabelValues(method, path, statusCode).Observe(duration.Seconds())

	s.logger.Debug("Upstream request metrics recorded",
		domain.Field{Key: "method", Value: method},
		domain.Field{Key: "path", Value: path},
		domain.Field{Key: "status", Value: status},
		domain.Field{Key: "duration_ms", Value: duration.Milliseconds()},
		domain.Field{Key: "tenant_count", Value: len(tenants)},
	)
}

// RecordQueryFilter records metrics for query filtering operations.
func (s *Service) RecordQueryFilter(
	_ context.Context,
	userID string,
	tenantCount int,
	filterApplied bool,
	duration time.Duration,
) {
	tenantCountStr := strconv.Itoa(tenantCount)
	filterAppliedStr := strconv.FormatBool(filterApplied)

	// Record query filtering metrics.
	s.metrics.queryFilteringTotal.WithLabelValues(userID, tenantCountStr, filterAppliedStr).Inc()
	s.metrics.queryFilteringDuration.WithLabelValues(userID).Observe(duration.Seconds())

	s.logger.Debug("Query filtering metrics recorded",
		domain.Field{Key: "user_id", Value: userID},
		domain.Field{Key: "tenant_count", Value: tenantCount},
		domain.Field{Key: "filter_applied", Value: filterApplied},
		domain.Field{Key: "duration_ms", Value: duration.Milliseconds()},
	)
}

// RecordAuthAttempt records authentication attempt metrics.
func (s *Service) RecordAuthAttempt(_ context.Context, userID, status string) {
	s.metrics.authAttemptsTotal.WithLabelValues(status, userID).Inc()

	s.logger.Debug("Auth attempt metrics recorded",
		domain.Field{Key: "user_id", Value: userID},
		domain.Field{Key: "status", Value: status},
	)
}

// RecordTenantAccess records tenant access check metrics.
func (s *Service) RecordTenantAccess(_ context.Context, userID, tenantID string, allowed bool) {
	allowedStr := strconv.FormatBool(allowed)
	s.metrics.tenantAccessTotal.WithLabelValues(userID, tenantID, allowedStr).Inc()

	s.logger.Debug("Tenant access metrics recorded",
		domain.Field{Key: "user_id", Value: userID},
		domain.Field{Key: "tenant_id", Value: tenantID},
		domain.Field{Key: "allowed", Value: allowed},
	)
}

// RecordUpstreamBackend records metrics for requests to specific upstream backends.
func (s *Service) RecordUpstreamBackend(
	_ context.Context,
	backendURL,
	method,
	path,
	status string,
	duration time.Duration,
	tenants []string,
) {
	s.metrics.upstreamBackendRequestsTotal.WithLabelValues(backendURL, method, path, status).Inc()
	s.metrics.upstreamBackendRequestDuration.WithLabelValues(backendURL, method, path, status).Observe(duration.Seconds())

	s.logger.Debug("Upstream backend metrics recorded",
		domain.Field{Key: "backend_url", Value: backendURL},
		domain.Field{Key: "method", Value: method},
		domain.Field{Key: "path", Value: path},
		domain.Field{Key: "status", Value: status},
		domain.Field{Key: "duration", Value: duration},
		domain.Field{Key: "tenants", Value: tenants},
	)
}

// RecordHealthCheck records metrics for health check operations.
func (s *Service) RecordHealthCheck(_ context.Context, backendURL string, success bool, duration time.Duration) {
	result := "failure"
	if success {
		result = "success"
	}

	s.metrics.healthCheckTotal.WithLabelValues(backendURL, result).Inc()
	s.metrics.healthCheckDuration.WithLabelValues(backendURL).Observe(duration.Seconds())

	s.logger.Debug("Health check metrics recorded",
		domain.Field{Key: "backend_url", Value: backendURL},
		domain.Field{Key: "success", Value: success},
		domain.Field{Key: "duration", Value: duration},
	)
}

// RecordBackendStateChange records metrics for backend state changes.
func (s *Service) RecordBackendStateChange(_ context.Context, backendURL string, oldState, newState domain.BackendState) {
	s.metrics.backendStateChangesTotal.WithLabelValues(backendURL, oldState.String(), newState.String()).Inc()

	s.logger.Debug("Backend state change metrics recorded",
		domain.Field{Key: "backend_url", Value: backendURL},
		domain.Field{Key: "old_state", Value: oldState.String()},
		domain.Field{Key: "new_state", Value: newState.String()},
	)
}

// RecordCircuitBreakerStateChange records metrics for circuit breaker state changes.
func (s *Service) RecordCircuitBreakerStateChange(_ context.Context, backendURL string, state domain.CircuitBreakerState) {
	s.metrics.circuitBreakerStateTotal.WithLabelValues(backendURL, state.String()).Inc()

	s.logger.Debug("Circuit breaker state change metrics recorded",
		domain.Field{Key: "backend_url", Value: backendURL},
		domain.Field{Key: "state", Value: state.String()},
	)
}

// RecordQueueOperation records metrics for queue operations.
func (s *Service) RecordQueueOperation(_ context.Context, operation string, duration time.Duration, queueSize int) {
	s.metrics.queueOperationsTotal.WithLabelValues(operation).Inc()
	s.metrics.queueOperationsDuration.WithLabelValues(operation).Observe(duration.Seconds())

	s.logger.Debug("Queue operation metrics recorded",
		domain.Field{Key: "operation", Value: operation},
		domain.Field{Key: "duration", Value: duration},
		domain.Field{Key: "queue_size", Value: queueSize},
	)
}

// RecordLoadBalancerSelection records metrics for load balancer selections.
func (s *Service) RecordLoadBalancerSelection(_ context.Context, strategy domain.LoadBalancingStrategy, backendURL string, duration time.Duration) {
	s.metrics.loadBalancerSelectionsTotal.WithLabelValues(string(strategy)).Inc()
	s.metrics.loadBalancerSelectionDuration.WithLabelValues(string(strategy)).Observe(duration.Seconds())

	s.logger.Debug("Load balancer selection metrics recorded",
		domain.Field{Key: "strategy", Value: string(strategy)},
		domain.Field{Key: "backend_url", Value: backendURL},
		domain.Field{Key: "duration", Value: duration},
	)
}
