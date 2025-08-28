# Load Balancing Strategies

## Overview

This document details the load balancing algorithms and strategies available for distributing requests across multiple VictoriaMetrics upstream instances. Each strategy is designed to handle different use cases and traffic patterns while maintaining high availability and performance.

## Load Balancing Strategies

### 1. Round Robin

**Algorithm:** Distributes requests sequentially across all healthy backends in a circular fashion.

**Use Cases:**
- Backends have similar capacity and performance characteristics
- Simple, predictable load distribution
- Development and testing environments

**Implementation:**
```go
type RoundRobinBalancer struct {
    backends []Backend
    current  int32  // atomic counter for thread safety
    mu       sync.RWMutex
}

func (rb *RoundRobinBalancer) NextBackend() (*Backend, error) {
    rb.mu.RLock()
    defer rb.mu.RUnlock()
    
    if len(rb.backends) == 0 {
        return nil, ErrNoHealthyBackends
    }
    
    // Atomic increment with modulo operation
    index := atomic.AddInt32(&rb.current, 1) % int32(len(rb.backends))
    return &rb.backends[index], nil
}
```

**Configuration:**
```yaml
upstream:
  backends:
    - url: "https://vmselect-1.example.com"
      weight: 1  # Ignored for round-robin
    - url: "https://vmselect-2.example.com"
      weight: 2  # Ignored for round-robin
  strategy: "round-robin"
```

**Characteristics:**
- ✅ Simple and fast implementation
- ✅ Predictable load distribution
- ✅ No state tracking required
- ❌ Doesn't account for backend capacity differences
- ❌ Doesn't consider current load or response times

### 2. Weighted Round Robin

**Algorithm:** Distributes requests based on assigned weights, with higher-weighted backends receiving proportionally more requests.

**Use Cases:**
- Backends have different capacity or performance characteristics
- Need to gradually migrate traffic between backends
- A/B testing with traffic splitting

**Implementation:**
```go
type WeightedRoundRobinBalancer struct {
    backends []WeightedBackend
    weights  []int
    current  int32
    total    int
    mu       sync.RWMutex
}

type WeightedBackend struct {
    Backend Backend
    Weight  int
    CurrentWeight int
}

func (wrr *WeightedRoundRobinBalancer) NextBackend() (*Backend, error) {
    wrr.mu.Lock()
    defer wrr.mu.Unlock()
    
    if len(wrr.backends) == 0 {
        return nil, ErrNoHealthyBackends
    }
    
    // Smooth weighted round-robin algorithm
    var selected *WeightedBackend
    total := 0
    
    for i := range wrr.backends {
        backend := &wrr.backends[i]
        backend.CurrentWeight += backend.Weight
        total += backend.Weight
        
        if selected == nil || backend.CurrentWeight > selected.CurrentWeight {
            selected = backend
        }
    }
    
    if selected != nil {
        selected.CurrentWeight -= total
        return &selected.Backend, nil
    }
    
    return nil, ErrNoHealthyBackends
}
```

**Configuration:**
```yaml
upstream:
  backends:
    - url: "https://vmselect-1.example.com"
      weight: 3  # Receives 3/6 = 50% of traffic
    - url: "https://vmselect-2.example.com"  
      weight: 2  # Receives 2/6 = 33% of traffic
    - url: "https://vmselect-3.example.com"
      weight: 1  # Receives 1/6 = 17% of traffic
  strategy: "weighted-round-robin"
```

**Weight Distribution Examples:**
```
Weights [3, 2, 1] → Distribution: A,A,B,A,C,B (smooth distribution)
Weights [5, 1, 1] → Distribution: A,A,A,A,B,A,C (higher preference for A)
Weights [0, 2, 2] → Distribution: B,C,B,C (excludes A completely)
```

**Characteristics:**
- ✅ Accounts for backend capacity differences
- ✅ Smooth distribution (avoids bursts)
- ✅ Flexible traffic shaping
- ✅ Dynamic weight updates possible
- ❌ More complex than simple round-robin
- ❌ Still doesn't consider real-time load

### 3. Least Connections

**Algorithm:** Routes requests to the backend with the fewest active connections.

**Use Cases:**
- Backends have varying response times
- Long-running requests (streaming, large downloads)
- Need adaptive load balancing based on real-time conditions

**Implementation:**
```go
type LeastConnectionsBalancer struct {
    backends    []Backend
    connections map[string]*ConnectionTracker
    mu          sync.RWMutex
}

type ConnectionTracker struct {
    count    int32
    lastUsed time.Time
}

func (lcb *LeastConnectionsBalancer) NextBackend() (*Backend, error) {
    lcb.mu.RLock()
    defer lcb.mu.RUnlock()
    
    if len(lcb.backends) == 0 {
        return nil, ErrNoHealthyBackends
    }
    
    var selected *Backend
    minConnections := int32(math.MaxInt32)
    
    for i := range lcb.backends {
        backend := &lcb.backends[i]
        tracker := lcb.connections[backend.URL]
        
        currentConnections := atomic.LoadInt32(&tracker.count)
        if currentConnections < minConnections {
            minConnections = currentConnections
            selected = backend
        }
    }
    
    if selected != nil {
        tracker := lcb.connections[selected.URL]
        atomic.AddInt32(&tracker.count, 1)
        tracker.lastUsed = time.Now()
    }
    
    return selected, nil
}

func (lcb *LeastConnectionsBalancer) ReleaseConnection(backend *Backend) {
    lcb.mu.RLock()
    defer lcb.mu.RUnlock()
    
    if tracker, exists := lcb.connections[backend.URL]; exists {
        atomic.AddInt32(&tracker.count, -1)
    }
}
```

**Configuration:**
```yaml
upstream:
  backends:
    - url: "https://vmselect-1.example.com"
      weight: 1  # Can be used as tie-breaker
    - url: "https://vmselect-2.example.com"
      weight: 1
  strategy: "least-connections"
```

**Connection Tracking:**
- Increment counter on request start
- Decrement counter on request completion
- Periodic cleanup of stale connections (every 5 minutes)
- Handle edge cases (crashed connections, timeouts)

**Characteristics:**
- ✅ Adapts to real-time backend load
- ✅ Good for long-running requests
- ✅ Handles varying backend performance
- ❌ More complex state management
- ❌ Potential for connection counting errors
- ❌ Requires careful cleanup mechanisms

## Advanced Load Balancing Features

### Health-Aware Load Balancing

All load balancing strategies integrate with the health checking system:

```go
func (lb *LoadBalancer) getHealthyBackends() []Backend {
    var healthy []Backend
    for _, backend := range lb.backends {
        switch backend.State {
        case BackendHealthy:
            healthy = append(healthy, backend)
        case BackendRateLimited:
            // Include rate-limited backends with reduced probability
            if time.Since(backend.LastRateLimitTime) > time.Minute {
                healthy = append(healthy, backend)
            }
        case BackendUnhealthy, BackendMaintenance:
            // Exclude from load balancing
            continue
        }
    }
    return healthy
}
```

### Circuit Breaker Integration

Load balancers integrate with circuit breaker patterns:

```go
func (lb *LoadBalancer) NextBackendWithCircuitBreaker() (*Backend, error) {
    for attempts := 0; attempts < len(lb.backends); attempts++ {
        backend, err := lb.nextBackendRaw()
        if err != nil {
            return nil, err
        }
        
        circuitBreaker := lb.circuitBreakers[backend.URL]
        if circuitBreaker.CanExecute() {
            return backend, nil
        }
    }
    
    return nil, ErrAllBackendsCircuitOpen
}
```

### Dynamic Backend Management

Backends can be added/removed/updated dynamically:

```go
type DynamicLoadBalancer interface {
    AddBackend(backend Backend) error
    RemoveBackend(url string) error
    UpdateBackendWeight(url string, weight int) error
    GetBackendsStatus() []BackendStatus
    SetBackendMaintenance(url string, maintenance bool) error
}
```

## Performance Considerations

### Thread Safety

All load balancers are designed for high concurrency:

- **Atomic Operations:** Use atomic counters for frequently updated values
- **Read-Write Mutexes:** Prefer RWMutex for read-heavy operations
- **Lock-Free Algorithms:** Where possible, avoid locks entirely

### Memory Management

```go
// Periodic cleanup of stale connection trackers
func (lcb *LeastConnectionsBalancer) cleanupStaleConnections() {
    lcb.mu.Lock()
    defer lcb.mu.Unlock()
    
    cutoff := time.Now().Add(-5 * time.Minute)
    for url, tracker := range lcb.connections {
        if tracker.lastUsed.Before(cutoff) && tracker.count == 0 {
            delete(lcb.connections, url)
        }
    }
}
```

### Hot Path Optimization

```go
// Fast path for single backend
func (rb *RoundRobinBalancer) NextBackend() (*Backend, error) {
    if len(rb.backends) == 1 {
        return &rb.backends[0], nil  // Skip atomic operations
    }
    
    // Standard round-robin logic
    index := atomic.AddInt32(&rb.current, 1) % int32(len(rb.backends))
    return &rb.backends[index], nil
}
```

## Error Handling and Fallback

### Backend Selection Failures

```go
func (lb *LoadBalancer) NextBackendWithFallback() (*Backend, error) {
    // Try to get healthy backend
    backend, err := lb.NextHealthyBackend()
    if err == nil {
        return backend, nil
    }
    
    // Fallback to rate-limited backends
    backend, err = lb.NextRateLimitedBackend()
    if err == nil {
        lb.logger.Warn("Using rate-limited backend as fallback", 
            Field{Key: "backend", Value: backend.URL})
        return backend, nil
    }
    
    // Last resort: try unhealthy backends
    backend, err = lb.NextUnhealthyBackend()
    if err == nil {
        lb.logger.Error("Using unhealthy backend as last resort",
            Field{Key: "backend", Value: backend.URL})
        return backend, nil
    }
    
    return nil, ErrNoAvailableBackends
}
```

### Graceful Degradation

```go
type GracefulLoadBalancer struct {
    primary   LoadBalancer
    fallback  LoadBalancer
    threshold time.Duration
}

func (glb *GracefulLoadBalancer) NextBackend() (*Backend, error) {
    // Try primary strategy
    backend, err := glb.primary.NextBackend()
    if err == nil {
        return backend, nil
    }
    
    // Fall back to simpler strategy
    glb.logger.Warn("Primary load balancer failed, using fallback")
    return glb.fallback.NextBackend()
}
```

## Metrics and Observability

### Load Balancing Metrics

```go
// Metrics tracked per backend and strategy
type LoadBalancerMetrics struct {
    BackendRequests     prometheus.CounterVec
    BackendConnections  prometheus.GaugeVec
    SelectionDuration   prometheus.HistogramVec
    StrategyChanges     prometheus.CounterVec
}

func (lb *LoadBalancer) recordBackendSelection(backend *Backend, duration time.Duration) {
    lb.metrics.BackendRequests.WithLabelValues(
        backend.URL, 
        lb.strategy,
        backend.State.String(),
    ).Inc()
    
    lb.metrics.SelectionDuration.WithLabelValues(
        lb.strategy,
    ).Observe(duration.Seconds())
}
```

### Health Status Monitoring

```go
func (lb *LoadBalancer) GetBackendsStatus() []BackendStatus {
    var status []BackendStatus
    
    for _, backend := range lb.backends {
        tracker := lb.connections[backend.URL]
        
        status = append(status, BackendStatus{
            URL:           backend.URL,
            State:         backend.State,
            Weight:        backend.Weight,
            ActiveConns:   atomic.LoadInt32(&tracker.count),
            LastUsed:      tracker.lastUsed,
            RequestCount:  lb.getRequestCount(backend.URL),
            ErrorRate:     lb.getErrorRate(backend.URL),
        })
    }
    
    return status
}
```

## Testing Strategies

### Unit Testing

```go
func TestWeightedRoundRobin_Distribution(t *testing.T) {
    backends := []WeightedBackend{
        {Backend: Backend{URL: "backend1"}, Weight: 3},
        {Backend: Backend{URL: "backend2"}, Weight: 1},
    }
    
    balancer := NewWeightedRoundRobinBalancer(backends)
    
    // Test distribution over 1000 requests
    counts := make(map[string]int)
    for i := 0; i < 1000; i++ {
        backend, err := balancer.NextBackend()
        require.NoError(t, err)
        counts[backend.URL]++
    }
    
    // Verify 3:1 ratio (within tolerance)
    assert.InDelta(t, 750, counts["backend1"], 50)
    assert.InDelta(t, 250, counts["backend2"], 50)
}
```

### Concurrency Testing

```go
func TestLoadBalancer_ConcurrentAccess(t *testing.T) {
    balancer := NewRoundRobinBalancer(backends)
    var wg sync.WaitGroup
    selections := make(chan string, 1000)
    
    // 100 goroutines making concurrent requests
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 10; j++ {
                backend, err := balancer.NextBackend()
                require.NoError(t, err)
                selections <- backend.URL
            }
        }()
    }
    
    wg.Wait()
    close(selections)
    
    // Verify no duplicates or corruption
    counts := make(map[string]int)
    for url := range selections {
        counts[url]++
    }
    
    total := 0
    for _, count := range counts {
        total += count
    }
    assert.Equal(t, 1000, total)
}
```

### Integration Testing

```go
func TestLoadBalancer_WithRealBackends(t *testing.T) {
    // Start test HTTP servers
    servers := make([]*httptest.Server, 3)
    for i := range servers {
        servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            w.WriteHeader(http.StatusOK)
            fmt.Fprintf(w, "Backend %d", i)
        }))
        defer servers[i].Close()
    }
    
    // Configure load balancer with test servers
    backends := make([]Backend, len(servers))
    for i, server := range servers {
        backends[i] = Backend{URL: server.URL, Weight: 1}
    }
    
    balancer := NewWeightedRoundRobinBalancer(backends)
    
    // Test actual HTTP requests
    for i := 0; i < 10; i++ {
        backend, err := balancer.NextBackend()
        require.NoError(t, err)
        
        resp, err := http.Get(backend.URL)
        require.NoError(t, err)
        assert.Equal(t, http.StatusOK, resp.StatusCode)
        resp.Body.Close()
    }
}
```

## Best Practices

### Strategy Selection Guidelines

1. **Use Round Robin When:**
   - Backends are homogeneous (same capacity/performance)
   - Simple deployment with minimal configuration
   - Testing/development environments

2. **Use Weighted Round Robin When:**
   - Backends have different capacities
   - Need traffic shaping or gradual migration
   - A/B testing scenarios

3. **Use Least Connections When:**
   - Request processing times vary significantly
   - Long-running connections (streaming, WebSocket)
   - Backends have unpredictable performance characteristics

### Performance Tuning

1. **Connection Tracking:**
   - Clean up stale connections regularly
   - Set reasonable limits on connection counts
   - Handle connection leaks gracefully

2. **Lock Contention:**
   - Use atomic operations for hot paths
   - Prefer RWMutex for read-heavy operations
   - Consider lock-free algorithms for high throughput

3. **Memory Usage:**
   - Limit the number of tracked connections
   - Implement bounded data structures
   - Regular cleanup of unused state

### Monitoring and Alerting

1. **Key Metrics to Monitor:**
   - Backend selection distribution
   - Connection counts per backend
   - Load balancer selection latency
   - Strategy fallback occurrences

2. **Alert Conditions:**
   - Uneven load distribution (beyond expected variance)
   - High connection counts indicating leaks
   - Frequent fallbacks to unhealthy backends
   - Load balancer selection failures