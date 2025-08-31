# Testing Strategy for Multiple Upstream URLs

## Overview

This document outlines a comprehensive testing strategy for the multiple upstream URLs feature, following Test-Driven Development (TDD) principles. The strategy covers unit tests, integration tests, performance tests, and chaos engineering to ensure reliability and correctness of the load balancing system.

## Testing Philosophy

### Test-Driven Development (TDD) Approach

1. **Red Phase**: Write failing tests first to define expected behavior
2. **Green Phase**: Write minimal code to make tests pass
3. **Refactor Phase**: Improve code quality while keeping tests green

### Testing Pyramid

```
    /\
   /  \     E2E Tests (Few)
  /____\    Integration Tests (Some)  
 /______\   Unit Tests (Many)
```

**Distribution:**
- **Unit Tests (70%)**: Fast, isolated, comprehensive coverage
- **Integration Tests (20%)**: Component interactions, real dependencies
- **E2E Tests (10%)**: Full system behavior, user scenarios

## Unit Testing Strategy

### Test Structure and Organization

```
internal/
├── services/
│   ├── loadbalancer/
│   │   ├── round_robin.go
│   │   ├── round_robin_test.go
│   │   ├── weighted_round_robin.go
│   │   ├── weighted_round_robin_test.go
│   │   ├── least_connections.go
│   │   └── least_connections_test.go
│   ├── healthchecker/
│   │   ├── health_checker.go
│   │   ├── health_checker_test.go
│   │   ├── circuit_breaker.go
│   │   └── circuit_breaker_test.go
│   └── statestorage/
│       ├── local_storage.go
│       ├── local_storage_test.go
│       ├── redis_storage.go
│       └── redis_storage_test.go
```

### Load Balancer Unit Tests

#### Round Robin Balancer Tests

```go
func TestRoundRobinBalancer_BasicDistribution(t *testing.T) {
    backends := []Backend{
        {URL: "http://backend1.com", Weight: 1},
        {URL: "http://backend2.com", Weight: 1},
        {URL: "http://backend3.com", Weight: 1},
    }
    
    balancer := NewRoundRobinBalancer(backends)
    
    // Test sequential distribution
    expectedOrder := []string{
        "http://backend1.com",
        "http://backend2.com", 
        "http://backend3.com",
        "http://backend1.com", // Should wrap around
    }
    
    for i, expected := range expectedOrder {
        backend, err := balancer.NextBackend()
        require.NoError(t, err)
        assert.Equal(t, expected, backend.URL, "Request %d should go to %s", i+1, expected)
    }
}

func TestRoundRobinBalancer_ConcurrentAccess(t *testing.T) {
    backends := []Backend{
        {URL: "http://backend1.com", Weight: 1},
        {URL: "http://backend2.com", Weight: 1},
    }
    
    balancer := NewRoundRobinBalancer(backends)
    
    const numGoroutines = 100
    const requestsPerGoroutine = 10
    
    results := make(chan string, numGoroutines*requestsPerGoroutine)
    var wg sync.WaitGroup
    
    // Start concurrent goroutines
    for i := 0; i < numGoroutines; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < requestsPerGoroutine; j++ {
                backend, err := balancer.NextBackend()
                require.NoError(t, err)
                results <- backend.URL
            }
        }()
    }
    
    wg.Wait()
    close(results)
    
    // Count distribution
    counts := make(map[string]int)
    totalRequests := 0
    for url := range results {
        counts[url]++
        totalRequests++
    }
    
    // Verify total requests
    assert.Equal(t, numGoroutines*requestsPerGoroutine, totalRequests)
    
    // Verify balanced distribution (within 10% tolerance)
    expected := totalRequests / len(backends)
    for backend, count := range counts {
        tolerance := float64(expected) * 0.1
        assert.InDeltaf(t, expected, count, tolerance, 
            "Backend %s should have ~%d requests, got %d", backend, expected, count)
    }
}

func TestRoundRobinBalancer_NoBackends(t *testing.T) {
    balancer := NewRoundRobinBalancer([]Backend{})
    
    backend, err := balancer.NextBackend()
    assert.Nil(t, backend)
    assert.Equal(t, ErrNoHealthyBackends, err)
}

func TestRoundRobinBalancer_SingleBackend(t *testing.T) {
    backends := []Backend{{URL: "http://only-backend.com", Weight: 1}}
    balancer := NewRoundRobinBalancer(backends)
    
    // Should return the same backend multiple times
    for i := 0; i < 5; i++ {
        backend, err := balancer.NextBackend()
        require.NoError(t, err)
        assert.Equal(t, "http://only-backend.com", backend.URL)
    }
}
```

#### Weighted Round Robin Tests

```go
func TestWeightedRoundRobinBalancer_WeightDistribution(t *testing.T) {
    backends := []WeightedBackend{
        {Backend: Backend{URL: "http://backend1.com"}, Weight: 3},
        {Backend: Backend{URL: "http://backend2.com"}, Weight: 1},
        {Backend: Backend{URL: "http://backend3.com"}, Weight: 2},
    }
    
    balancer := NewWeightedRoundRobinBalancer(backends)
    
    // Test over 600 requests (100 cycles of weight sum = 6)
    requests := 600
    counts := make(map[string]int)
    
    for i := 0; i < requests; i++ {
        backend, err := balancer.NextBackend()
        require.NoError(t, err)
        counts[backend.URL]++
    }
    
    // Verify weight proportions
    assert.Equal(t, 300, counts["http://backend1.com"]) // 3/6 * 600
    assert.Equal(t, 100, counts["http://backend2.com"]) // 1/6 * 600  
    assert.Equal(t, 200, counts["http://backend3.com"]) // 2/6 * 600
}

func TestWeightedRoundRobinBalancer_SmoothDistribution(t *testing.T) {
    backends := []WeightedBackend{
        {Backend: Backend{URL: "http://backend1.com"}, Weight: 5},
        {Backend: Backend{URL: "http://backend2.com"}, Weight: 1},
        {Backend: Backend{URL: "http://backend3.com"}, Weight: 1},
    }
    
    balancer := NewWeightedRoundRobinBalancer(backends)
    
    // Expected smooth pattern for weights [5,1,1]
    expectedPattern := []string{
        "http://backend1.com", "http://backend1.com", "http://backend2.com",
        "http://backend1.com", "http://backend3.com", "http://backend1.com",
        "http://backend1.com", // Smooth distribution avoids bursts
    }
    
    for i, expected := range expectedPattern {
        backend, err := balancer.NextBackend()
        require.NoError(t, err)
        assert.Equal(t, expected, backend.URL, "Request %d pattern mismatch", i+1)
    }
}

func TestWeightedRoundRobinBalancer_ZeroWeights(t *testing.T) {
    backends := []WeightedBackend{
        {Backend: Backend{URL: "http://backend1.com"}, Weight: 0}, // Should be excluded
        {Backend: Backend{URL: "http://backend2.com"}, Weight: 2},
        {Backend: Backend{URL: "http://backend3.com"}, Weight: 1},
    }
    
    balancer := NewWeightedRoundRobinBalancer(backends)
    
    // Make 100 requests, backend1 should never be selected
    counts := make(map[string]int)
    for i := 0; i < 100; i++ {
        backend, err := balancer.NextBackend()
        require.NoError(t, err)
        counts[backend.URL]++
    }
    
    assert.Equal(t, 0, counts["http://backend1.com"]) // Zero weight = excluded
    assert.True(t, counts["http://backend2.com"] > 0)
    assert.True(t, counts["http://backend3.com"] > 0)
}
```

#### Least Connections Tests

```go
func TestLeastConnectionsBalancer_InitialDistribution(t *testing.T) {
    backends := []Backend{
        {URL: "http://backend1.com", Weight: 1},
        {URL: "http://backend2.com", Weight: 1},
        {URL: "http://backend3.com", Weight: 1},
    }
    
    balancer := NewLeastConnectionsBalancer(backends)
    
    // First 3 requests should go to different backends
    selectedBackends := make(map[string]bool)
    
    for i := 0; i < 3; i++ {
        backend, err := balancer.NextBackend()
        require.NoError(t, err)
        
        // Start connection (simulate request start)
        balancer.StartConnection(backend)
        
        selectedBackends[backend.URL] = true
    }
    
    // All backends should have been selected
    assert.Len(t, selectedBackends, 3)
    
    // Verify connection counts
    status := balancer.GetBackendsStatus()
    for _, backendStatus := range status {
        assert.Equal(t, int32(1), backendStatus.ActiveConns,
            "Backend %s should have 1 active connection", backendStatus.Backend.URL)
    }
}

func TestLeastConnectionsBalancer_ConnectionTracking(t *testing.T) {
    backends := []Backend{
        {URL: "http://backend1.com", Weight: 1},
        {URL: "http://backend2.com", Weight: 1},
    }
    
    balancer := NewLeastConnectionsBalancer(backends)
    
    // Start 2 connections to backend1
    backend1, err := balancer.NextBackend()
    require.NoError(t, err)
    balancer.StartConnection(backend1)
    balancer.StartConnection(backend1)
    
    // Next request should go to backend2 (0 connections vs 2)
    backend2, err := balancer.NextBackend()
    require.NoError(t, err)
    assert.NotEqual(t, backend1.URL, backend2.URL)
    
    // Finish one connection on backend1
    balancer.FinishConnection(backend1)
    
    // Verify connection counts
    status := balancer.GetBackendsStatus()
    backend1Status := findBackendStatus(status, backend1.URL)
    backend2Status := findBackendStatus(status, backend2.URL)
    
    assert.Equal(t, int32(1), backend1Status.ActiveConns)
    assert.Equal(t, int32(0), backend2Status.ActiveConns)
}

func TestLeastConnectionsBalancer_ConnectionLeakProtection(t *testing.T) {
    backends := []Backend{{URL: "http://backend1.com", Weight: 1}}
    balancer := NewLeastConnectionsBalancer(backends)
    
    // Simulate connection leak (start without finish)
    backend, err := balancer.NextBackend()
    require.NoError(t, err)
    
    for i := 0; i < 10; i++ {
        balancer.StartConnection(backend)
    }
    
    // Trigger cleanup (normally done periodically)
    balancer.CleanupStaleConnections()
    
    // Verify cleanup worked
    status := balancer.GetBackendsStatus()
    backendStatus := findBackendStatus(status, backend.URL)
    assert.LessOrEqual(t, backendStatus.ActiveConns, int32(10),
        "Cleanup should limit connection count growth")
}

func findBackendStatus(status []BackendStatus, url string) *BackendStatus {
    for _, s := range status {
        if s.Backend.URL == url {
            return &s
        }
    }
    return nil
}
```

### Health Checker Unit Tests

#### Basic Health Checking

```go
func TestHealthChecker_BasicHealthCheck(t *testing.T) {
    // Setup mock HTTP server
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        assert.Equal(t, "/-/ready", r.URL.Path)
        w.WriteHeader(http.StatusOK)
    }))
    defer server.Close()
    
    backend := Backend{URL: server.URL, Weight: 1}
    config := HealthCheckConfig{
        Endpoint: "/-/ready",
        Timeout:  time.Second,
        Thresholds: HealthThresholds{
            Failure: 3,
            Success: 1,
        },
    }
    
    hc := NewHealthChecker(config, nil, testLogger())
    
    // Perform health check
    healthy, duration, err := hc.performHealthCheck(context.Background(), backend)
    
    assert.NoError(t, err)
    assert.True(t, healthy)
    assert.Greater(t, duration, time.Duration(0))
}

func TestHealthChecker_UnhealthyBackend(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusInternalServerError)
    }))
    defer server.Close()
    
    backend := Backend{URL: server.URL, Weight: 1}
    config := HealthCheckConfig{
        Endpoint: "/-/ready",
        Timeout:  time.Second,
        Thresholds: HealthThresholds{
            Failure: 1, // Fail immediately
            Success: 1,
        },
    }
    
    hc := NewHealthChecker(config, nil, testLogger())
    
    healthy, _, err := hc.performHealthCheck(context.Background(), backend)
    
    assert.NoError(t, err)
    assert.False(t, healthy)
}

func TestHealthChecker_RateLimitedBackend(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusTooManyRequests)
    }))
    defer server.Close()
    
    backend := Backend{URL: server.URL, Weight: 1}
    config := HealthCheckConfig{
        Endpoint: "/-/ready",
        Timeout:  time.Second,
    }
    
    hc := NewHealthChecker(config, nil, testLogger())
    
    healthy, _, err := hc.performHealthCheck(context.Background(), backend)
    
    assert.NoError(t, err)
    assert.False(t, healthy)
    
    // Check if backend is marked as rate limited
    bh := hc.backends[backend.URL]
    assert.Equal(t, BackendRateLimited, bh.State)
}

func TestHealthChecker_TimeoutHandling(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        time.Sleep(100 * time.Millisecond) // Longer than timeout
        w.WriteHeader(http.StatusOK)
    }))
    defer server.Close()
    
    backend := Backend{URL: server.URL, Weight: 1}
    config := HealthCheckConfig{
        Endpoint: "/-/ready",
        Timeout:  50 * time.Millisecond, // Short timeout
    }
    
    hc := NewHealthChecker(config, nil, testLogger())
    
    healthy, duration, err := hc.performHealthCheck(context.Background(), backend)
    
    assert.Error(t, err)
    assert.False(t, healthy)
    assert.Greater(t, duration, config.Timeout-10*time.Millisecond) // Should timeout
}
```

#### Circuit Breaker Tests

```go
func TestCircuitBreaker_StateTransitions(t *testing.T) {
    config := CircuitBreakerConfig{
        MinRequestsToTrip:    3,
        OpenTimeout:         100 * time.Millisecond,
        HalfOpenMaxRequests: 2,
        SuccessThreshold:    2,
    }
    
    cb := NewCircuitBreaker(config)
    
    // Initial state should be CLOSED
    assert.Equal(t, CircuitClosed, cb.GetState())
    assert.True(t, cb.CanExecute())
    
    // Record failures to trip circuit
    for i := 0; i < 3; i++ {
        cb.RecordResult(false)
    }
    
    // Should transition to OPEN
    assert.Equal(t, CircuitOpen, cb.GetState())
    assert.False(t, cb.CanExecute())
    
    // Wait for open timeout
    time.Sleep(120 * time.Millisecond)
    
    // Should transition to HALF_OPEN
    assert.True(t, cb.CanExecute())
    assert.Equal(t, CircuitHalfOpen, cb.GetState())
    
    // Record successes to close circuit
    cb.RecordResult(true)
    cb.RecordResult(true)
    
    // Should transition back to CLOSED
    assert.Equal(t, CircuitClosed, cb.GetState())
    assert.True(t, cb.CanExecute())
}

func TestCircuitBreaker_HalfOpenLimiting(t *testing.T) {
    config := CircuitBreakerConfig{
        OpenTimeout:         50 * time.Millisecond,
        HalfOpenMaxRequests: 2,
    }
    
    cb := NewCircuitBreaker(config)
    
    // Force to OPEN state
    cb.transitionToOpen()
    time.Sleep(60 * time.Millisecond)
    
    // Should allow limited requests in HALF_OPEN
    assert.True(t, cb.CanExecute())  // 1st request
    assert.True(t, cb.CanExecute())  // 2nd request
    assert.False(t, cb.CanExecute()) // 3rd request should be blocked
}

func TestCircuitBreaker_FailureInHalfOpen(t *testing.T) {
    config := CircuitBreakerConfig{
        OpenTimeout: 50 * time.Millisecond,
    }
    
    cb := NewCircuitBreaker(config)
    
    // Force to HALF_OPEN
    cb.transitionToOpen()
    time.Sleep(60 * time.Millisecond)
    cb.CanExecute() // Transition to HALF_OPEN
    
    // Record failure in HALF_OPEN
    cb.RecordResult(false)
    
    // Should transition back to OPEN
    assert.Equal(t, CircuitOpen, cb.GetState())
    assert.False(t, cb.CanExecute())
}
```

### State Storage Unit Tests

#### Local Storage Tests

```go
func TestLocalStorage_BasicOperations(t *testing.T) {
    storage := NewLocalStorage("test-node")
    ctx := context.Background()
    
    key := "test-key"
    value := []byte("test-value")
    
    // Test Set and Get
    err := storage.Set(ctx, key, value, time.Hour)
    assert.NoError(t, err)
    
    retrieved, err := storage.Get(ctx, key)
    assert.NoError(t, err)
    assert.Equal(t, value, retrieved)
    
    // Test Delete
    err = storage.Delete(ctx, key)
    assert.NoError(t, err)
    
    _, err = storage.Get(ctx, key)
    assert.Equal(t, ErrKeyNotFound, err)
}

func TestLocalStorage_TTLExpiration(t *testing.T) {
    storage := NewLocalStorage("test-node")
    ctx := context.Background()
    
    key := "expire-key"
    value := []byte("expire-value")
    
    // Set with short TTL
    err := storage.Set(ctx, key, value, 10*time.Millisecond)
    assert.NoError(t, err)
    
    // Should exist immediately
    retrieved, err := storage.Get(ctx, key)
    assert.NoError(t, err)
    assert.Equal(t, value, retrieved)
    
    // Wait for expiration
    time.Sleep(20 * time.Millisecond)
    
    // Should be expired
    _, err = storage.Get(ctx, key)
    assert.Equal(t, ErrKeyNotFound, err)
}

func TestLocalStorage_Watch(t *testing.T) {
    storage := NewLocalStorage("test-node")
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    // Setup watcher
    events, err := storage.Watch(ctx, "test:")
    assert.NoError(t, err)
    
    // Set a key that matches the prefix
    key := "test:watched-key"
    value := []byte("watched-value")
    
    go func() {
        time.Sleep(10 * time.Millisecond)
        storage.Set(context.Background(), key, value, time.Hour)
    }()
    
    // Wait for event
    select {
    case event := <-events:
        assert.Equal(t, EventSet, event.Type)
        assert.Equal(t, key, event.Key)
        assert.Equal(t, value, event.Value)
    case <-time.After(100 * time.Millisecond):
        t.Fatal("Expected watch event not received")
    }
}

func TestLocalStorage_ConcurrentAccess(t *testing.T) {
    storage := NewLocalStorage("test-node")
    ctx := context.Background()
    
    var wg sync.WaitGroup
    numGoroutines := 100
    
    // Concurrent writes
    for i := 0; i < numGoroutines; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            key := fmt.Sprintf("concurrent-key-%d", i)
            value := []byte(fmt.Sprintf("value-%d", i))
            
            err := storage.Set(ctx, key, value, time.Hour)
            assert.NoError(t, err)
        }(i)
    }
    
    wg.Wait()
    
    // Verify all values exist
    for i := 0; i < numGoroutines; i++ {
        key := fmt.Sprintf("concurrent-key-%d", i)
        expected := []byte(fmt.Sprintf("value-%d", i))
        
        value, err := storage.Get(ctx, key)
        assert.NoError(t, err)
        assert.Equal(t, expected, value)
    }
}
```

## Integration Testing Strategy

### Test Environment Setup

```go
type IntegrationTestSuite struct {
    backends     []*httptest.Server
    loadBalancer LoadBalancer
    healthChecker *HealthChecker
    stateStorage StateStorage
    cleanup      func()
}

func (suite *IntegrationTestSuite) SetupSuite() {
    // Start test HTTP servers
    suite.backends = make([]*httptest.Server, 3)
    
    for i := range suite.backends {
        i := i // Capture loop variable
        suite.backends[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if r.URL.Path == "/-/ready" {
                // Health check endpoint
                w.WriteHeader(http.StatusOK)
                return
            }
            
            // Regular request endpoint
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusOK)
            fmt.Fprintf(w, `{"status":"success","data":{"backend":"%d"}}`, i)
        }))
    }
    
    // Setup state storage
    suite.stateStorage = NewLocalStorage("test-integration")
    
    // Create backends configuration
    backends := make([]Backend, len(suite.backends))
    for i, server := range suite.backends {
        backends[i] = Backend{
            URL:    server.URL,
            Weight: 1,
        }
    }
    
    // Setup load balancer
    suite.loadBalancer = NewRoundRobinBalancer(backends)
    
    // Setup health checker
    config := HealthCheckConfig{
        Enabled:  true,
        Endpoint: "/-/ready",
        Interval: 100 * time.Millisecond,
        Timeout:  50 * time.Millisecond,
        Strategy: "exclude-unhealthy",
        Thresholds: HealthThresholds{
            Failure: 2,
            Success: 1,
        },
    }
    
    suite.healthChecker = NewHealthChecker(config, suite.stateStorage, testLogger())
    
    suite.cleanup = func() {
        for _, server := range suite.backends {
            server.Close()
        }
        suite.stateStorage.Close()
        suite.healthChecker.Stop()
    }
}

func (suite *IntegrationTestSuite) TearDownSuite() {
    suite.cleanup()
}
```

### End-to-End Request Flow Tests

```go
func TestIntegration_RequestFlow(t *testing.T) {
    suite := &IntegrationTestSuite{}
    suite.SetupSuite()
    defer suite.TearDownSuite()
    
    ctx := context.Background()
    
    // Start health checking
    err := suite.healthChecker.StartMonitoring(ctx)
    require.NoError(t, err)
    
    // Wait for initial health checks
    time.Sleep(200 * time.Millisecond)
    
    // Make requests and verify distribution
    client := &http.Client{Timeout: time.Second}
    responses := make(map[string]int)
    
    for i := 0; i < 30; i++ {
        backend, err := suite.loadBalancer.NextBackend()
        require.NoError(t, err)
        
        resp, err := client.Get(backend.URL + "/api/v1/query")
        require.NoError(t, err)
        defer resp.Body.Close()
        
        assert.Equal(t, http.StatusOK, resp.StatusCode)
        
        var result map[string]interface{}
        err = json.NewDecoder(resp.Body).Decode(&result)
        require.NoError(t, err)
        
        data := result["data"].(map[string]interface{})
        backendID := data["backend"].(string)
        responses[backendID]++
    }
    
    // Verify all backends received requests
    assert.Len(t, responses, 3, "All backends should receive requests")
    
    // Verify reasonable distribution (each backend should get ~10 requests ±3)
    for backendID, count := range responses {
        assert.InDelta(t, 10, count, 3, "Backend %s distribution", backendID)
    }
}

func TestIntegration_BackendFailureRecovery(t *testing.T) {
    suite := &IntegrationTestSuite{}
    suite.SetupSuite()
    defer suite.TearDownSuite()
    
    ctx := context.Background()
    err := suite.healthChecker.StartMonitoring(ctx)
    require.NoError(t, err)
    
    // Wait for initial health checks
    time.Sleep(200 * time.Millisecond)
    
    // Verify all backends are healthy
    status := suite.healthChecker.GetBackendsStatus()
    for _, backendStatus := range status {
        assert.Equal(t, BackendHealthy, backendStatus.State)
    }
    
    // Make one backend unhealthy
    failingBackend := suite.backends[0]
    failingBackend.Close()
    
    // Wait for health checker to detect failure
    time.Sleep(300 * time.Millisecond)
    
    // Verify failing backend is marked unhealthy
    status = suite.healthChecker.GetBackendsStatus()
    var failingBackendStatus *BackendStatus
    for _, s := range status {
        if strings.Contains(s.Backend.URL, failingBackend.Listener.Addr().String()) {
            failingBackendStatus = &s
            break
        }
    }
    
    require.NotNil(t, failingBackendStatus)
    assert.Equal(t, BackendUnhealthy, failingBackendStatus.State)
    
    // Make requests - should only go to healthy backends
    client := &http.Client{Timeout: time.Second}
    healthyResponses := 0
    
    for i := 0; i < 20; i++ {
        backend, err := suite.loadBalancer.NextBackend()
        if err == ErrNoHealthyBackends {
            continue // Skip if no healthy backends
        }
        require.NoError(t, err)
        
        resp, err := client.Get(backend.URL + "/api/v1/query")
        if err != nil {
            continue // Skip failed requests
        }
        resp.Body.Close()
        
        if resp.StatusCode == http.StatusOK {
            healthyResponses++
        }
    }
    
    // Should have successful responses only from healthy backends
    assert.Greater(t, healthyResponses, 0, "Should have successful responses from healthy backends")
}
```

### Redis Integration Tests

```go
func TestRedisIntegration_StateSync(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping Redis integration test in short mode")
    }
    
    // Setup Redis storage instances for two nodes
    config1 := RedisConfig{
        Address: "localhost:6379",
        NodeID:  "node-1",
        KeyPrefix: "test-sync:",
    }
    
    config2 := RedisConfig{
        Address: "localhost:6379",
        NodeID:  "node-2", 
        KeyPrefix: "test-sync:",
    }
    
    storage1, err := NewRedisStorage(config1)
    require.NoError(t, err)
    defer storage1.Close()
    
    storage2, err := NewRedisStorage(config2)
    require.NoError(t, err)
    defer storage2.Close()
    
    ctx := context.Background()
    
    // Setup watcher on node-2
    events, err := storage2.Watch(ctx, "backend:health:")
    require.NoError(t, err)
    
    // Write data from node-1
    key := "backend:health:test-backend"
    value := []byte(`{"url":"test-backend","state":"healthy"}`)
    
    err = storage1.Set(ctx, key, value, time.Minute)
    require.NoError(t, err)
    
    // Verify node-2 receives the event
    select {
    case event := <-events:
        assert.Equal(t, EventSet, event.Type)
        assert.Equal(t, key, event.Key)
        assert.Equal(t, value, event.Value)
        assert.Equal(t, "node-1", event.NodeID)
    case <-time.After(time.Second):
        t.Fatal("Expected sync event not received")
    }
    
    // Verify node-2 can read the data
    retrieved, err := storage2.Get(ctx, key)
    require.NoError(t, err)
    assert.Equal(t, value, retrieved)
}
```

## Performance Testing Strategy

### Load Testing

```go
func BenchmarkRoundRobinBalancer_NextBackend(b *testing.B) {
    backends := make([]Backend, 10)
    for i := range backends {
        backends[i] = Backend{
            URL:    fmt.Sprintf("http://backend%d.com", i),
            Weight: 1,
        }
    }
    
    balancer := NewRoundRobinBalancer(backends)
    
    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            _, err := balancer.NextBackend()
            if err != nil {
                b.Fatal(err)
            }
        }
    })
}

func BenchmarkWeightedRoundRobinBalancer_NextBackend(b *testing.B) {
    backends := make([]WeightedBackend, 10)
    for i := range backends {
        backends[i] = WeightedBackend{
            Backend: Backend{URL: fmt.Sprintf("http://backend%d.com", i)},
            Weight:  i + 1, // Varying weights
        }
    }
    
    balancer := NewWeightedRoundRobinBalancer(backends)
    
    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            _, err := balancer.NextBackend()
            if err != nil {
                b.Fatal(err)
            }
        }
    })
}

func BenchmarkLeastConnectionsBalancer_NextBackend(b *testing.B) {
    backends := make([]Backend, 10)
    for i := range backends {
        backends[i] = Backend{
            URL:    fmt.Sprintf("http://backend%d.com", i),
            Weight: 1,
        }
    }
    
    balancer := NewLeastConnectionsBalancer(backends)
    
    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            backend, err := balancer.NextBackend()
            if err != nil {
                b.Fatal(err)
            }
            
            // Simulate connection lifecycle
            balancer.StartConnection(backend)
            balancer.FinishConnection(backend)
        }
    })
}
```

### Memory Leak Testing

```go
func TestMemoryLeak_ConnectionTracking(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping memory leak test in short mode")
    }
    
    backends := []Backend{{URL: "http://test-backend.com", Weight: 1}}
    balancer := NewLeastConnectionsBalancer(backends)
    
    // Get initial memory stats
    var initialStats runtime.MemStats
    runtime.GC()
    runtime.ReadMemStats(&initialStats)
    
    // Simulate many connection starts/finishes
    backend := &backends[0]
    for i := 0; i < 100000; i++ {
        balancer.StartConnection(backend)
        if i%2 == 0 { // Simulate some connections finishing
            balancer.FinishConnection(backend)
        }
        
        if i%10000 == 0 {
            balancer.CleanupStaleConnections()
        }
    }
    
    // Force garbage collection and check memory
    runtime.GC()
    var finalStats runtime.MemStats
    runtime.ReadMemStats(&finalStats)
    
    // Memory growth should be reasonable (less than 10MB)
    memoryGrowth := finalStats.Alloc - initialStats.Alloc
    assert.Less(t, memoryGrowth, uint64(10*1024*1024), 
        "Memory growth should be less than 10MB, got %d bytes", memoryGrowth)
    
    // Connection count should be reasonable
    status := balancer.GetBackendsStatus()
    backendStatus := status[0]
    assert.Less(t, backendStatus.ActiveConns, int32(1000),
        "Connection count should be reasonable after cleanup")
}
```

### Stress Testing

```go
func TestStress_HighConcurrencyLoadBalancing(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping stress test in short mode")
    }
    
    backends := make([]Backend, 20)
    for i := range backends {
        backends[i] = Backend{
            URL:    fmt.Sprintf("http://backend%d.com", i),
            Weight: 1,
        }
    }
    
    balancer := NewRoundRobinBalancer(backends)
    
    const (
        numGoroutines      = 1000
        requestsPerRoutine = 1000
        totalRequests      = numGoroutines * requestsPerRoutine
    )
    
    results := make(chan string, totalRequests)
    errors := make(chan error, totalRequests)
    
    var wg sync.WaitGroup
    start := time.Now()
    
    // Launch concurrent goroutines
    for i := 0; i < numGoroutines; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            
            for j := 0; j < requestsPerRoutine; j++ {
                backend, err := balancer.NextBackend()
                if err != nil {
                    errors <- err
                    return
                }
                results <- backend.URL
            }
        }()
    }
    
    wg.Wait()
    close(results)
    close(errors)
    
    duration := time.Since(start)
    
    // Check for errors
    errorCount := len(errors)
    assert.Equal(t, 0, errorCount, "Should have no errors during stress test")
    
    // Verify all requests completed
    resultCount := len(results)
    assert.Equal(t, totalRequests, resultCount, "All requests should complete")
    
    // Performance assertion
    requestsPerSecond := float64(totalRequests) / duration.Seconds()
    assert.Greater(t, requestsPerSecond, 100000.0, 
        "Should handle >100k requests/sec, got %.2f", requestsPerSecond)
    
    t.Logf("Stress test completed: %d requests in %v (%.2f req/sec)", 
        totalRequests, duration, requestsPerSecond)
}
```

## Chaos Engineering

### Network Partition Simulation

```go
func TestChaos_NetworkPartitions(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping chaos test in short mode")
    }
    
    // Setup multiple nodes with Raft storage
    nodes := make([]*RaftStorage, 3)
    for i := 0; i < 3; i++ {
        config := RaftConfig{
            NodeID:      fmt.Sprintf("node-%d", i),
            BindAddress: fmt.Sprintf("127.0.0.1:%d", 9000+i),
            DataDir:     t.TempDir(),
            Peers:       []string{"node-0:9000", "node-1:9001", "node-2:9002"},
        }
        
        node, err := NewRaftStorage(config)
        require.NoError(t, err)
        nodes[i] = node
    }
    
    defer func() {
        for _, node := range nodes {
            node.Close()
        }
    }()
    
    // Wait for cluster formation
    time.Sleep(2 * time.Second)
    
    // Find leader
    var leader *RaftStorage
    for _, node := range nodes {
        if node.raft.State() == raft.Leader {
            leader = node
            break
        }
    }
    require.NotNil(t, leader)
    
    // Write initial data
    ctx := context.Background()
    err := leader.Set(ctx, "test-key", []byte("test-value"), 0)
    require.NoError(t, err)
    
    // Simulate network partition by stopping one node
    partitionedNode := nodes[2]
    partitionedNode.raft.Shutdown()
    
    // Continue operations with remaining nodes
    for i := 0; i < 10; i++ {
        key := fmt.Sprintf("partition-key-%d", i)
        err := leader.Set(ctx, key, []byte(fmt.Sprintf("value-%d", i)), 0)
        if err != nil {
            // Leadership might change during partition
            t.Logf("Write failed during partition: %v", err)
        }
    }
    
    // Verify cluster continues operating
    time.Sleep(time.Second)
    
    // Check that remaining nodes have consistent state
    for i := 0; i < 2; i++ {
        node := nodes[i]
        if node == partitionedNode {
            continue
        }
        
        value, err := node.Get(ctx, "test-key")
        if err == nil {
            assert.Equal(t, []byte("test-value"), value)
        }
    }
}

func TestChaos_BackendFlapping(t *testing.T) {
    // Setup backends that randomly fail
    flakyBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Randomly fail 30% of requests
        if rand.Float32() < 0.3 {
            w.WriteHeader(http.StatusInternalServerError)
            return
        }
        w.WriteHeader(http.StatusOK)
    }))
    defer flakyBackend.Close()
    
    stableBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))
    defer stableBackend.Close()
    
    backends := []Backend{
        {URL: flakyBackend.URL, Weight: 1},
        {URL: stableBackend.URL, Weight: 1},
    }
    
    balancer := NewRoundRobinBalancer(backends)
    
    config := HealthCheckConfig{
        Endpoint: "/-/ready",
        Interval: 50 * time.Millisecond,
        Timeout:  25 * time.Millisecond,
        Strategy: "circuit-breaker",
        Thresholds: HealthThresholds{
            Failure: 2,
            Success: 1,
        },
        CircuitBreaker: CircuitBreakerConfig{
            OpenTimeout:         200 * time.Millisecond,
            HalfOpenMaxRequests: 3,
        },
    }
    
    hc := NewHealthChecker(config, nil, testLogger())
    
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    err := hc.StartMonitoring(ctx)
    require.NoError(t, err)
    
    // Run for several seconds and track state changes
    stateChanges := make(map[string]int)
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            goto analysis
        case <-ticker.C:
            status := hc.GetBackendsStatus()
            for _, s := range status {
                stateChanges[s.Backend.URL+":"+s.State.String()]++
            }
        }
    }
    
analysis:
    // Verify that flaky backend experienced state changes
    flakyStateChanges := 0
    stableStateChanges := 0
    
    for state, count := range stateChanges {
        if strings.Contains(state, flakyBackend.URL) {
            flakyStateChanges += count
        } else if strings.Contains(state, stableBackend.URL) {
            stableStateChanges += count
        }
    }
    
    assert.Greater(t, flakyStateChanges, stableStateChanges,
        "Flaky backend should have more state changes than stable backend")
    
    t.Logf("Flaky backend state changes: %d, Stable backend: %d", 
        flakyStateChanges, stableStateChanges)
}
```

## Test Data Management

### Test Fixtures

```go
// TestDataManager helps manage test data and fixtures
type TestDataManager struct {
    tempDir string
}

func NewTestDataManager(t *testing.T) *TestDataManager {
    return &TestDataManager{
        tempDir: t.TempDir(),
    }
}

func (tdm *TestDataManager) CreateConfigFile(config interface{}) string {
    configBytes, err := yaml.Marshal(config)
    if err != nil {
        panic(err)
    }
    
    configFile := filepath.Join(tdm.tempDir, "test-config.yaml")
    err = os.WriteFile(configFile, configBytes, 0644)
    if err != nil {
        panic(err)
    }
    
    return configFile
}

func (tdm *TestDataManager) CreateBackendConfigs() []Backend {
    return []Backend{
        {URL: "http://backend1.example.com", Weight: 1},
        {URL: "http://backend2.example.com", Weight: 2}, 
        {URL: "http://backend3.example.com", Weight: 1},
    }
}

func (tdm *TestDataManager) CreateHealthCheckConfig() HealthCheckConfig {
    return HealthCheckConfig{
        Enabled:  true,
        Endpoint: "/-/ready",
        Interval: 100 * time.Millisecond,
        Timeout:  50 * time.Millisecond,
        Strategy: "exclude-unhealthy",
        Thresholds: HealthThresholds{
            Failure: 3,
            Success: 1,
        },
    }
}
```

### Mock Implementations

```go
// MockStateStorage for testing without external dependencies
type MockStateStorage struct {
    data   sync.Map
    events map[string][]chan StateEvent
    mu     sync.RWMutex
}

func NewMockStateStorage() *MockStateStorage {
    return &MockStateStorage{
        events: make(map[string][]chan StateEvent),
    }
}

func (mss *MockStateStorage) Get(ctx context.Context, key string) ([]byte, error) {
    if value, exists := mss.data.Load(key); exists {
        return value.([]byte), nil
    }
    return nil, ErrKeyNotFound
}

func (mss *MockStateStorage) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
    mss.data.Store(key, value)
    
    // Notify watchers
    event := StateEvent{
        Type:      EventSet,
        Key:       key,
        Value:     value,
        Timestamp: time.Now(),
        NodeID:    "mock-node",
    }
    
    mss.notifyWatchers(key, event)
    return nil
}

func (mss *MockStateStorage) Watch(ctx context.Context, keyPrefix string) (<-chan StateEvent, error) {
    ch := make(chan StateEvent, 100)
    
    mss.mu.Lock()
    mss.events[keyPrefix] = append(mss.events[keyPrefix], ch)
    mss.mu.Unlock()
    
    // Cleanup on context cancellation
    go func() {
        <-ctx.Done()
        mss.removeWatcher(keyPrefix, ch)
        close(ch)
    }()
    
    return ch, nil
}

// MockLogger for testing
type MockLogger struct {
    entries []LogEntry
    mu      sync.Mutex
}

type LogEntry struct {
    Level   string
    Message string
    Fields  map[string]interface{}
}

func NewMockLogger() *MockLogger {
    return &MockLogger{}
}

func (ml *MockLogger) Debug(msg string, fields ...Field) {
    ml.log("DEBUG", msg, fields)
}

func (ml *MockLogger) Info(msg string, fields ...Field) {
    ml.log("INFO", msg, fields)
}

func (ml *MockLogger) Warn(msg string, fields ...Field) {
    ml.log("WARN", msg, fields)
}

func (ml *MockLogger) Error(msg string, fields ...Field) {
    ml.log("ERROR", msg, fields)
}

func (ml *MockLogger) log(level, message string, fields []Field) {
    ml.mu.Lock()
    defer ml.mu.Unlock()
    
    fieldMap := make(map[string]interface{})
    for _, field := range fields {
        fieldMap[field.Key] = field.Value
    }
    
    ml.entries = append(ml.entries, LogEntry{
        Level:   level,
        Message: message,
        Fields:  fieldMap,
    })
}

func (ml *MockLogger) GetEntries() []LogEntry {
    ml.mu.Lock()
    defer ml.mu.Unlock()
    
    entries := make([]LogEntry, len(ml.entries))
    copy(entries, ml.entries)
    return entries
}

func (ml *MockLogger) HasEntry(level, message string) bool {
    ml.mu.Lock()
    defer ml.mu.Unlock()
    
    for _, entry := range ml.entries {
        if entry.Level == level && entry.Message == message {
            return true
        }
    }
    return false
}
```

## Test Execution and CI/CD

### Test Commands

```bash
# Run all tests
make test

# Run unit tests only
make test-unit

# Run integration tests (requires Redis/external deps)
make test-integration

# Run performance benchmarks
make benchmark

# Run tests with race detection
make test-race

# Run tests with coverage
make test-coverage

# Run specific test packages
go test -v ./internal/services/loadbalancer/...
go test -v ./internal/services/healthchecker/...

# Run tests matching pattern
go test -v -run TestLoadBalancer ./...

# Run benchmarks with memory profiling
go test -bench=. -memprofile=mem.prof ./internal/services/loadbalancer/
```

### Makefile Integration

```makefile
.PHONY: test test-unit test-integration test-race test-coverage benchmark

test: test-unit test-integration

test-unit:
	@echo "Running unit tests..."
	go test -short -v ./...

test-integration:
	@echo "Running integration tests..."
	@if [ -z "$(SKIP_INTEGRATION)" ]; then \
		go test -v -tags=integration ./...; \
	else \
		echo "Skipping integration tests (SKIP_INTEGRATION set)"; \
	fi

test-race:
	@echo "Running tests with race detection..."
	go test -race -short ./...

test-coverage:
	@echo "Running tests with coverage..."
	go test -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

benchmark:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./internal/services/loadbalancer/
	go test -bench=. -benchmem ./internal/services/healthchecker/
	go test -bench=. -benchmem ./internal/services/statestorage/

# Run specific test categories
test-loadbalancer:
	go test -v ./internal/services/loadbalancer/...

test-healthchecker:
	go test -v ./internal/services/healthchecker/...

test-statestorage:
	go test -v ./internal/services/statestorage/...

# Performance profiling
profile-cpu:
	go test -cpuprofile=cpu.prof -bench=. ./internal/services/loadbalancer/
	go tool pprof -http=:8080 cpu.prof

profile-mem:
	go test -memprofile=mem.prof -bench=. ./internal/services/loadbalancer/
	go tool pprof -http=:8080 mem.prof
```

### CI/CD Pipeline Configuration

```yaml
# .github/workflows/test.yml
name: Test Suite

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    
    services:
      redis:
        image: redis:7-alpine
        ports:
          - 6379:6379
        options: --health-cmd "redis-cli ping" --health-interval 10s --health-timeout 5s --health-retries 5
    
    steps:
    - uses: actions/checkout@v4
    
    - name: Set up Go
      uses: actions/setup-go@v4
      with:
        go-version: '1.21'
    
    - name: Cache Go modules
      uses: actions/cache@v3
      with:
        path: |
          ~/.cache/go-build
          ~/go/pkg/mod
        key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
        restore-keys: |
          ${{ runner.os }}-go-
    
    - name: Run unit tests
      run: make test-unit
    
    - name: Run integration tests
      run: make test-integration
      env:
        REDIS_URL: localhost:6379
    
    - name: Run tests with race detection
      run: make test-race
    
    - name: Generate coverage
      run: make test-coverage
    
    - name: Upload coverage to Codecov
      uses: codecov/codecov-action@v3
      with:
        file: ./coverage.out
        flags: unittests
        name: codecov-umbrella
  
  benchmark:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v4
    
    - name: Set up Go
      uses: actions/setup-go@v4
      with:
        go-version: '1.21'
    
    - name: Run benchmarks
      run: |
        make benchmark | tee benchmark.txt
        
    - name: Store benchmark result
      uses: benchmark-action/github-action-benchmark@v1
      with:
        tool: 'go'
        output-file-path: benchmark.txt
        github-token: ${{ secrets.GITHUB_TOKEN }}
        auto-push: true
```

## Best Practices and Guidelines

### Test Organization

1. **Package Structure:**
   - Keep tests in same package as code (`_test.go` files)
   - Use separate test packages for integration tests (`package foo_test`)
   - Group related tests in test suites

2. **Test Naming:**
   - `TestFunctionName_Scenario` for unit tests
   - `TestIntegration_FeatureName` for integration tests
   - `BenchmarkFunctionName_Scenario` for benchmarks

3. **Test Data:**
   - Use table-driven tests for multiple scenarios
   - Create helper functions for test data generation
   - Use testify/assert and testify/require for assertions

### Coverage Goals

- **Overall Coverage**: >85%
- **Critical Path Coverage**: >95%
- **New Code Coverage**: >90%

### Performance Targets

- **Load Balancer Selection**: <1μs per operation
- **Health Check**: <100ms per check
- **State Storage**: <10ms for local, <50ms for Redis, <100ms for Raft

### Testing Anti-patterns to Avoid

1. **Don't test implementation details** - Test behavior, not internals
2. **Avoid test interdependencies** - Each test should be independent
3. **Don't ignore test failures** - Fix or skip with explanation
4. **Avoid overly complex test setup** - Keep setup simple and focused
5. **Don't forget cleanup** - Always clean up resources in teardown