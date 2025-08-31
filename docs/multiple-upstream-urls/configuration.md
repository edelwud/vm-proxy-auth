# Configuration: Multiple Upstream URLs

## Overview

This document defines the configuration format and validation rules for the multiple upstream URLs feature. The configuration extends the existing Viper-based system while maintaining backward compatibility during the transition period.

## Configuration Schema

### Top-Level Configuration Structure

```yaml
# Global state storage configuration
stateStorage:
  type: "local"  # local, redis, raft
  redis:
    address: "localhost:6379"
    password: ""
    database: 0
    keyPrefix: "vm-proxy-auth:backends:"
    maxRetries: 3
    dialTimeout: "5s"
  raft:
    nodeId: "node-1"
    bindAddress: "127.0.0.1:8081"
    peers: ["node-1:8081", "node-2:8081", "node-3:8081"]
    dataDir: "/var/lib/vm-proxy-auth/raft"
    snapshotInterval: "5m"
    snapshotThreshold: 1000

# Request queue configuration
requestQueue:
  enabled: true
  maxSize: 1000
  timeout: "30s"
  workers: 10
  drainTimeout: "10s"

# Enhanced upstream configuration
upstream:
  backends:
    - url: "https://vmselect-1.example.com"
      weight: 1
    - url: "https://vmselect-2.example.com"
      weight: 2
    - url: "https://vmselect-3.example.com"
      weight: 1
  strategy: "weighted-round-robin"
  healthCheck:
    enabled: true
    endpoint: "/-/ready"
    interval: "30s"
    timeout: "5s"
    strategy: "circuit-breaker"
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
    maxBackoffDelay: "30s"
    retryableErrors:
      - "connection_timeout"
      - "connection_refused"
      - "5xx_status"
```

## Configuration Sections

### State Storage Configuration

#### Local Storage (Default)
```yaml
stateStorage:
  type: "local"
```

**Properties:**
- No additional configuration required
- Suitable for single-instance deployments
- State is not shared between instances

#### Redis Storage
```yaml
stateStorage:
  type: "redis"
  redis:
    address: "localhost:6379"         # Required: Redis server address
    password: ""                      # Optional: Redis password
    database: 0                       # Optional: Redis database number (default: 0)
    keyPrefix: "vm-proxy-auth:backends:"  # Optional: Key prefix for namespacing
    maxRetries: 3                     # Optional: Max connection retries (default: 3)
    dialTimeout: "5s"                 # Optional: Connection timeout (default: 5s)
    readTimeout: "3s"                 # Optional: Read timeout (default: 3s)
    writeTimeout: "3s"                # Optional: Write timeout (default: 3s)
    poolSize: 10                      # Optional: Connection pool size (default: 10)
    minIdleConns: 5                   # Optional: Min idle connections (default: 5)
```

**Validation Rules:**
- `address` must be valid host:port format
- `database` must be >= 0
- Timeouts must be valid duration strings
- Pool settings must be > 0

#### Raft Storage  
```yaml
stateStorage:
  type: "raft"
  raft:
    nodeId: "node-1"                  # Required: Unique node identifier
    bindAddress: "127.0.0.1:8081"     # Required: Raft cluster bind address
    peers: ["node-1:8081"]            # Required: List of all cluster peers (including self)
    dataDir: "/var/lib/vm-proxy-auth/raft"  # Required: Data directory for Raft logs
    snapshotInterval: "5m"            # Optional: Snapshot frequency (default: 5m)
    snapshotThreshold: 1000           # Optional: Log entries before snapshot (default: 1000)
    heartbeatTimeout: "1s"            # Optional: Heartbeat timeout (default: 1s)
    electionTimeout: "1s"             # Optional: Election timeout (default: 1s)
    leaderLeaseTimeout: "500ms"       # Optional: Leader lease timeout (default: 500ms)
    commitTimeout: "50ms"             # Optional: Commit timeout (default: 50ms)
```

**Validation Rules:**
- `nodeId` must be unique across the cluster
- `bindAddress` must be valid host:port format
- `peers` must contain at least the current node
- `dataDir` must be writable directory path
- All timeout values must be valid duration strings

### Request Queue Configuration

```yaml
requestQueue:
  enabled: true                       # Optional: Enable request queuing (default: false)
  maxSize: 1000                      # Optional: Maximum queue size (default: 1000)
  timeout: "30s"                     # Optional: Max time request can wait in queue (default: 30s)
  workers: 10                        # Optional: Number of worker goroutines (default: 10)
  drainTimeout: "10s"                # Optional: Time to wait for queue drain on shutdown (default: 10s)
```

**Validation Rules:**
- `maxSize` must be > 0 and <= 100000
- `timeout` must be valid duration string between 1s and 300s
- `workers` must be > 0 and <= 1000
- `drainTimeout` must be valid duration string

### Upstream Configuration

#### Backends Definition
```yaml
upstream:
  backends:
    - url: "https://vmselect-1.example.com"
      weight: 1
    - url: "https://vmselect-2.example.com"  
      weight: 2
    - url: "https://vmselect-3.example.com"
      weight: 0  # Disabled backend (weight 0 = exclude from rotation)
```

**Backend Properties:**
- `url` (Required): Full URL to VictoriaMetrics instance
- `weight` (Optional): Weight for load balancing (default: 1, >= 0)

**Validation Rules:**
- At least one backend must be configured
- Each `url` must be valid HTTP/HTTPS URL
- `weight` must be >= 0
- At least one backend must have weight > 0

#### Load Balancing Strategy
```yaml
upstream:
  strategy: "weighted-round-robin"   # Options: round-robin, weighted-round-robin, least-connections
```

**Supported Strategies:**
- `round-robin`: Simple round-robin distribution (ignores weights)
- `weighted-round-robin`: Distribution based on backend weights  
- `least-connections`: Routes to backend with fewest active connections

### Health Check Configuration

```yaml
upstream:
  healthCheck:
    enabled: true                     # Optional: Enable health checking (default: true)
    endpoint: "/-/ready"              # Optional: Health check endpoint (default: "/-/ready")
    interval: "30s"                   # Optional: Check interval (default: 30s)
    timeout: "5s"                     # Optional: Request timeout (default: 5s)
    strategy: "circuit-breaker"       # Optional: Strategy (default: "exclude-unhealthy")
    thresholds:
      failure: 3                      # Optional: Failed checks to mark unhealthy (default: 3)
      success: 1                      # Optional: Successful checks to mark healthy (default: 1)
    circuitBreaker:                   # Optional: Circuit breaker specific settings
      openTimeout: "60s"              # Time to keep circuit open (default: 60s)
      halfOpenMaxRequests: 5          # Max requests in half-open state (default: 5)
      minRequestsToTrip: 20           # Min requests to evaluate circuit (default: 20)
```

**Health Check Strategies:**
- `exclude-unhealthy`: Simply exclude unhealthy backends from rotation
- `circuit-breaker`: Use circuit breaker pattern with CLOSED/OPEN/HALF_OPEN states

**Validation Rules:**
- `interval` must be between 5s and 300s
- `timeout` must be between 1s and 60s and less than interval
- `thresholds.failure` must be > 0 and <= 10
- `thresholds.success` must be > 0 and <= 10
- Circuit breaker timeouts must be valid duration strings

### Retry Configuration

```yaml
upstream:
  retry:
    maxRetries: 3                     # Optional: Max retries per backend (default: 3)
    retryDelay: "1s"                  # Optional: Base delay between retries (default: 1s)
    backoffMultiplier: 1.5            # Optional: Exponential backoff multiplier (default: 1.5)
    maxBackoffDelay: "30s"            # Optional: Maximum backoff delay (default: 30s)
    retryableErrors:                  # Optional: Which error types to retry
      - "connection_timeout"
      - "connection_refused"
      - "5xx_status"
      - "network_error"
```

**Retryable Error Types:**
- `connection_timeout`: Connection or read timeouts
- `connection_refused`: Connection refused errors
- `5xx_status`: HTTP 5xx status codes (except 503 rate limit from upstream)
- `network_error`: General network connectivity issues

**Validation Rules:**
- `maxRetries` must be >= 0 and <= 10
- `retryDelay` must be valid duration between 100ms and 10s
- `backoffMultiplier` must be >= 1.0 and <= 3.0
- `maxBackoffDelay` must be >= retryDelay

## Environment Variable Overrides

All configuration values can be overridden using environment variables with the `VM_PROXY_AUTH_` prefix:

```bash
# State storage
VM_PROXY_AUTH_STATESTORAGE_TYPE=redis
VM_PROXY_AUTH_STATESTORAGE_REDIS_ADDRESS=redis.example.com:6379

# Request queue
VM_PROXY_AUTH_REQUESTQUEUE_ENABLED=true
VM_PROXY_AUTH_REQUESTQUEUE_MAXSIZE=2000

# Upstream strategy
VM_PROXY_AUTH_UPSTREAM_STRATEGY=round-robin

# Health check settings
VM_PROXY_AUTH_UPSTREAM_HEALTHCHECK_ENABLED=true
VM_PROXY_AUTH_UPSTREAM_HEALTHCHECK_INTERVAL=15s
```

## Configuration Validation

### Validation Rules Summary

1. **Required Fields:**
   - At least one backend must be configured
   - Each backend must have a valid URL

2. **Backend Validation:**
   - URLs must be valid HTTP/HTTPS format
   - At least one backend must have weight > 0
   - Weights must be >= 0

3. **Strategy Validation:**
   - Strategy must be one of: `round-robin`, `weighted-round-robin`, `least-connections`
   - Weighted strategies require at least one backend with weight > 0

4. **Health Check Validation:**
   - Intervals and timeouts must be reasonable durations
   - Thresholds must be within acceptable ranges
   - Circuit breaker settings must be consistent

5. **State Storage Validation:**
   - Redis: Connection parameters must be valid
   - Raft: Cluster configuration must be consistent
   - Data directories must be accessible

6. **Queue Validation:**
   - Queue size must be reasonable for available memory
   - Worker count should not exceed system capabilities

### Configuration Examples

#### Minimal Configuration
```yaml
upstream:
  backends:
    - url: "https://vmselect-1.example.com"
    - url: "https://vmselect-2.example.com"
```

#### Production Configuration
```yaml
stateStorage:
  type: "redis"
  redis:
    address: "redis-cluster.example.com:6379"
    password: "${REDIS_PASSWORD}"
    keyPrefix: "vm-proxy-prod:"

requestQueue:
  enabled: true
  maxSize: 5000
  workers: 50

upstream:
  backends:
    - url: "https://vmselect-1.prod.example.com"
      weight: 3
    - url: "https://vmselect-2.prod.example.com"
      weight: 3  
    - url: "https://vmselect-3.prod.example.com"
      weight: 2
    - url: "https://vmselect-4.staging.example.com"
      weight: 1
  strategy: "weighted-round-robin"
  healthCheck:
    enabled: true
    interval: "15s"
    timeout: "3s"
    strategy: "circuit-breaker"
    thresholds:
      failure: 2
      success: 1
    circuitBreaker:
      openTimeout: "30s"
      halfOpenMaxRequests: 3
  retry:
    maxRetries: 2
    retryDelay: "500ms"
    backoffMultiplier: 2.0
```

#### High Availability Raft Configuration
```yaml
stateStorage:
  type: "raft"
  raft:
    nodeId: "proxy-node-1"
    bindAddress: "10.0.1.10:8081"
    peers:
      - "proxy-node-1:8081"
      - "proxy-node-2:8081"
      - "proxy-node-3:8081"
    dataDir: "/var/lib/vm-proxy-auth/raft"
    snapshotInterval: "2m"

upstream:
  backends:
    - url: "https://vmselect-dc1-1.example.com"
      weight: 2
    - url: "https://vmselect-dc1-2.example.com"
      weight: 2
    - url: "https://vmselect-dc2-1.example.com" 
      weight: 1
  strategy: "least-connections"
  healthCheck:
    enabled: true
    strategy: "circuit-breaker"
```

## Migration from Single URL

### Automatic Migration
The system will automatically convert single URL configuration to backends format:

**Old Format:**
```yaml
upstream:
  url: "https://vmselect.example.com"
```

**Automatically Converted To:**
```yaml  
upstream:
  backends:
    - url: "https://vmselect.example.com"
      weight: 1
  strategy: "round-robin"
```

### Migration Tooling

A migration utility will be provided to convert existing configurations:

```bash
# Convert single URL to multiple backends
./vm-proxy-auth migrate-config --input config.yaml --output config-multi.yaml

# Validate new configuration format
./vm-proxy-auth validate-config --config config-multi.yaml --check-backends
```

## Configuration Best Practices

1. **Production Deployments:**
   - Use Redis or Raft for state storage in multi-instance deployments
   - Enable request queuing for better resilience
   - Configure appropriate health check intervals
   - Use weighted strategies for heterogeneous backend capacity

2. **Development/Testing:**
   - Local state storage is sufficient
   - Shorter health check intervals for faster feedback
   - Lower queue sizes to avoid memory issues

3. **Security:**
   - Use TLS for all backend connections
   - Secure Redis/Raft communications
   - Rotate credentials regularly
   - Use environment variables for sensitive values

4. **Performance:**
   - Tune worker pool sizes based on expected load
   - Adjust queue sizes based on available memory
   - Configure appropriate timeouts for your network
   - Monitor backend latency and adjust weights accordingly