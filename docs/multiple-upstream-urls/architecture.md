# Architecture: Multiple Upstream URLs

## Overview

The Multiple Upstream URLs feature implements load balancing across multiple VictoriaMetrics instances with health checking, request queuing, and distributed state management. This design follows the existing domain-driven architecture principles and maintains clean separation of concerns.

## System Components

### Domain Layer (`internal/domain/`)

#### New Types and Interfaces

```go
// Backend represents an upstream server endpoint
type Backend struct {
    URL    string
    Weight int
    State  BackendState
}

// BackendState represents the current state of a backend
type BackendState int

const (
    BackendHealthy BackendState = iota
    BackendUnhealthy
    BackendRateLimited
    BackendMaintenance
)

// LoadBalancer manages backend selection and state
type LoadBalancer interface {
    NextBackend(ctx context.Context) (*Backend, error)
    ReportResult(backend *Backend, err error, statusCode int)
    BackendsStatus() []*BackendStatus
    Close() error
}

// HealthChecker monitors backend health
type HealthChecker interface {
    CheckHealth(ctx context.Context, backend *Backend) error
    StartMonitoring(ctx context.Context) error
    Stop() error
}

// StateStorage provides distributed state management
type StateStorage interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    Watch(ctx context.Context, keyPrefix string) (<-chan StateEvent, error)
    Close() error
}

// RequestQueue manages request queuing for resilience
type RequestQueue interface {
    Enqueue(ctx context.Context, req *ProxyRequest) error
    Dequeue(ctx context.Context) (*ProxyRequest, error)
    Size() int
    Close() error
}
```

#### Enhanced ProxyService Interface

```go
type ProxyService interface {
    Forward(ctx context.Context, req *ProxyRequest) (*ProxyResponse, error)
    // New methods for multiple upstreams
    GetBackendsStatus() []*BackendStatus
    SetMaintenanceMode(backend string, enabled bool) error
}
```

### Services Layer (`internal/services/`)

#### Load Balancer Service (`internal/services/loadbalancer/`)

**Strategies:**
- `RoundRobinBalancer` - Simple round-robin selection
- `WeightedRoundRobinBalancer` - Weighted distribution based on backend weights
- `LeastConnectionsBalancer` - Routes to backend with fewest active connections

**Key Features:**
- Thread-safe backend selection using `sync.RWMutex`
- Connection tracking for least-connections strategy
- Circuit breaker pattern integration
- Comprehensive metrics collection

#### Health Checker Service (`internal/services/healthchecker/`)

**Responsibilities:**
- Periodic health checks using `/-/ready` endpoint
- Circuit breaker state management (CLOSED → OPEN → HALF_OPEN)
- Backend state transitions based on configurable thresholds
- Integration with distributed state storage

**Health Check Flow:**
1. Send GET request to `backend_url/-/ready`
2. Evaluate response (2xx = healthy, 5xx = unhealthy, 429 = rate limited)
3. Update backend state based on thresholds
4. Publish state changes to distributed storage

#### Queue Service (`internal/services/queue/`)

**Implementation:**
- Bounded channel-based queue with configurable size
- Worker pool for concurrent request processing
- Request timeout handling to prevent memory leaks
- Graceful shutdown with request draining

#### State Storage Service (`internal/services/statestorage/`)

**Implementations:**
- `LocalStorage` - In-memory storage for single-instance deployments
- `RedisStorage` - Redis-based storage for distributed deployments
- `RaftStorage` - Raft consensus for dependency-free distributed storage

### Infrastructure Layer (`internal/infrastructure/`)

#### Configuration Extensions

Enhanced `UpstreamSettings` in `internal/config/`:

```yaml
# Top-level state storage configuration
stateStorage:
  type: "local"  # local, redis, raft
  redis:
    address: "localhost:6379"
    keyPrefix: "vm-proxy-auth:backends:"
  raft:
    nodeId: "node-1" 
    peers: ["node-1:8081", "node-2:8081"]
    dataDir: "/var/lib/vm-proxy-auth/raft"

# Request queue configuration
requestQueue:
  enabled: true
  maxSize: 1000
  timeout: "30s"
  workers: 10

# Enhanced upstream configuration
upstream:
  backends:
    - url: "https://vmselect-1.example.com"
      weight: 1
    - url: "https://vmselect-2.example.com"
      weight: 2
  strategy: "weighted-round-robin"  # round-robin, weighted-round-robin, least-connections
  healthCheck:
    enabled: true
    endpoint: "/-/ready"
    interval: "30s"
    timeout: "5s"
    strategy: "circuit-breaker"  # exclude-unhealthy, circuit-breaker
    thresholds:
      failure: 3
      success: 1
    circuitBreaker:
      openTimeout: "60s"
      halfOpenMaxRequests: 5
      minRequestsToTrip: 20
  retry:
    maxRetries: 3
    retryDelay: "1s"
    backoffMultiplier: 1.5
    retryableErrors:
      - "connection_timeout"
      - "connection_refused"
      - "5xx_status"
```

## Request Processing Flow

### 1. Request Arrival
```
HTTP Request → Gateway Handler → Auth Service → Tenant Service
```

### 2. Load Balancing
```
Proxy Service → Request Queue → Load Balancer → Backend Selection
```

### 3. Health-aware Routing
```
Load Balancer → Health Checker → Circuit Breaker → Backend State Check
```

### 4. Request Execution
```
Selected Backend → HTTP Client → Upstream Request → Response Processing
```

### 5. Error Handling & Retry
```
Error Classification → Retry Logic → Alternative Backend Selection → State Updates
```

## Concurrency and Thread Safety

### Backend State Management
- `sync.RWMutex` for read-heavy backend selection operations
- Atomic counters for connection tracking in least-connections strategy
- Channel-based coordination for state updates

### Request Queue
- Buffered channels with configurable capacity
- Worker goroutines for concurrent request processing
- Context-based cancellation for timeout handling

### Health Checking
- Separate goroutine per backend for independent monitoring
- Timer-based scheduling with configurable intervals
- State synchronization through distributed storage

## Error Handling Strategy

### Error Classification
```go
type ErrorType int

const (
    NetworkError ErrorType = iota    // Retryable on different backend
    PromQLError                      // Not retryable, return to client
    RateLimitError                   // Mark backend as rate-limited
    TimeoutError                     // Retryable with backoff
)
```

### Retry Logic
1. Classify error type
2. If retryable, attempt on different backend
3. Apply exponential backoff with jitter
4. Update backend state based on error patterns
5. Record metrics for observability

### Fallback Behavior
- All backends unhealthy: Return 503 with detailed error JSON
- Queue full: Return 503 with queue status information
- State storage unavailable: Continue with local state, log warnings

## Metrics and Observability

### Backend-Specific Metrics
```
vm_proxy_auth_upstream_backend_requests_total{backend, method, status}
vm_proxy_auth_upstream_backend_duration_seconds{backend}
vm_proxy_auth_upstream_backend_health_status{backend, state}
vm_proxy_auth_upstream_backend_active_connections{backend}
vm_proxy_auth_circuit_breaker_state{backend, state}
```

### Queue Metrics
```
vm_proxy_auth_queue_size
vm_proxy_auth_queue_wait_duration_seconds
vm_proxy_auth_queue_full_errors_total
```

### Load Balancer Metrics
```
vm_proxy_auth_backend_selection_total{strategy, backend}
vm_proxy_auth_backend_state_transitions_total{backend, from_state, to_state}
```

## Security Considerations

### State Storage Security
- Redis connections with authentication
- Raft cluster with TLS encryption
- Key prefixing to avoid conflicts

### Backend Authentication  
- Preserve original authentication headers
- Support for backend-specific credentials (future enhancement)
- TLS verification for upstream connections

## Scalability Considerations

### Horizontal Scaling
- Distributed state storage enables multiple proxy instances
- Load balancer state synchronized across instances
- Health check coordination to avoid thundering herd

### Vertical Scaling
- Configurable worker pool sizes
- Bounded queues to prevent memory exhaustion
- Connection pooling with appropriate limits

## Integration Points

### Existing Services
- **Auth Service**: No changes required, continues to validate JWT tokens
- **Tenant Service**: No changes required, continues to filter queries
- **Metrics Service**: Enhanced with new backend-specific metrics
- **Proxy Service**: Completely refactored to use LoadBalancer

### External Dependencies
- VictoriaMetrics instances (upstream backends)
- Redis (optional, for distributed state)
- Monitoring systems (Prometheus, Grafana)

## Migration Strategy

### Phase 1: Backward Compatibility
- Support existing single `url` configuration during transition
- Gradual migration tooling for configuration conversion
- Feature flags for enabling new load balancing features

### Phase 2: Full Migration
- Deprecate single URL configuration
- Comprehensive testing with production workloads
- Rollback procedures in case of issues

### Phase 3: Advanced Features
- Dynamic backend discovery
- Advanced health checking strategies
- Multi-region load balancing