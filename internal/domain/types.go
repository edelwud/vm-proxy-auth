// Package domain contains core business types and interfaces
package domain

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// User represents an authenticated user with their permissions.
type User struct {
	ID             string     `json:"id"`
	Email          string     `json:"email,omitempty"`
	Username       string     `json:"username,omitempty"`
	Groups         []string   `json:"groups"`
	AllowedTenants []string   `json:"allowed_tenants"`
	VMTenants      []VMTenant `json:"vm_tenants,omitempty"` // VictoriaMetrics specific
	ReadOnly       bool       `json:"read_only"`
	ExpiresAt      time.Time  `json:"expires_at"`
}

// VMTenant represents a VictoriaMetrics tenant (account:project).
type VMTenant struct {
	AccountID string `json:"account_id"`
	ProjectID string `json:"project_id,omitempty"` // Optional
}

// String returns the tenant identifier for VictoriaMetrics.
func (t VMTenant) String() string {
	if t.ProjectID != "" {
		return fmt.Sprintf("%s:%s", t.AccountID, t.ProjectID)
	}

	return t.AccountID
}

// ProxyRequest represents a request to be proxied to upstream.
type ProxyRequest struct {
	User            *User
	OriginalRequest *http.Request
	FilteredQuery   string
	TargetTenant    string
}

// ProxyResponse wraps the upstream response.
type ProxyResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// RequestContext contains request-specific data.
type RequestContext struct {
	User      *User
	RequestID string
	StartTime time.Time
}

// AuthResult represents the result of authentication.
type AuthResult struct {
	User  *User
	Error error
}

// AuthService handles authentication logic.
type AuthService interface {
	Authenticate(ctx context.Context, token string) (*User, error)
}

// TenantService handles tenant-related operations.
type TenantService interface {
	FilterQuery(ctx context.Context, user *User, query string) (string, error)
	CanAccessTenant(ctx context.Context, user *User, tenantID string) bool
	DetermineTargetTenant(ctx context.Context, user *User, r *http.Request) (string, error)
}

// ProxyService handles request proxying with load balancing support.
type ProxyService interface {
	// Forward forwards a request to an upstream backend using load balancing.
	Forward(ctx context.Context, req *ProxyRequest) (*ProxyResponse, error)
	// GetBackendsStatus returns the current status of all backends for monitoring.
	GetBackendsStatus() []*BackendStatus
	// SetMaintenanceMode enables or disables maintenance mode for a specific backend.
	SetMaintenanceMode(backend string, enabled bool) error
}

// WriteService handles write operations with tenant injection.
type WriteService interface {
	ProcessWrite(
		ctx context.Context,
		data []byte,
		tenantID string,
		contentType string,
	) ([]byte, error)
}

// AccessControlService handles authorization.
type AccessControlService interface {
	CanAccess(ctx context.Context, user *User, path string, method string) error
}

// MetricsService handles metrics collection including backend-specific metrics.
type MetricsService interface {
	RecordRequest(
		ctx context.Context,
		method, path, status string,
		duration time.Duration,
		user *User,
	)
	RecordUpstream(
		ctx context.Context,
		method, path, status string,
		duration time.Duration,
		tenants []string,
	)
	RecordQueryFilter(
		ctx context.Context,
		userID string,
		tenantCount int,
		filterApplied bool,
		duration time.Duration,
	)
	RecordAuthAttempt(ctx context.Context, userID, status string)
	RecordTenantAccess(ctx context.Context, userID, tenantID string, allowed bool)

	// Backend-specific metrics.
	RecordUpstreamBackend(
		ctx context.Context,
		backendURL, method, path, status string,
		duration time.Duration,
		tenants []string,
	)
	RecordHealthCheck(
		ctx context.Context,
		backendURL string,
		healthy bool,
		duration time.Duration,
	)
	RecordBackendStateChange(
		ctx context.Context,
		backendURL string,
		fromState, toState BackendState,
	)
	RecordCircuitBreakerStateChange(
		ctx context.Context,
		backendURL string,
		state CircuitBreakerState,
	)
	RecordQueueOperation(
		ctx context.Context,
		operation string, // enqueue, dequeue, timeout, full
		duration time.Duration,
		queueSize int,
	)
	RecordLoadBalancerSelection(
		ctx context.Context,
		strategy LoadBalancingStrategy,
		backendURL string,
		duration time.Duration,
	)

	Handler() http.Handler
}

// Logger provides structured logging interface.
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	With(fields ...Field) Logger
}

// Field represents a structured log field.
type Field struct {
	Key   string
	Value any
}

// LogFormat represents the output format for logs.
type LogFormat string

const (
	// LogFormatJSON outputs logs in JSON format (default for production).
	LogFormatJSON LogFormat = "json"
	// LogFormatLogFmt outputs logs in logfmt format (structured key=value pairs).
	LogFormatLogFmt LogFormat = "logfmt"
	// LogFormatPretty outputs logs in human-readable format with colors (development).
	LogFormatPretty LogFormat = "pretty"
	// LogFormatConsole outputs logs in simple console format (minimal output).
	LogFormatConsole LogFormat = "console"
)

// LogLevel represents the logging level.
type LogLevel string

const (
	// LogLevelDebug enables debug-level logging.
	LogLevelDebug LogLevel = "debug"
	// LogLevelInfo enables info-level logging.
	LogLevelInfo LogLevel = "info"
	// LogLevelWarn enables warning-level logging.
	LogLevelWarn LogLevel = "warn"
	// LogLevelError enables error-level logging.
	LogLevelError LogLevel = "error"
)

// ContextualLogger provides contextual logging capabilities for specific components.
type ContextualLogger interface {
	Logger
	// WithComponent creates a logger with pre-configured component context.
	WithComponent(component string) Logger
	// WithRequestID creates a logger with request ID context.
	WithRequestID(requestID string) Logger
	// WithUser creates a logger with user context.
	WithUser(userID string) Logger
	// WithTenant creates a logger with tenant context.
	WithTenant(accountID, projectID string) Logger
}

// TenantFilterStrategy defines how tenant filtering should be applied to PromQL queries.
type TenantFilterStrategy string

const (
	// TenantFilterStrategyOrConditions uses separate OR conditions for each tenant pair.
	// This ensures exact tenant isolation and prevents cross-tenant data leakage.
	// Example: {vm_account_id="1000"} or {vm_account_id="2000",vm_project_id="20"}.
	TenantFilterStrategyOrConditions TenantFilterStrategy = "orConditions"
	// TenantFilterStrategyAndConditions uses AND conditions for tenant filtering.
	TenantFilterStrategyAndConditions TenantFilterStrategy = "andConditions"
)

// StateStorageType defines the type of state storage backend.
type StateStorageType string

const (
	StateStorageTypeLocal StateStorageType = "local"
	StateStorageTypeRedis StateStorageType = "redis"
	StateStorageTypeRaft  StateStorageType = "raft"
)

// JWTAlgorithm defines JWT signing algorithms.
type JWTAlgorithm string

const (
	JWTAlgorithmHS256 JWTAlgorithm = "HS256"
	JWTAlgorithmRS256 JWTAlgorithm = "RS256"
)

// DiscoveryProvider defines the interface for peer discovery implementations.
type DiscoveryProvider interface {
	// Discover finds available peer addresses
	Discover(ctx context.Context) ([]string, error)

	// Start starts the discovery provider (for active providers like mDNS server)
	Start(ctx context.Context) error

	// Stop stops the discovery provider
	Stop() error

	// GetProviderType returns the provider type name
	GetProviderType() string
}

// PeerInfo represents discovered peer information.
type PeerInfo struct {
	Address  string
	Port     int
	NodeID   string
	Metadata map[string]string
}

// TenantFilterConfig contains configuration for tenant filtering strategy.
type TenantFilterConfig struct {
	Strategy TenantFilterStrategy `yaml:"strategy" default:"or_conditions"`
}

// IsValid validates the tenant filter strategy.
func (s TenantFilterStrategy) IsValid() bool {
	return s == TenantFilterStrategyOrConditions || s == TenantFilterStrategyAndConditions
}

// TenantFilter represents a filter that can be applied to PromQL queries.
type TenantFilter interface {
	// ApplyToVectorSelector applies tenant filtering to a single vector selector.
	// Returns true if the selector was modified, false otherwise.
	ApplyToVectorSelector(vs VectorSelector, tenants []VMTenant, config TenantLabelsConfig) (bool, error)
}

// VectorSelector represents a PromQL vector selector that can be modified.
// This interface abstracts away the Prometheus parser dependency from domain layer.
type VectorSelector interface {
	// GetName returns the metric name.
	GetName() string
	// HasLabel checks if a label matcher exists.
	HasLabel(name string) bool
	// AddLabel adds a label matcher.
	AddLabel(name, value string, matchType LabelMatchType) error
}

// LabelMatchType defines the type of label matching.
type LabelMatchType int

const (
	// LabelMatchEqual represents exact equality (=).
	LabelMatchEqual LabelMatchType = iota
	// LabelMatchRegexp represents regex matching (=~).
	LabelMatchRegexp
)

// TenantLabelsConfig contains configuration for tenant label names.
type TenantLabelsConfig struct {
	TenantLabel  string
	ProjectLabel string
	UseProjectID bool
}

// Backend represents an upstream server endpoint.
type Backend struct {
	URL    string       `json:"url"`
	Weight int          `json:"weight"`
	State  BackendState `json:"state"`
}

// String returns a string representation of the backend.
func (b Backend) String() string {
	return fmt.Sprintf("Backend{URL: %s, Weight: %d, State: %s}", b.URL, b.Weight, b.State.String())
}

// BackendState represents the current state of a backend.
type BackendState int

const (
	// BackendHealthy indicates the backend is available for load balancing.
	BackendHealthy BackendState = iota
	// BackendUnhealthy indicates the backend is unavailable and excluded from load balancing.
	BackendUnhealthy
	// BackendRateLimited indicates the backend is rate-limited but may be used as fallback.
	BackendRateLimited
	// BackendMaintenance indicates the backend is manually excluded from load balancing.
	BackendMaintenance
)

// String returns the string representation of the backend state.
func (bs BackendState) String() string {
	switch bs {
	case BackendHealthy:
		return "healthy"
	case BackendUnhealthy:
		return "unhealthy"
	case BackendRateLimited:
		return "rate-limited"
	case BackendMaintenance:
		return "maintenance"
	default:
		return "unknown"
	}
}

// IsAvailable returns true if the backend is available for normal load balancing.
func (bs BackendState) IsAvailable() bool {
	return bs == BackendHealthy
}

// IsAvailableWithFallback returns true if the backend can be used including fallback scenarios.
func (bs BackendState) IsAvailableWithFallback() bool {
	return bs == BackendHealthy || bs == BackendRateLimited
}

// LoadBalancingStrategy defines the strategy for distributing requests across backends.
type LoadBalancingStrategy string

const (
	// LoadBalancingStrategyRoundRobin distributes requests in round-robin fashion.
	LoadBalancingStrategyRoundRobin LoadBalancingStrategy = "round-robin"
	// LoadBalancingStrategyWeighted distributes requests based on backend weights.
	LoadBalancingStrategyWeighted LoadBalancingStrategy = "weighted-round-robin"
	// LoadBalancingStrategyLeastConnection routes to backend with fewest active connections.
	LoadBalancingStrategyLeastConnection LoadBalancingStrategy = "least-connections"
)

// IsValid validates the load balancing strategy.
func (lbs LoadBalancingStrategy) IsValid() bool {
	switch lbs {
	case LoadBalancingStrategyRoundRobin, LoadBalancingStrategyWeighted, LoadBalancingStrategyLeastConnection:
		return true
	default:
		return false
	}
}

// BackendStatus represents the current status of a backend for monitoring.
type BackendStatus struct {
	Backend     Backend   `json:"backend"`
	IsHealthy   bool      `json:"is_healthy"`
	ActiveConns int32     `json:"active_connections"`
	LastCheck   time.Time `json:"last_check"`
	ErrorCount  int       `json:"error_count"`
}

// CircuitBreakerState represents the state of a circuit breaker.
type CircuitBreakerState int

const (
	// CircuitClosed indicates normal operation.
	CircuitClosed CircuitBreakerState = iota
	// CircuitOpen indicates the circuit is open and requests are blocked.
	CircuitOpen
	// CircuitHalfOpen indicates the circuit is testing for recovery.
	CircuitHalfOpen
)

// String returns the string representation of the circuit breaker state.
func (cbs CircuitBreakerState) String() string {
	switch cbs {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// StateEventType represents the type of state change event.
type StateEventType int

const (
	// StateEventSet indicates a key was set or updated.
	StateEventSet StateEventType = iota
	// StateEventDelete indicates a key was deleted.
	StateEventDelete
)

// String returns the string representation of the state event type.
func (set StateEventType) String() string {
	switch set {
	case StateEventSet:
		return "SET"
	case StateEventDelete:
		return "DELETE"
	default:
		return "UNKNOWN"
	}
}

// StateEvent represents a change in distributed state.
type StateEvent struct {
	Type      StateEventType `json:"type"`
	Key       string         `json:"key"`
	Value     []byte         `json:"value,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	NodeID    string         `json:"node_id"`
}

// IsValid validates the state event.
func (se StateEvent) IsValid() bool {
	return se.Key != "" && se.NodeID != "" && !se.Timestamp.IsZero()
}

// LoadBalancer manages backend selection and state.
type LoadBalancer interface {
	// NextBackend returns the next available backend according to the strategy.
	NextBackend(ctx context.Context) (*Backend, error)
	// ReportResult reports the result of a request to a backend for health tracking.
	ReportResult(backend *Backend, err error, statusCode int)
	// BackendsStatus returns the current status of all backends.
	BackendsStatus() []*BackendStatus
	// Close performs cleanup and graceful shutdown.
	Close() error
}

// HealthChecker monitors backend health.
type HealthChecker interface {
	// CheckHealth performs a health check on a specific backend.
	CheckHealth(ctx context.Context, backend *Backend) error
	// StartMonitoring begins continuous health monitoring.
	StartMonitoring(ctx context.Context) error
	// Stop stops health monitoring.
	Stop() error
}

// StateStorage provides distributed state management.
type StateStorage interface {
	// Get retrieves a value by key.
	Get(ctx context.Context, key string) ([]byte, error)
	// Set stores a value with optional TTL.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Delete removes a key.
	Delete(ctx context.Context, key string) error
	// GetMultiple retrieves multiple values efficiently.
	GetMultiple(ctx context.Context, keys []string) (map[string][]byte, error)
	// SetMultiple stores multiple values efficiently.
	SetMultiple(ctx context.Context, items map[string][]byte, ttl time.Duration) error
	// Watch observes changes to keys matching the prefix.
	Watch(ctx context.Context, keyPrefix string) (<-chan StateEvent, error)
	// Close performs cleanup and graceful shutdown.
	Close() error
	// Ping checks the health of the storage system.
	Ping(ctx context.Context) error
}

// RequestQueue manages queuing of requests for better resilience.
type RequestQueue interface {
	// Enqueue adds a request to the queue.
	Enqueue(ctx context.Context, req *ProxyRequest) error
	// Dequeue retrieves the next request from the queue.
	Dequeue(ctx context.Context) (*ProxyRequest, error)
	// Size returns the current queue size.
	Size() int
	// Close performs cleanup and graceful shutdown.
	Close() error
}

// ServiceDiscovery provides service discovery for distributed deployments.
type ServiceDiscovery interface {
	// Start begins the service discovery process.
	Start(ctx context.Context) error
	// Stop gracefully shuts down the service discovery.
	Stop() error
	// Events returns a channel for receiving discovery events.
	Events() <-chan ServiceDiscoveryEvent
	// DiscoverPeers discovers peer nodes for Raft cluster formation.
	DiscoverPeers(ctx context.Context) ([]*PeerInfo, error)
	// DiscoverBackends discovers backend services (VictoriaMetrics instances).
	DiscoverBackends(ctx context.Context) ([]*BackendInfo, error)
}

// BackendInfo represents a discovered backend service.
type BackendInfo struct {
	URL      string            `json:"url"`
	Weight   int               `json:"weight"`
	Healthy  bool              `json:"healthy"`
	LastSeen time.Time         `json:"last_seen"`
	Metadata map[string]string `json:"metadata"`
}

// NodeInfo represents information about this node.
type NodeInfo struct {
	NodeID      string            `json:"node_id"`
	Address     string            `json:"address"`
	RaftAddress string            `json:"raft_address"`
	Version     string            `json:"version"`
	StartTime   time.Time         `json:"start_time"`
	Metadata    map[string]string `json:"metadata"`
}

// ServiceDiscoveryEvent represents a change in service topology.
type ServiceDiscoveryEvent struct {
	Type      ServiceDiscoveryEventType `json:"type"`
	Peer      *PeerInfo                 `json:"peer,omitempty"`
	Backend   *BackendInfo              `json:"backend,omitempty"`
	Timestamp time.Time                 `json:"timestamp"`
}

// ServiceDiscoveryEventType represents the type of service discovery event.
type ServiceDiscoveryEventType string

const (
	ServiceDiscoveryEventTypePeerJoined     ServiceDiscoveryEventType = "peer_joined"
	ServiceDiscoveryEventTypePeerLeft       ServiceDiscoveryEventType = "peer_left"
	ServiceDiscoveryEventTypePeerUpdated    ServiceDiscoveryEventType = "peer_updated"
	ServiceDiscoveryEventTypeBackendAdded   ServiceDiscoveryEventType = "backend_added"
	ServiceDiscoveryEventTypeBackendRemoved ServiceDiscoveryEventType = "backend_removed"
	ServiceDiscoveryEventTypeBackendUpdated ServiceDiscoveryEventType = "backend_updated"
)

// HealthStatus represents the health status of a component.
type HealthStatus string

const (
	// HealthStatusHealthy indicates the component is functioning normally.
	HealthStatusHealthy HealthStatus = "healthy"
	// HealthStatusDegraded indicates the component has degraded performance.
	HealthStatusDegraded HealthStatus = "degraded"
)

// RaftManager defines the interface for managing Raft cluster membership.
type RaftManager interface {
	AddVoter(nodeID, address string) error
	RemoveServer(nodeID string) error
	GetLeader() (string, string)
	GetPeers() ([]string, error)
	IsLeader() bool
	TryDelayedBootstrap(discoveredPeers []string) error // Consul pattern support
	ForceRecoverCluster(nodeID, address string) error   // Emergency recovery for split-brain
}

// MemberlistDiscoveryService defines interface for auto-discovery integration.
type MemberlistDiscoveryService interface {
	SetPeerJoiner(joiner interface{})
	Start(ctx context.Context) error
	Stop() error
}

// PeerJoiner defines interface for joining discovered peers.
type PeerJoiner interface {
	Join(peers []string) error
	GetLocalNode() interface{} // Using interface{} to avoid importing memberlist
	GetMembers() interface{}   // Using interface{} to avoid importing memberlist
}

// PeerDiscovery defines interface for discovering cluster peers before bootstrap.
type PeerDiscovery interface {
	GetDiscoveredPeers() []string
	HasExistingRaftPeers() (bool, error)
}

// ContextKey defines custom type for context keys to avoid collisions.
type ContextKey string

const (
	// RequestIDKey is the context key for request ID.
	RequestIDKey ContextKey = "request_id"
)
