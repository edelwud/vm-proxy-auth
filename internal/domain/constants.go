package domain

import "time"

// Default timeout and duration constants.
const (
	// HTTP timeouts.
	DefaultReadTimeout  = 30 * time.Second
	DefaultWriteTimeout = 30 * time.Second
	DefaultIdleTimeout  = 60 * time.Second
	DefaultTimeout      = 30 * time.Second

	// Cache settings.
	DefaultCacheTTL = 5 * time.Minute
	DefaultTokenTTL = time.Hour

	// Retry settings.
	DefaultMaxRetries     = 3
	DefaultRetryDelay     = time.Second
	DefaultRetryBackoffMs = 100

	// Shutdown timeout.
	DefaultShutdownTimeout = 30 * time.Second

	// Proxy service defaults.
	DefaultProxyTimeoutSeconds = 30
	DefaultProxyMaxRetries     = 3
	DefaultQueueCheckInterval  = 100 * time.Millisecond

	// Load balancer defaults.
	DefaultBackendWeight = 1

	// State storage defaults.
	DefaultWatchChannelBufferSize = 100

	// Raft storage defaults.
	DefaultRaftMaxConnections   = 3
	DefaultRaftApplyTimeout     = 10 * time.Second
	DefaultRaftApplyTimeoutBulk = 30 * time.Second
	DefaultRaftWatchChannelSize = 100
	DefaultRaftMinPeerParts     = 2
	DefaultLeaderLeaseTimeout   = 500 * time.Millisecond
	DefaultCommitTimeout        = 50 * time.Millisecond
	DefaultSnapshotRetention    = 3
	DefaultSnapshotThreshold    = 1024
	DefaultTrailingLogs         = 1024

	// Redis storage defaults.
	DefaultRedisConnectTimeout  = 5 * time.Second
	DefaultRedisReadTimeout     = 3 * time.Second
	DefaultRedisWriteTimeout    = 3 * time.Second
	DefaultRedisPoolSize        = 10
	DefaultRedisMinIdleConns    = 5
	DefaultRedisMaxRetries      = 3
	DefaultRedisDatabase        = 0
	DefaultRedisMinRetryBackoff = 100 * time.Millisecond
	DefaultRedisMaxRetryBackoff = 1000 * time.Millisecond
	DefaultRedisKeyPrefix       = "vm-proxy-auth:"
	DefaultRedisPubSubChannel   = "vm-proxy-auth:events"
	DefaultRedisConnMaxIdleTime = 30 * time.Minute
	DefaultRedisReceiveTimeout  = 100 * time.Millisecond

	// DNS discovery defaults.
	DefaultDNSUpdateInterval   = 30 * time.Second
	DefaultDNSPort             = 8080
	DefaultDNSRaftPort         = 9000
	DefaultDNSTTL              = 2 * time.Minute
	DefaultDNSEventChannelSize = 100
	DefaultDNSSRVService       = "vm-proxy-auth"
	DefaultDNSSRVProtocol      = "tcp"
	DefaultDNSBackendService   = "vm-backend"

	// Kubernetes discovery defaults.
	DefaultK8sWatchTimeout = 10 * time.Minute

	// mDNS discovery defaults.
	DefaultMDNSServiceType      = "_vm-proxy-auth._tcp"
	DefaultMDNSServiceDomain    = "local."
	DefaultMDNSUpdateInterval   = 15 * time.Second
	DefaultMDNSEventChannelSize = 100
	DefaultMDNSQueryTimeout     = 5 * time.Second
	DefaultMDNSEntryChannelSize = 32

	// Queue defaults.
	DefaultQueuePercentageMultiplier        = 100
	DefaultQueueHealthyUtilizationThreshold = 90
	DefaultQueueMaxSize                     = 1000
	DefaultQueueTimeoutSeconds              = 5

	// Health check defaults.
	DefaultHealthyThreshold   = 2
	DefaultUnhealthyThreshold = 3

	// Test loop counts.
	DefaultTestRetries = 5
	DefaultTestCount   = 3
	DefaultBenchCount  = 10

	// HTTP status codes.
	StatusInternalServerError = 500
	StatusBadRequest          = 400

	// Health status constants.
	HealthStatusDegraded = "degraded"
	HealthStatusHealthy  = "healthy"

	// Log parsing constants.
	DefaultFieldsPerKeyValue      = 2
	DefaultComponentNameMaxLength = 20
	DefaultColonSeparatorOffset   = 2

	// Commonly used log field keys.
	LogFieldComponent = "component"
	LogFieldError     = "error"
	LogFieldUserID    = "user_id"
	LogFieldRequestID = "request_id"
	LogFieldTenantID  = "tenant_id"
)
