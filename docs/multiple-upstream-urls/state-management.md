# State Management and Distributed Storage

## Overview

This document describes the state management system for distributing backend health, load balancing state, and configuration across multiple instances of the vm-proxy-auth service. The system supports local storage for single-instance deployments and distributed storage for multi-instance high-availability setups.

## State Storage Architecture

### Storage Interface

```go
type StateStorage interface {
    // Basic key-value operations
    Get(ctx context.Context, key string) ([]byte, error)
    Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    
    // Batch operations for efficiency
    GetMultiple(ctx context.Context, keys []string) (map[string][]byte, error)
    SetMultiple(ctx context.Context, items map[string][]byte, ttl time.Duration) error
    
    // Watch for changes (distributed coordination)
    Watch(ctx context.Context, keyPrefix string) (<-chan StateEvent, error)
    
    // Lifecycle management
    Close() error
    
    // Health checking
    Ping(ctx context.Context) error
}

type StateEvent struct {
    Type      EventType // SET, DELETE
    Key       string
    Value     []byte
    Timestamp time.Time
    NodeID    string
}

type EventType int

const (
    EventSet EventType = iota
    EventDelete
)
```

### State Data Models

```go
// Backend health state
type BackendHealthState struct {
    URL              string        `json:"url"`
    State            BackendState  `json:"state"`
    LastCheck        time.Time     `json:"last_check"`
    LastHealthy      time.Time     `json:"last_healthy"`
    ConsecutiveFails int           `json:"consecutive_fails"`
    ConsecutiveOKs   int           `json:"consecutive_oks"`
    RateLimitCount   int           `json:"rate_limit_count"`
    UpdatedBy        string        `json:"updated_by"`
    UpdatedAt        time.Time     `json:"updated_at"`
    Version          int64         `json:"version"`
}

// Load balancer state
type LoadBalancerState struct {
    Strategy         string            `json:"strategy"`
    BackendStates    map[string]int    `json:"backend_states"`   // URL -> connection count
    LastRotation     int32             `json:"last_rotation"`    // round-robin position
    WeightedCounters map[string]int    `json:"weighted_counters"` // URL -> current weight
    UpdatedBy        string            `json:"updated_by"`
    UpdatedAt        time.Time         `json:"updated_at"`
    Version          int64             `json:"version"`
}

// Circuit breaker state
type CircuitBreakerState struct {
    BackendURL          string                `json:"backend_url"`
    State               CircuitBreakerState   `json:"state"`
    LastStateChange     time.Time             `json:"last_state_change"`
    ConsecutiveFails    int32                 `json:"consecutive_fails"`
    ConsecutiveSuccess  int32                 `json:"consecutive_success"`
    RequestsInHalfOpen  int32                 `json:"requests_in_half_open"`
    UpdatedBy           string                `json:"updated_by"`
    UpdatedAt           time.Time             `json:"updated_at"`
    Version             int64                 `json:"version"`
}
```

## Storage Implementations

### 1. Local Storage (Default)

**Use Cases:**
- Single instance deployments
- Development and testing
- Edge deployments without distributed coordination needs

**Implementation:**
```go
type LocalStorage struct {
    data    sync.Map
    watches map[string][]chan StateEvent
    mu      sync.RWMutex
    nodeID  string
}

func NewLocalStorage(nodeID string) *LocalStorage {
    return &LocalStorage{
        watches: make(map[string][]chan StateEvent),
        nodeID:  nodeID,
    }
}

func (ls *LocalStorage) Get(ctx context.Context, key string) ([]byte, error) {
    if value, exists := ls.data.Load(key); exists {
        if item, ok := value.(*storageItem); ok && !item.isExpired() {
            return item.value, nil
        }
        // Clean up expired item
        ls.data.Delete(key)
    }
    return nil, ErrKeyNotFound
}

func (ls *LocalStorage) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
    item := &storageItem{
        value:     make([]byte, len(value)),
        createdAt: time.Now(),
        ttl:       ttl,
    }
    copy(item.value, value)
    
    ls.data.Store(key, item)
    
    // Notify watchers
    ls.notifyWatchers(key, StateEvent{
        Type:      EventSet,
        Key:       key,
        Value:     value,
        Timestamp: time.Now(),
        NodeID:    ls.nodeID,
    })
    
    return nil
}

func (ls *LocalStorage) Watch(ctx context.Context, keyPrefix string) (<-chan StateEvent, error) {
    ch := make(chan StateEvent, 100)
    
    ls.mu.Lock()
    ls.watches[keyPrefix] = append(ls.watches[keyPrefix], ch)
    ls.mu.Unlock()
    
    // Cleanup on context cancellation
    go func() {
        <-ctx.Done()
        ls.removeWatcher(keyPrefix, ch)
        close(ch)
    }()
    
    return ch, nil
}

type storageItem struct {
    value     []byte
    createdAt time.Time
    ttl       time.Duration
}

func (si *storageItem) isExpired() bool {
    if si.ttl == 0 {
        return false // No expiration
    }
    return time.Since(si.createdAt) > si.ttl
}
```

### 2. Redis Storage

**Use Cases:**
- Multi-instance deployments with Redis infrastructure
- Existing Redis-based architectures
- Need for persistence and backup

**Implementation:**
```go
type RedisStorage struct {
    client      redis.UniversalClient
    keyPrefix   string
    pubsub      *redis.PubSub
    nodeID      string
    watchChans  map[string][]chan StateEvent
    mu          sync.RWMutex
    stopCh      chan struct{}
    wg          sync.WaitGroup
}

func NewRedisStorage(config RedisConfig) (*RedisStorage, error) {
    rdb := redis.NewUniversalClient(&redis.UniversalOptions{
        Addrs:        []string{config.Address},
        Password:     config.Password,
        DB:           config.Database,
        MaxRetries:   config.MaxRetries,
        DialTimeout:  config.DialTimeout,
        ReadTimeout:  config.ReadTimeout,
        WriteTimeout: config.WriteTimeout,
        PoolSize:     config.PoolSize,
        MinIdleConns: config.MinIdleConns,
    })
    
    // Test connection
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    if err := rdb.Ping(ctx).Err(); err != nil {
        return nil, fmt.Errorf("failed to connect to Redis: %w", err)
    }
    
    rs := &RedisStorage{
        client:     rdb,
        keyPrefix:  config.KeyPrefix,
        nodeID:     config.NodeID,
        watchChans: make(map[string][]chan StateEvent),
        stopCh:     make(chan struct{}),
    }
    
    // Start pub/sub for watching
    if err := rs.startPubSub(); err != nil {
        return nil, err
    }
    
    return rs, nil
}

func (rs *RedisStorage) Get(ctx context.Context, key string) ([]byte, error) {
    fullKey := rs.keyPrefix + key
    result, err := rs.client.Get(ctx, fullKey).Bytes()
    
    if err == redis.Nil {
        return nil, ErrKeyNotFound
    }
    if err != nil {
        return nil, fmt.Errorf("redis get failed: %w", err)
    }
    
    return result, nil
}

func (rs *RedisStorage) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
    fullKey := rs.keyPrefix + key
    
    // Use pipeline for atomic operations
    pipe := rs.client.Pipeline()
    
    if ttl > 0 {
        pipe.SetEX(ctx, fullKey, value, ttl)
    } else {
        pipe.Set(ctx, fullKey, value, 0)
    }
    
    // Publish change notification
    event := StateEvent{
        Type:      EventSet,
        Key:       key,
        Value:     value,
        Timestamp: time.Now(),
        NodeID:    rs.nodeID,
    }
    
    eventData, _ := json.Marshal(event)
    pipe.Publish(ctx, rs.keyPrefix+"events:"+key, eventData)
    
    _, err := pipe.Exec(ctx)
    if err != nil {
        return fmt.Errorf("redis set failed: %w", err)
    }
    
    return nil
}

func (rs *RedisStorage) Watch(ctx context.Context, keyPrefix string) (<-chan StateEvent, error) {
    ch := make(chan StateEvent, 100)
    
    rs.mu.Lock()
    rs.watchChans[keyPrefix] = append(rs.watchChans[keyPrefix], ch)
    rs.mu.Unlock()
    
    // Start subscription for this key prefix
    rs.subscribeToEvents(keyPrefix)
    
    // Cleanup on context cancellation
    go func() {
        <-ctx.Done()
        rs.removeWatcher(keyPrefix, ch)
        close(ch)
    }()
    
    return ch, nil
}

func (rs *RedisStorage) startPubSub() error {
    rs.pubsub = rs.client.Subscribe(context.Background())
    
    rs.wg.Add(1)
    go rs.handlePubSubMessages()
    
    return nil
}

func (rs *RedisStorage) handlePubSubMessages() {
    defer rs.wg.Done()
    
    ch := rs.pubsub.Channel()
    for {
        select {
        case msg := <-ch:
            rs.processEventMessage(msg)
        case <-rs.stopCh:
            return
        }
    }
}
```

### 3. Raft Storage

**Use Cases:**
- Multi-instance deployments without external dependencies
- Consensus-based state management
- Strong consistency requirements

**Implementation:**
```go
type RaftStorage struct {
    raft        *raft.Raft
    fsm         *raftFSM
    transport   *raft.NetworkTransport
    logStore    raft.LogStore
    stableStore raft.StableStore
    snapStore   raft.SnapshotStore
    config      RaftConfig
    nodeID      string
    watchChans  map[string][]chan StateEvent
    mu          sync.RWMutex
}

func NewRaftStorage(config RaftConfig) (*RaftStorage, error) {
    // Setup Raft configuration
    raftConfig := raft.DefaultConfig()
    raftConfig.LocalID = raft.ServerID(config.NodeID)
    raftConfig.HeartbeatTimeout = config.HeartbeatTimeout
    raftConfig.ElectionTimeout = config.ElectionTimeout
    raftConfig.LeaderLeaseTimeout = config.LeaderLeaseTimeout
    raftConfig.CommitTimeout = config.CommitTimeout
    
    // Create transport
    addr, err := net.ResolveTCPAddr("tcp", config.BindAddress)
    if err != nil {
        return nil, fmt.Errorf("failed to resolve bind address: %w", err)
    }
    
    transport, err := raft.NewTCPTransport(config.BindAddress, addr, 3, 10*time.Second, os.Stderr)
    if err != nil {
        return nil, fmt.Errorf("failed to create transport: %w", err)
    }
    
    // Create stores
    logStore, err := raftboltdb.NewBoltStore(filepath.Join(config.DataDir, "raft-log.db"))
    if err != nil {
        return nil, fmt.Errorf("failed to create log store: %w", err)
    }
    
    stableStore, err := raftboltdb.NewBoltStore(filepath.Join(config.DataDir, "raft-stable.db"))
    if err != nil {
        return nil, fmt.Errorf("failed to create stable store: %w", err)
    }
    
    snapStore, err := raft.NewFileSnapshotStore(config.DataDir, 3, os.Stderr)
    if err != nil {
        return nil, fmt.Errorf("failed to create snapshot store: %w", err)
    }
    
    // Create FSM
    fsm := &raftFSM{
        data:       make(map[string][]byte),
        watchChans: make(map[string][]chan StateEvent),
    }
    
    // Create Raft instance
    r, err := raft.NewRaft(raftConfig, fsm, logStore, stableStore, snapStore, transport)
    if err != nil {
        return nil, fmt.Errorf("failed to create raft instance: %w", err)
    }
    
    rs := &RaftStorage{
        raft:        r,
        fsm:         fsm,
        transport:   transport,
        logStore:    logStore,
        stableStore: stableStore,
        snapStore:   snapStore,
        config:      config,
        nodeID:      config.NodeID,
        watchChans:  make(map[string][]chan StateEvent),
    }
    
    // Bootstrap cluster if needed
    if err := rs.bootstrapCluster(); err != nil {
        return nil, err
    }
    
    return rs, nil
}

func (rs *RaftStorage) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
    if rs.raft.State() != raft.Leader {
        return ErrNotLeader
    }
    
    cmd := raftCommand{
        Type:  raftCommandSet,
        Key:   key,
        Value: value,
        TTL:   ttl,
        NodeID: rs.nodeID,
    }
    
    data, err := json.Marshal(cmd)
    if err != nil {
        return err
    }
    
    future := rs.raft.Apply(data, 10*time.Second)
    if err := future.Error(); err != nil {
        return fmt.Errorf("failed to apply raft command: %w", err)
    }
    
    return nil
}

func (rs *RaftStorage) Get(ctx context.Context, key string) ([]byte, error) {
    return rs.fsm.get(key)
}

// Raft FSM implementation
type raftFSM struct {
    data       map[string][]byte
    watchChans map[string][]chan StateEvent
    mu         sync.RWMutex
}

type raftCommand struct {
    Type   raftCommandType `json:"type"`
    Key    string          `json:"key"`
    Value  []byte          `json:"value"`
    TTL    time.Duration   `json:"ttl"`
    NodeID string          `json:"node_id"`
}

type raftCommandType int

const (
    raftCommandSet raftCommandType = iota
    raftCommandDelete
)

func (f *raftFSM) Apply(log *raft.Log) interface{} {
    var cmd raftCommand
    if err := json.Unmarshal(log.Data, &cmd); err != nil {
        return err
    }
    
    f.mu.Lock()
    defer f.mu.Unlock()
    
    switch cmd.Type {
    case raftCommandSet:
        f.data[cmd.Key] = cmd.Value
        
        // Notify watchers
        event := StateEvent{
            Type:      EventSet,
            Key:       cmd.Key,
            Value:     cmd.Value,
            Timestamp: time.Now(),
            NodeID:    cmd.NodeID,
        }
        f.notifyWatchers(cmd.Key, event)
        
    case raftCommandDelete:
        delete(f.data, cmd.Key)
        
        event := StateEvent{
            Type:      EventDelete,
            Key:       cmd.Key,
            Timestamp: time.Now(),
            NodeID:    cmd.NodeID,
        }
        f.notifyWatchers(cmd.Key, event)
    }
    
    return nil
}

func (f *raftFSM) Snapshot() (raft.FSMSnapshot, error) {
    f.mu.Lock()
    defer f.mu.Unlock()
    
    // Create a copy of the data
    data := make(map[string][]byte)
    for k, v := range f.data {
        data[k] = make([]byte, len(v))
        copy(data[k], v)
    }
    
    return &raftSnapshot{data: data}, nil
}

func (f *raftFSM) Restore(snapshot io.ReadCloser) error {
    defer snapshot.Close()
    
    var data map[string][]byte
    decoder := json.NewDecoder(snapshot)
    if err := decoder.Decode(&data); err != nil {
        return err
    }
    
    f.mu.Lock()
    f.data = data
    f.mu.Unlock()
    
    return nil
}
```

## State Synchronization Patterns

### Backend Health State Sync

```go
type BackendHealthSync struct {
    storage    StateStorage
    nodeID     string
    logger     domain.Logger
    syncPeriod time.Duration
    stopCh     chan struct{}
}

func (bhs *BackendHealthSync) SyncBackendHealth(backend Backend, health *BackendHealth) error {
    state := BackendHealthState{
        URL:              backend.URL,
        State:            health.State,
        LastCheck:        health.LastCheck,
        LastHealthy:      health.LastHealthy,
        ConsecutiveFails: health.ConsecutiveFails,
        ConsecutiveOKs:   health.ConsecutiveOKs,
        UpdatedBy:        bhs.nodeID,
        UpdatedAt:        time.Now(),
        Version:          health.Version,
    }
    
    data, err := json.Marshal(state)
    if err != nil {
        return err
    }
    
    key := fmt.Sprintf("backend:health:%s", backend.URL)
    return bhs.storage.Set(context.Background(), key, data, 5*time.Minute)
}

func (bhs *BackendHealthSync) LoadBackendHealth(backendURL string) (*BackendHealthState, error) {
    key := fmt.Sprintf("backend:health:%s", backendURL)
    data, err := bhs.storage.Get(context.Background(), key)
    if err != nil {
        return nil, err
    }
    
    var state BackendHealthState
    if err := json.Unmarshal(data, &state); err != nil {
        return nil, err
    }
    
    return &state, nil
}

func (bhs *BackendHealthSync) WatchBackendHealth(ctx context.Context) (<-chan BackendHealthState, error) {
    events, err := bhs.storage.Watch(ctx, "backend:health:")
    if err != nil {
        return nil, err
    }
    
    ch := make(chan BackendHealthState, 100)
    
    go func() {
        defer close(ch)
        for event := range events {
            if event.Type != EventSet {
                continue
            }
            
            var state BackendHealthState
            if err := json.Unmarshal(event.Value, &state); err != nil {
                bhs.logger.Error("Failed to unmarshal backend health state",
                    Field{Key: "error", Value: err},
                    Field{Key: "key", Value: event.Key})
                continue
            }
            
            ch <- state
        }
    }()
    
    return ch, nil
}
```

### Load Balancer State Sync

```go
type LoadBalancerStateSync struct {
    storage   StateStorage
    nodeID    string
    logger    domain.Logger
    mu        sync.RWMutex
}

func (lbs *LoadBalancerStateSync) SyncConnectionCounts(counts map[string]int32) error {
    state := LoadBalancerState{
        Strategy:      "least-connections",
        BackendStates: make(map[string]int, len(counts)),
        UpdatedBy:     lbs.nodeID,
        UpdatedAt:     time.Now(),
    }
    
    for backend, count := range counts {
        state.BackendStates[backend] = int(count)
    }
    
    data, err := json.Marshal(state)
    if err != nil {
        return err
    }
    
    key := "loadbalancer:state"
    return lbs.storage.Set(context.Background(), key, data, time.Minute)
}

func (lbs *LoadBalancerStateSync) GetAggregatedCounts() (map[string]int32, error) {
    // Get all load balancer states from different nodes
    key := "loadbalancer:state"
    data, err := lbs.storage.Get(context.Background(), key)
    if err != nil {
        return nil, err
    }
    
    var state LoadBalancerState
    if err := json.Unmarshal(data, &state); err != nil {
        return nil, err
    }
    
    counts := make(map[string]int32)
    for backend, count := range state.BackendStates {
        counts[backend] = int32(count)
    }
    
    return counts, nil
}
```

## Conflict Resolution

### Version-based Conflict Resolution

```go
type ConflictResolver struct {
    logger domain.Logger
}

func (cr *ConflictResolver) ResolveBackendHealthConflict(local, remote BackendHealthState) BackendHealthState {
    // Use vector clocks or timestamps for ordering
    if remote.Version > local.Version {
        cr.logger.Debug("Using remote state (newer version)",
            Field{Key: "local_version", Value: local.Version},
            Field{Key: "remote_version", Value: remote.Version})
        return remote
    }
    
    if local.Version > remote.Version {
        cr.logger.Debug("Using local state (newer version)",
            Field{Key: "local_version", Value: local.Version},
            Field{Key: "remote_version", Value: remote.Version})
        return local
    }
    
    // Same version, use timestamp
    if remote.UpdatedAt.After(local.UpdatedAt) {
        return remote
    }
    
    // Prefer unhealthy state for safety in conflicts
    if local.State == BackendUnhealthy || remote.State == BackendUnhealthy {
        if local.State == BackendUnhealthy {
            cr.logger.Debug("Preferring unhealthy state for safety")
            return local
        }
        return remote
    }
    
    return local
}

// Last-writer-wins with safety bias
func (cr *ConflictResolver) ResolveCircuitBreakerConflict(local, remote CircuitBreakerState) CircuitBreakerState {
    // Always prefer OPEN state for safety
    if local.State == CircuitOpen || remote.State == CircuitOpen {
        if local.State == CircuitOpen {
            return local
        }
        return remote
    }
    
    // Use most recent state change
    if remote.LastStateChange.After(local.LastStateChange) {
        return remote
    }
    
    return local
}
```

### Distributed Locks for Critical Operations

```go
type DistributedLock struct {
    storage   StateStorage
    lockKey   string
    nodeID    string
    ttl       time.Duration
    renewable bool
}

func NewDistributedLock(storage StateStorage, lockKey, nodeID string, ttl time.Duration) *DistributedLock {
    return &DistributedLock{
        storage: storage,
        lockKey: lockKey,
        nodeID:  nodeID,
        ttl:     ttl,
    }
}

func (dl *DistributedLock) Acquire(ctx context.Context) error {
    lockData := map[string]interface{}{
        "node_id":    dl.nodeID,
        "acquired_at": time.Now(),
        "ttl":       dl.ttl,
    }
    
    data, _ := json.Marshal(lockData)
    
    // Try to acquire lock atomically
    existing, err := dl.storage.Get(ctx, dl.lockKey)
    if err != nil && err != ErrKeyNotFound {
        return err
    }
    
    if existing != nil {
        // Lock exists, check if it's expired
        var existingLock map[string]interface{}
        if err := json.Unmarshal(existing, &existingLock); err == nil {
            if acquiredAt, ok := existingLock["acquired_at"].(string); ok {
                if t, err := time.Parse(time.RFC3339, acquiredAt); err == nil {
                    if time.Since(t) < dl.ttl {
                        return ErrLockAlreadyHeld
                    }
                }
            }
        }
    }
    
    // Set lock with TTL
    return dl.storage.Set(ctx, dl.lockKey, data, dl.ttl)
}

func (dl *DistributedLock) Release(ctx context.Context) error {
    // Verify we own the lock before releasing
    data, err := dl.storage.Get(ctx, dl.lockKey)
    if err != nil {
        return err
    }
    
    var lockData map[string]interface{}
    if err := json.Unmarshal(data, &lockData); err != nil {
        return err
    }
    
    if nodeID, ok := lockData["node_id"].(string); !ok || nodeID != dl.nodeID {
        return ErrNotLockOwner
    }
    
    return dl.storage.Delete(ctx, dl.lockKey)
}
```

## Monitoring and Metrics

### State Storage Metrics

```go
type StateStorageMetrics struct {
    OperationsTotal    *prometheus.CounterVec
    OperationDuration  *prometheus.HistogramVec
    StorageSize        *prometheus.GaugeVec
    ConnectionStatus   *prometheus.GaugeVec
    SyncEvents         *prometheus.CounterVec
}

func NewStateStorageMetrics() *StateStorageMetrics {
    return &StateStorageMetrics{
        OperationsTotal: prometheus.NewCounterVec(
            prometheus.CounterOpts{
                Name: "vm_proxy_auth_state_storage_operations_total",
                Help: "Total number of state storage operations",
            },
            []string{"storage_type", "operation", "result"},
        ),
        OperationDuration: prometheus.NewHistogramVec(
            prometheus.HistogramOpts{
                Name:    "vm_proxy_auth_state_storage_operation_duration_seconds",
                Help:    "Duration of state storage operations",
                Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
            },
            []string{"storage_type", "operation"},
        ),
        StorageSize: prometheus.NewGaugeVec(
            prometheus.GaugeOpts{
                Name: "vm_proxy_auth_state_storage_size",
                Help: "Current size of state storage",
            },
            []string{"storage_type"},
        ),
        ConnectionStatus: prometheus.NewGaugeVec(
            prometheus.GaugeOpts{
                Name: "vm_proxy_auth_state_storage_connection_status",
                Help: "Connection status to state storage (1=connected, 0=disconnected)",
            },
            []string{"storage_type"},
        ),
    }
}
```

## Performance Considerations

### Batch Operations

```go
func (rs *RedisStorage) SetMultiple(ctx context.Context, items map[string][]byte, ttl time.Duration) error {
    pipe := rs.client.Pipeline()
    
    for key, value := range items {
        fullKey := rs.keyPrefix + key
        if ttl > 0 {
            pipe.SetEX(ctx, fullKey, value, ttl)
        } else {
            pipe.Set(ctx, fullKey, value, 0)
        }
    }
    
    _, err := pipe.Exec(ctx)
    return err
}
```

### Connection Pooling

```go
type PooledRedisStorage struct {
    *RedisStorage
    pool *redis.Pool
}

func (prs *PooledRedisStorage) executeWithRetry(ctx context.Context, operation func(redis.UniversalClient) error) error {
    var lastErr error
    
    for attempts := 0; attempts < 3; attempts++ {
        if err := operation(prs.client); err == nil {
            return nil
        } else {
            lastErr = err
            if !isRetryableError(err) {
                break
            }
            
            // Exponential backoff
            backoff := time.Duration(attempts) * time.Millisecond * 100
            time.Sleep(backoff)
        }
    }
    
    return lastErr
}
```

### Memory Management

```go
func (ls *LocalStorage) cleanup() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            ls.cleanupExpiredKeys()
        case <-ls.stopCh:
            return
        }
    }
}

func (ls *LocalStorage) cleanupExpiredKeys() {
    ls.data.Range(func(key, value interface{}) bool {
        if item, ok := value.(*storageItem); ok && item.isExpired() {
            ls.data.Delete(key)
        }
        return true
    })
}
```

## Testing Strategies

### Unit Testing

```go
func TestLocalStorage_BasicOperations(t *testing.T) {
    storage := NewLocalStorage("test-node")
    ctx := context.Background()
    
    // Test Set/Get
    key := "test-key"
    value := []byte("test-value")
    
    err := storage.Set(ctx, key, value, time.Minute)
    assert.NoError(t, err)
    
    retrieved, err := storage.Get(ctx, key)
    assert.NoError(t, err)
    assert.Equal(t, value, retrieved)
    
    // Test TTL expiration
    err = storage.Set(ctx, "expire-key", []byte("expire-value"), time.Millisecond)
    assert.NoError(t, err)
    
    time.Sleep(10 * time.Millisecond)
    
    _, err = storage.Get(ctx, "expire-key")
    assert.Equal(t, ErrKeyNotFound, err)
}

func TestRedisStorage_Concurrency(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping Redis integration test in short mode")
    }
    
    config := RedisConfig{
        Address: "localhost:6379",
        NodeID:  "test-node",
    }
    
    storage, err := NewRedisStorage(config)
    require.NoError(t, err)
    defer storage.Close()
    
    ctx := context.Background()
    var wg sync.WaitGroup
    
    // Concurrent writes
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            key := fmt.Sprintf("concurrent-key-%d", i)
            value := []byte(fmt.Sprintf("value-%d", i))
            
            err := storage.Set(ctx, key, value, time.Minute)
            assert.NoError(t, err)
        }(i)
    }
    
    wg.Wait()
    
    // Verify all values
    for i := 0; i < 100; i++ {
        key := fmt.Sprintf("concurrent-key-%d", i)
        expected := []byte(fmt.Sprintf("value-%d", i))
        
        value, err := storage.Get(ctx, key)
        assert.NoError(t, err)
        assert.Equal(t, expected, value)
    }
}
```

### Integration Testing

```go
func TestRaftStorage_ClusterFormation(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping Raft cluster test in short mode")
    }
    
    // Create 3-node Raft cluster
    nodes := make([]*RaftStorage, 3)
    
    for i := 0; i < 3; i++ {
        tempDir := t.TempDir()
        config := RaftConfig{
            NodeID:      fmt.Sprintf("node-%d", i),
            BindAddress: fmt.Sprintf("127.0.0.1:%d", 9000+i),
            DataDir:     tempDir,
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
    
    // Wait for leader election
    time.Sleep(2 * time.Second)
    
    // Find leader
    var leader *RaftStorage
    for _, node := range nodes {
        if node.raft.State() == raft.Leader {
            leader = node
            break
        }
    }
    
    require.NotNil(t, leader, "No leader elected")
    
    // Test replication
    ctx := context.Background()
    key := "test-replication"
    value := []byte("replicated-value")
    
    err := leader.Set(ctx, key, value, 0)
    require.NoError(t, err)
    
    // Wait for replication
    time.Sleep(100 * time.Millisecond)
    
    // Verify all nodes have the data
    for i, node := range nodes {
        retrieved, err := node.Get(ctx, key)
        assert.NoError(t, err, "Node %d should have replicated data", i)
        assert.Equal(t, value, retrieved, "Node %d has incorrect data", i)
    }
}
```

## Best Practices

### Configuration Guidelines

1. **Storage Selection:**
   - Local: Single instance, development
   - Redis: Multi-instance with existing Redis infrastructure
   - Raft: Multi-instance without external dependencies

2. **TTL Settings:**
   - Backend health state: 2-5 minutes
   - Load balancer state: 30-60 seconds
   - Circuit breaker state: 1-2 minutes

3. **Batch Operations:**
   - Use batching for multiple related updates
   - Batch size should not exceed network MTU considerations
   - Implement proper error handling for partial failures

### Monitoring and Alerting

1. **Critical Alerts:**
   - State storage connection failures
   - High operation latency
   - Raft cluster split-brain scenarios

2. **Warning Alerts:**
   - Increased conflict resolution frequency
   - Growing state storage size
   - Watch channel buffer overflows

### Security Considerations

1. **Redis Security:**
   - Enable authentication
   - Use TLS for connections
   - Network isolation for Redis instances

2. **Raft Security:**
   - Enable TLS for inter-node communication
   - Secure file system permissions for data directories
   - Regular backup of Raft logs

3. **Access Control:**
   - Implement proper key prefixing
   - Use dedicated storage instances per environment
   - Regular rotation of credentials