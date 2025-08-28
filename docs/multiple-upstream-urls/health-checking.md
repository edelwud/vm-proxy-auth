# Health Checking and Circuit Breaker

## Overview

This document describes the health checking system and circuit breaker implementation for monitoring and managing the health of upstream VictoriaMetrics instances. The system ensures high availability by automatically detecting and routing around failed or degraded backends.

## Health Checking Architecture

### Health Check Process

The health checking system continuously monitors backend health through periodic HTTP requests to a designated health endpoint.

**Health Check Flow:**
1. Send GET request to `{backend_url}/{health_endpoint}`
2. Evaluate response status and latency
3. Update backend state based on configurable thresholds
4. Trigger circuit breaker state transitions
5. Synchronize state across distributed instances

### Backend States

```go
type BackendState int

const (
    BackendHealthy      BackendState = iota // Available for load balancing
    BackendUnhealthy                        // Excluded from load balancing
    BackendRateLimited                      // Temporary exclusion due to 429 responses
    BackendMaintenance                      // Manually excluded from load balancing
)

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

func (bs BackendState) IsAvailable() bool {
    return bs == BackendHealthy
}

func (bs BackendState) IsAvailableWithFallback() bool {
    return bs == BackendHealthy || bs == BackendRateLimited
}
```

### Health Check Implementation

```go
type HealthChecker struct {
    config        HealthCheckConfig
    backends      map[string]*BackendHealth
    stateStorage  StateStorage
    logger        domain.Logger
    metrics       HealthCheckMetrics
    stopCh        chan struct{}
    wg            sync.WaitGroup
    mu            sync.RWMutex
}

type BackendHealth struct {
    Backend         Backend
    State           BackendState
    LastCheck       time.Time
    LastHealthy     time.Time
    ConsecutiveFails int
    ConsecutiveOKs   int
    CircuitBreaker  *CircuitBreaker
    CheckInProgress atomic.Bool
}

func (hc *HealthChecker) StartMonitoring(ctx context.Context) error {
    hc.logger.Info("Starting health checker",
        Field{Key: "interval", Value: hc.config.Interval},
        Field{Key: "strategy", Value: hc.config.Strategy})
    
    for _, backend := range hc.backends {
        hc.wg.Add(1)
        go hc.monitorBackend(ctx, backend)
    }
    
    return nil
}

func (hc *HealthChecker) monitorBackend(ctx context.Context, bh *BackendHealth) {
    defer hc.wg.Done()
    
    ticker := time.NewTicker(hc.config.Interval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-hc.stopCh:
            return
        case <-ticker.C:
            hc.checkBackendHealth(ctx, bh)
        }
    }
}
```

## Health Check Strategies

### 1. Exclude Unhealthy Strategy

**Behavior:** Simply removes unhealthy backends from load balancing rotation.

```go
type ExcludeUnhealthyStrategy struct {
    thresholds HealthThresholds
}

func (eus *ExcludeUnhealthyStrategy) UpdateState(bh *BackendHealth, healthy bool) BackendState {
    if healthy {
        bh.ConsecutiveOKs++
        bh.ConsecutiveFails = 0
        
        if bh.ConsecutiveOKs >= eus.thresholds.Success {
            return BackendHealthy
        }
    } else {
        bh.ConsecutiveFails++
        bh.ConsecutiveOKs = 0
        
        if bh.ConsecutiveFails >= eus.thresholds.Failure {
            return BackendUnhealthy
        }
    }
    
    return bh.State // No state change
}
```

**Configuration:**
```yaml
upstream:
  healthCheck:
    strategy: "exclude-unhealthy"
    thresholds:
      failure: 3    # Mark unhealthy after 3 consecutive failures
      success: 1    # Mark healthy after 1 successful check
```

**Use Cases:**
- Simple deployments
- Predictable failure patterns
- Quick recovery scenarios

### 2. Circuit Breaker Strategy

**Behavior:** Implements circuit breaker pattern with CLOSED/OPEN/HALF_OPEN states.

```go
type CircuitBreakerStrategy struct {
    thresholds HealthThresholds
    config     CircuitBreakerConfig
}

type CircuitBreakerState int

const (
    CircuitClosed   CircuitBreakerState = iota // Normal operation
    CircuitOpen                                // Failing, requests blocked
    CircuitHalfOpen                            // Testing recovery
)

type CircuitBreaker struct {
    state              CircuitBreakerState
    lastStateChange    time.Time
    consecutiveFails   int32
    consecutiveSuccess int32
    requestsInHalfOpen int32
    mu                sync.RWMutex
}

func (cb *CircuitBreaker) CanExecute() bool {
    cb.mu.RLock()
    defer cb.mu.RUnlock()
    
    switch cb.state {
    case CircuitClosed:
        return true
    case CircuitOpen:
        // Check if we should transition to half-open
        if time.Since(cb.lastStateChange) > cb.config.OpenTimeout {
            cb.transitionToHalfOpen()
            return true
        }
        return false
    case CircuitHalfOpen:
        // Allow limited requests in half-open state
        return atomic.LoadInt32(&cb.requestsInHalfOpen) < int32(cb.config.HalfOpenMaxRequests)
    default:
        return false
    }
}

func (cb *CircuitBreaker) RecordResult(success bool) {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    
    switch cb.state {
    case CircuitClosed:
        if success {
            atomic.StoreInt32(&cb.consecutiveFails, 0)
        } else {
            fails := atomic.AddInt32(&cb.consecutiveFails, 1)
            if fails >= int32(cb.config.MinRequestsToTrip) {
                cb.transitionToOpen()
            }
        }
        
    case CircuitHalfOpen:
        atomic.AddInt32(&cb.requestsInHalfOpen, -1)
        
        if success {
            successes := atomic.AddInt32(&cb.consecutiveSuccess, 1)
            if successes >= int32(cb.config.SuccessThreshold) {
                cb.transitionToClosed()
            }
        } else {
            cb.transitionToOpen()
        }
    }
}
```

**Configuration:**
```yaml
upstream:
  healthCheck:
    strategy: "circuit-breaker"
    thresholds:
      failure: 3
      success: 2
    circuitBreaker:
      openTimeout: "60s"              # Time to keep circuit open
      halfOpenMaxRequests: 5          # Max requests in half-open state
      minRequestsToTrip: 20           # Min requests before evaluating
      successThreshold: 3             # Successes needed to close circuit
```

**State Transitions:**
```
CLOSED → OPEN: After threshold consecutive failures
OPEN → HALF_OPEN: After openTimeout period
HALF_OPEN → CLOSED: After successThreshold successes
HALF_OPEN → OPEN: On any failure
```

## Health Check Endpoint Integration

### VictoriaMetrics Health Endpoints

```go
type HealthEndpoint struct {
    Path           string
    ExpectedStatus []int
    Timeout        time.Duration
    Headers        map[string]string
}

var DefaultVictoriaMetricsEndpoints = map[string]HealthEndpoint{
    "ready": {
        Path:           "/-/ready",
        ExpectedStatus: []int{200},
        Timeout:        5 * time.Second,
    },
    "healthy": {
        Path:           "/-/healthy",
        ExpectedStatus: []int{200},
        Timeout:        3 * time.Second,
    },
    "custom": {
        Path:           "/health",
        ExpectedStatus: []int{200, 204},
        Timeout:        5 * time.Second,
        Headers:        map[string]string{"User-Agent": "vm-proxy-auth-health-check"},
    },
}
```

### Health Check Request Implementation

```go
func (hc *HealthChecker) performHealthCheck(ctx context.Context, backend Backend) (bool, time.Duration, error) {
    start := time.Now()
    
    // Build health check URL
    healthURL := strings.TrimRight(backend.URL, "/") + "/" + strings.TrimLeft(hc.config.Endpoint, "/")
    
    // Create request with timeout
    checkCtx, cancel := context.WithTimeout(ctx, hc.config.Timeout)
    defer cancel()
    
    req, err := http.NewRequestWithContext(checkCtx, "GET", healthURL, nil)
    if err != nil {
        return false, time.Since(start), fmt.Errorf("failed to create request: %w", err)
    }
    
    // Add custom headers
    req.Header.Set("User-Agent", "vm-proxy-auth/health-checker")
    req.Header.Set("Accept", "application/json")
    
    // Execute request
    client := &http.Client{
        Timeout: hc.config.Timeout,
        Transport: &http.Transport{
            DisableKeepAlives:   true,
            MaxIdleConnsPerHost: 1,
        },
    }
    
    resp, err := client.Do(req)
    duration := time.Since(start)
    
    if err != nil {
        return false, duration, err
    }
    defer resp.Body.Close()
    
    // Evaluate response
    healthy := hc.evaluateResponse(resp, backend)
    
    hc.logger.Debug("Health check completed",
        Field{Key: "backend", Value: backend.URL},
        Field{Key: "status_code", Value: resp.StatusCode},
        Field{Key: "duration_ms", Value: duration.Milliseconds()},
        Field{Key: "healthy", Value: healthy})
    
    return healthy, duration, nil
}

func (hc *HealthChecker) evaluateResponse(resp *http.Response, backend Backend) bool {
    switch {
    case resp.StatusCode >= 200 && resp.StatusCode < 300:
        return true
    case resp.StatusCode == 429:
        // Rate limited - handle specially
        hc.handleRateLimit(backend)
        return false
    case resp.StatusCode >= 500:
        return false
    default:
        // 4xx errors (except 429) might indicate misconfiguration
        hc.logger.Warn("Unexpected health check response",
            Field{Key: "backend", Value: backend.URL},
            Field{Key: "status_code", Value: resp.StatusCode})
        return false
    }
}
```

## Rate Limiting Handling

### Rate Limit Detection and Management

```go
func (hc *HealthChecker) handleRateLimit(backend Backend) {
    hc.mu.Lock()
    defer hc.mu.Unlock()
    
    if bh, exists := hc.backends[backend.URL]; exists {
        bh.State = BackendRateLimited
        bh.LastRateLimitTime = time.Now()
        
        // Exponential backoff for rate limited backends
        backoffDuration := hc.calculateRateLimitBackoff(bh.RateLimitCount)
        bh.NextCheckTime = time.Now().Add(backoffDuration)
        bh.RateLimitCount++
        
        hc.logger.Warn("Backend rate limited",
            Field{Key: "backend", Value: backend.URL},
            Field{Key: "backoff_duration", Value: backoffDuration},
            Field{Key: "rate_limit_count", Value: bh.RateLimitCount})
        
        hc.metrics.RecordRateLimit(backend.URL)
    }
}

func (hc *HealthChecker) calculateRateLimitBackoff(count int) time.Duration {
    base := time.Duration(30) * time.Second
    backoff := base * time.Duration(math.Pow(2, float64(count)))
    
    // Cap at maximum backoff
    maxBackoff := 5 * time.Minute
    if backoff > maxBackoff {
        backoff = maxBackoff
    }
    
    // Add jitter to avoid thundering herd
    jitter := time.Duration(rand.Int63n(int64(backoff) / 4))
    return backoff + jitter
}
```

### Rate Limited Backend Recovery

```go
func (hc *HealthChecker) checkRateLimitedRecovery(bh *BackendHealth) bool {
    if bh.State != BackendRateLimited {
        return false
    }
    
    // Don't check too frequently when rate limited
    if time.Now().Before(bh.NextCheckTime) {
        return false
    }
    
    // Perform gentle health check
    healthy, duration, err := hc.performHealthCheck(context.Background(), bh.Backend)
    
    if err == nil && healthy {
        bh.State = BackendHealthy
        bh.RateLimitCount = 0
        bh.LastHealthy = time.Now()
        
        hc.logger.Info("Backend recovered from rate limiting",
            Field{Key: "backend", Value: bh.Backend.URL},
            Field{Key: "duration_ms", Value: duration.Milliseconds()})
        
        hc.metrics.RecordRecovery(bh.Backend.URL, "rate_limit")
        return true
    }
    
    return false
}
```

## Distributed State Synchronization

### State Storage Integration

```go
func (hc *HealthChecker) syncStateToStorage(bh *BackendHealth) error {
    if hc.stateStorage == nil {
        return nil // Local-only mode
    }
    
    stateData := BackendHealthState{
        URL:              bh.Backend.URL,
        State:            bh.State,
        LastCheck:        bh.LastCheck,
        LastHealthy:      bh.LastHealthy,
        ConsecutiveFails: bh.ConsecutiveFails,
        UpdatedBy:        hc.nodeID,
        UpdatedAt:        time.Now(),
    }
    
    data, err := json.Marshal(stateData)
    if err != nil {
        return err
    }
    
    key := fmt.Sprintf("backend:health:%s", bh.Backend.URL)
    return hc.stateStorage.Set(context.Background(), key, data, 2*hc.config.Interval)
}

func (hc *HealthChecker) syncStateFromStorage() error {
    if hc.stateStorage == nil {
        return nil
    }
    
    // Watch for state changes from other instances
    events, err := hc.stateStorage.Watch(context.Background(), "backend:health:")
    if err != nil {
        return err
    }
    
    go hc.handleStateEvents(events)
    return nil
}

func (hc *HealthChecker) handleStateEvents(events <-chan StateEvent) {
    for event := range events {
        var stateData BackendHealthState
        if err := json.Unmarshal(event.Value, &stateData); err != nil {
            hc.logger.Error("Failed to unmarshal backend state", 
                Field{Key: "error", Value: err})
            continue
        }
        
        // Don't process our own updates
        if stateData.UpdatedBy == hc.nodeID {
            continue
        }
        
        hc.updateBackendFromRemoteState(stateData)
    }
}
```

### Conflict Resolution

```go
func (hc *HealthChecker) resolveStateConflict(local, remote BackendHealthState) BackendHealthState {
    // Use most recent health information
    if remote.UpdatedAt.After(local.UpdatedAt) {
        // Remote state is newer
        if remote.State == BackendHealthy || 
           (local.State == BackendUnhealthy && remote.State == BackendRateLimited) {
            return remote
        }
    }
    
    // Prefer unhealthy state for safety
    if local.State == BackendUnhealthy || remote.State == BackendUnhealthy {
        if local.State == BackendUnhealthy {
            return local
        }
        return remote
    }
    
    // Default to local state
    return local
}
```

## Metrics and Monitoring

### Health Check Metrics

```go
type HealthCheckMetrics struct {
    ChecksTotal         *prometheus.CounterVec   // Total checks performed
    CheckDuration       *prometheus.HistogramVec // Check duration
    BackendState        *prometheus.GaugeVec     // Current backend state
    StateTransitions    *prometheus.CounterVec   // State change events
    CircuitBreakerState *prometheus.GaugeVec     // Circuit breaker states
    RateLimitEvents     *prometheus.CounterVec   // Rate limit occurrences
}

func NewHealthCheckMetrics() *HealthCheckMetrics {
    return &HealthCheckMetrics{
        ChecksTotal: prometheus.NewCounterVec(
            prometheus.CounterOpts{
                Name: "vm_proxy_auth_health_checks_total",
                Help: "Total number of health checks performed",
            },
            []string{"backend", "result"},
        ),
        CheckDuration: prometheus.NewHistogramVec(
            prometheus.HistogramOpts{
                Name:    "vm_proxy_auth_health_check_duration_seconds",
                Help:    "Duration of health check requests",
                Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
            },
            []string{"backend"},
        ),
        BackendState: prometheus.NewGaugeVec(
            prometheus.GaugeOpts{
                Name: "vm_proxy_auth_backend_state",
                Help: "Current state of backend (0=unhealthy, 1=healthy, 2=rate-limited, 3=maintenance)",
            },
            []string{"backend"},
        ),
        StateTransitions: prometheus.NewCounterVec(
            prometheus.CounterOpts{
                Name: "vm_proxy_auth_backend_state_transitions_total",
                Help: "Total number of backend state transitions",
            },
            []string{"backend", "from_state", "to_state"},
        ),
        CircuitBreakerState: prometheus.NewGaugeVec(
            prometheus.GaugeOpts{
                Name: "vm_proxy_auth_circuit_breaker_state",
                Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
            },
            []string{"backend"},
        ),
    }
}
```

### Health Dashboard Integration

```json
{
  "dashboard": {
    "title": "VM Proxy Auth - Backend Health",
    "panels": [
      {
        "title": "Backend Health Status",
        "type": "stat",
        "targets": [
          {
            "expr": "vm_proxy_auth_backend_state",
            "legendFormat": "{{backend}}"
          }
        ]
      },
      {
        "title": "Health Check Success Rate",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(vm_proxy_auth_health_checks_total{result=\"success\"}[5m]) / rate(vm_proxy_auth_health_checks_total[5m])",
            "legendFormat": "{{backend}}"
          }
        ]
      },
      {
        "title": "Circuit Breaker States",
        "type": "graph",
        "targets": [
          {
            "expr": "vm_proxy_auth_circuit_breaker_state",
            "legendFormat": "{{backend}}"
          }
        ]
      }
    ]
  }
}
```

## Testing Strategies

### Unit Testing

```go
func TestCircuitBreaker_StateTransitions(t *testing.T) {
    cb := NewCircuitBreaker(CircuitBreakerConfig{
        MinRequestsToTrip:     3,
        OpenTimeout:          time.Second,
        HalfOpenMaxRequests:  2,
        SuccessThreshold:     2,
    })
    
    // Test CLOSED → OPEN transition
    assert.Equal(t, CircuitClosed, cb.GetState())
    
    for i := 0; i < 3; i++ {
        cb.RecordResult(false)
    }
    assert.Equal(t, CircuitOpen, cb.GetState())
    
    // Test OPEN → HALF_OPEN after timeout
    time.Sleep(1100 * time.Millisecond)
    assert.True(t, cb.CanExecute())
    assert.Equal(t, CircuitHalfOpen, cb.GetState())
    
    // Test HALF_OPEN → CLOSED after successes
    cb.RecordResult(true)
    cb.RecordResult(true)
    assert.Equal(t, CircuitClosed, cb.GetState())
}

func TestHealthChecker_RateLimitHandling(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusTooManyRequests)
    }))
    defer server.Close()
    
    backend := Backend{URL: server.URL}
    hc := NewHealthChecker(HealthCheckConfig{
        Endpoint: "/-/ready",
        Interval: time.Millisecond * 100,
        Timeout:  time.Millisecond * 50,
    })
    
    healthy, _, err := hc.performHealthCheck(context.Background(), backend)
    assert.NoError(t, err)
    assert.False(t, healthy)
    
    // Check that backend is marked as rate limited
    bh := hc.backends[backend.URL]
    assert.Equal(t, BackendRateLimited, bh.State)
}
```

### Integration Testing

```go
func TestHealthChecker_EndToEnd(t *testing.T) {
    // Start test servers with different behaviors
    healthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))
    defer healthyServer.Close()
    
    unhealthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusInternalServerError)
    }))
    defer unhealthyServer.Close()
    
    backends := []Backend{
        {URL: healthyServer.URL, Weight: 1},
        {URL: unhealthyServer.URL, Weight: 1},
    }
    
    hc := NewHealthChecker(HealthCheckConfig{
        Endpoint: "/-/ready",
        Interval: time.Millisecond * 100,
        Strategy: "exclude-unhealthy",
        Thresholds: HealthThresholds{
            Failure: 2,
            Success: 1,
        },
    })
    
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    err := hc.StartMonitoring(ctx)
    require.NoError(t, err)
    
    // Wait for health checks to complete
    time.Sleep(time.Millisecond * 300)
    
    // Verify states
    healthyBackend := hc.backends[healthyServer.URL]
    unhealthyBackend := hc.backends[unhealthyServer.URL]
    
    assert.Equal(t, BackendHealthy, healthyBackend.State)
    assert.Equal(t, BackendUnhealthy, unhealthyBackend.State)
}
```

## Best Practices

### Configuration Guidelines

1. **Health Check Intervals:**
   - Production: 30s - 60s intervals
   - Development: 10s - 15s intervals
   - Critical systems: 15s - 30s intervals

2. **Timeout Settings:**
   - Health check timeout should be < interval/2
   - Consider network latency in timeout calculations
   - Set timeouts appropriate for backend SLA

3. **Threshold Configuration:**
   - Failure threshold: 2-5 consecutive failures
   - Success threshold: 1-2 consecutive successes
   - Balance between sensitivity and stability

### Monitoring and Alerting

1. **Critical Alerts:**
   - All backends unhealthy
   - Circuit breaker open for extended periods
   - High rate limit occurrences

2. **Warning Alerts:**
   - Single backend consistently failing
   - Health check duration increasing
   - Frequent state transitions

### Performance Optimization

1. **Resource Management:**
   - Use connection pooling for health checks
   - Implement request timeouts
   - Clean up resources properly

2. **Distributed Coordination:**
   - Use efficient state synchronization
   - Implement conflict resolution
   - Handle network partitions gracefully