package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "prometheus_oauth_gateway_requests_total",
			Help: "Total number of requests processed by the gateway",
		},
		[]string{"method", "path", "status_code", "tenant"},
	)

	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "prometheus_oauth_gateway_request_duration_seconds",
			Help:    "Request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "tenant"},
	)

	AuthenticationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "prometheus_oauth_gateway_authentication_total",
			Help: "Total number of authentication attempts",
		},
		[]string{"result", "user_id"},
	)

	AuthenticationDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "prometheus_oauth_gateway_authentication_duration_seconds",
			Help:    "Authentication duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
		},
	)

	UpstreamRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "prometheus_oauth_gateway_upstream_requests_total",
			Help: "Total number of requests forwarded to upstream",
		},
		[]string{"method", "path", "status_code", "tenant"},
	)

	UpstreamRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "prometheus_oauth_gateway_upstream_request_duration_seconds",
			Help:    "Upstream request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "tenant"},
	)

	QueryFiltersApplied = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "prometheus_oauth_gateway_query_filters_applied_total",
			Help: "Total number of query filters applied",
		},
		[]string{"user_id", "tenant"},
	)

	TenantAccessDenied = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "prometheus_oauth_gateway_tenant_access_denied_total",
			Help: "Total number of tenant access denied events",
		},
		[]string{"user_id", "requested_tenant"},
	)

	ActiveConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "prometheus_oauth_gateway_active_connections",
			Help: "Number of active connections",
		},
	)

	TokenCacheHits = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "prometheus_oauth_gateway_token_cache_hits_total",
			Help: "Total number of token cache hits",
		},
	)

	TokenCacheMisses = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "prometheus_oauth_gateway_token_cache_misses_total",
			Help: "Total number of token cache misses",
		},
	)

	JWKSFetchTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "prometheus_oauth_gateway_jwks_fetch_total",
			Help: "Total number of JWKS fetch attempts",
		},
		[]string{"result"},
	)

	JWKSFetchDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "prometheus_oauth_gateway_jwks_fetch_duration_seconds",
			Help:    "JWKS fetch duration in seconds",
			Buckets: []float64{0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0},
		},
	)
)

func RecordRequest(method, path, statusCode, tenant string, duration time.Duration) {
	RequestsTotal.WithLabelValues(method, path, statusCode, tenant).Inc()
	RequestDuration.WithLabelValues(method, path, tenant).Observe(duration.Seconds())
}

func RecordAuthentication(result, userID string, duration time.Duration) {
	AuthenticationTotal.WithLabelValues(result, userID).Inc()
	AuthenticationDuration.Observe(duration.Seconds())
}

func RecordUpstreamRequest(method, path, statusCode, tenant string, duration time.Duration) {
	UpstreamRequestsTotal.WithLabelValues(method, path, statusCode, tenant).Inc()
	UpstreamRequestDuration.WithLabelValues(method, path, tenant).Observe(duration.Seconds())
}

func RecordQueryFilter(userID, tenant string) {
	QueryFiltersApplied.WithLabelValues(userID, tenant).Inc()
}

func RecordTenantAccessDenied(userID, requestedTenant string) {
	TenantAccessDenied.WithLabelValues(userID, requestedTenant).Inc()
}

func RecordActiveConnection() {
	ActiveConnections.Inc()
}

func RecordConnectionClosed() {
	ActiveConnections.Dec()
}

func RecordTokenCacheHit() {
	TokenCacheHits.Inc()
}

func RecordTokenCacheMiss() {
	TokenCacheMisses.Inc()
}

func RecordJWKSFetch(result string, duration time.Duration) {
	JWKSFetchTotal.WithLabelValues(result).Inc()
	JWKSFetchDuration.Observe(duration.Seconds())
}