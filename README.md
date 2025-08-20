# VM Proxy Auth

A production-ready authentication and authorization proxy for VictoriaMetrics with multi-tenant support and advanced PromQL query filtering.

## Features

### 🔐 Authentication & Authorization
- **JWT Authentication** with RS256 and JWKS support
- **Multi-tenant isolation** using VictoriaMetrics tenant system
- **Role-based access control** with configurable tenant mappings
- **Header-based tenant selection** for write operations

### 🎯 Advanced Query Filtering
- **Production-ready PromQL parsing** using official Prometheus library
- **AST-based tenant injection** - ensures ALL metrics in complex queries get filtered
- **Binary operation support** - correctly handles `sum(metric1) / sum(metric2)` type queries
- **VictoriaMetrics compatibility** - eliminates "missing tag filters" errors

### 🏗️ Clean Architecture
- **Domain-driven design** with clear separation of concerns
- **Dependency injection** for easy testing and maintenance
- **Structured logging** with configurable levels and formats
- **Graceful shutdown** with proper resource cleanup

## Quick Start

### Build
```bash
go build -o vm-proxy-auth ./cmd/gateway
```

### Run
```bash
./vm-proxy-auth --config config.yaml
```

### Command Line Options
```bash
./vm-proxy-auth --help
  -config string
        Path to configuration file
  -log-level string
        Log level (debug, info, warn, error)
  -validate-config
        Validate configuration and exit
  -version
        Show version information
```

## Configuration

### Basic Configuration
```yaml
server:
  address: ":8080"
  read_timeout: "30s"
  write_timeout: "30s"
  idle_timeout: "60s"

upstream:
  url: "http://victoriametrics:8428"
  timeout: "30s"
  tenant_label: "vm_account_id"
  project_label: "vm_project_id"
  use_project_id: false

auth:
  jwt:
    jwks_url: "https://your-auth-provider.com/.well-known/jwks.json"
    issuer: "https://your-auth-provider.com/"
    audience: "your-prometheus-gateway"

logging:
  level: "info"
  format: "json"

tenant_maps:
  - user_claim: "sub"
    user_value: "user@example.com"
    vm_tenants:
      - account_id: "1000"
        project_id: "main"
```

### VictoriaMetrics Integration

The gateway seamlessly integrates with VictoriaMetrics multi-tenant setup:

```yaml
upstream:
  url: "http://victoriametrics:8428"
  tenant_label: "vm_account_id"     # Default tenant label
  project_label: "vm_project_id"    # Optional project label
  use_project_id: true              # Enable project-level isolation
```

### Tenant Mapping

Configure how JWT claims map to VictoriaMetrics tenants:

```yaml
tenant_maps:
  - user_claim: "sub"
    user_value: "team-a@company.com"
    vm_tenants:
      - account_id: "1000"
        project_id: "production"
      - account_id: "1001" 
        project_id: "staging"
  
  - user_claim: "department"
    user_value: "engineering"
    vm_tenants:
      - account_id: "2000"
```

## Architecture

### Clean Architecture Layers

```
┌─────────────────────────────────────────┐
│               Handlers                  │  HTTP Layer
├─────────────────────────────────────────┤
│              Services                   │  Business Logic
│  ┌─────────┬─────────┬─────────────────┐│
│  │  Auth   │ Tenant  │ Proxy/Access    ││
│  └─────────┴─────────┴─────────────────┘│
├─────────────────────────────────────────┤
│             Domain                      │  Core Business Rules
├─────────────────────────────────────────┤
│          Infrastructure                 │  External Dependencies
│  ┌─────────┬─────────┬─────────────────┐│
│  │ Logger  │ Config  │    HTTP Client  ││
│  └─────────┴─────────┴─────────────────┘│
└─────────────────────────────────────────┘
```

### Query Processing Flow

1. **Authentication**: JWT validation and user extraction
2. **Authorization**: Tenant access validation
3. **Query Parsing**: PromQL AST parsing using Prometheus library
4. **Tenant Injection**: Systematic tenant label injection to ALL metrics
5. **Proxy**: Forward filtered query to VictoriaMetrics
6. **Response**: Return results to client

## Advanced Features

### PromQL Query Filtering

The gateway uses the official Prometheus parser to ensure accurate tenant filtering:

**Input Query:**
```promql
sum(kube_pod_info{cluster=~".*"}) / sum(machine_cores{cluster=~".*"})
```

**Filtered Output:**
```promql
sum(kube_pod_info{vm_account_id="1000",cluster=~".*"}) / sum(machine_cores{vm_account_id="1000",cluster=~".*"})
```

**Supported Query Types:**
- ✅ Simple metrics: `up`, `cpu_usage`
- ✅ Metrics with labels: `http_requests{job="api"}`
- ✅ Functions: `rate()`, `increase()`, `histogram_quantile()`
- ✅ Aggregations: `sum by (instance) (metric)`
- ✅ Binary operations: `metric1 + metric2`, `rate(a) / rate(b)`
- ✅ Complex nested queries with parentheses
- ✅ Subqueries: `metric[5m:30s]`

### Multi-Tenant Support

**VictoriaMetrics Tenant Isolation:**
```go
type VMTenant struct {
    AccountID string `json:"account_id"`
    ProjectID string `json:"project_id,omitempty"`
}
```

**Query Filtering Examples:**
- Single tenant: `{vm_account_id="1000"}`
- With project: `{vm_account_id="1000",vm_project_id="prod"}`
- Multiple tenants: Uses first tenant (extensible for OR logic)

## API Endpoints

### Health Checks
```bash
GET /health   # Health check
GET /ready    # Readiness check
```

### Metrics Endpoint
```bash
GET /metrics  # Prometheus metrics (if enabled)
```

### Prometheus API Proxy
All Prometheus API endpoints are proxied with tenant filtering:

```bash
# Query
GET /api/v1/query?query=up

# Query Range  
GET /api/v1/query_range?query=up&start=...&end=...

# Series
GET /api/v1/series?match[]=up

# Labels
GET /api/v1/labels

# Write (with tenant header)
POST /api/v1/write
X-Tenant-ID: 1000
```

## Monitoring & Observability

### Structured Logging

The gateway provides comprehensive structured logging:

```json
{
  "level": "info",
  "time": "2025-01-20T10:30:00Z",
  "msg": "TENANT FILTER RESULT",
  "user_id": "user@example.com",
  "original_query": "sum(cpu_usage) / sum(memory_total)",
  "filtered_query": "sum(cpu_usage{vm_account_id=\"1000\"}) / sum(memory_total{vm_account_id=\"1000\"})",
  "filter_applied": true,
  "used_production_parser": true
}
```

### Log Levels
- **DEBUG**: Detailed parsing and tenant injection logs
- **INFO**: Request/response and tenant filtering results
- **WARN**: Authentication failures and misconfigurations  
- **ERROR**: System errors and parsing failures

### Prometheus Metrics

VM Proxy Auth exposes comprehensive Prometheus metrics when enabled:

```yaml
metrics:
  enabled: true
```

#### Available Metrics

**HTTP Request Metrics:**
- `vm_proxy_auth_http_requests_total` - Total HTTP requests by method, path, status, user
- `vm_proxy_auth_http_request_duration_seconds` - HTTP request duration histogram

**Upstream (VictoriaMetrics) Metrics:**
- `vm_proxy_auth_upstream_requests_total` - Total upstream requests by method, path, status, tenant count
- `vm_proxy_auth_upstream_request_duration_seconds` - Upstream request duration histogram

**Authentication Metrics:**
- `vm_proxy_auth_auth_attempts_total` - Authentication attempts by status and user
- `vm_proxy_auth_query_filtering_total` - Query filtering operations by user, tenant count, filter applied
- `vm_proxy_auth_query_filtering_duration_seconds` - Query filtering duration histogram

**Tenant Access Metrics:**
- `vm_proxy_auth_tenant_access_total` - Tenant access checks by user, tenant, allowed status

**Example Metrics Query:**
```promql
# Request rate by status code
rate(vm_proxy_auth_http_requests_total[5m])

# Authentication failure rate
rate(vm_proxy_auth_auth_attempts_total{status="failed"}[5m])

# Query filtering performance
histogram_quantile(0.95, rate(vm_proxy_auth_query_filtering_duration_seconds_bucket[5m]))

# Top users by request volume
topk(10, sum by (user_id) (rate(vm_proxy_auth_http_requests_total[5m])))
```

## Development

### Project Structure
```
├── cmd/gateway/           # Application entrypoint
├── internal/
│   ├── config/           # Configuration management
│   ├── domain/           # Core business logic and interfaces
│   ├── handlers/         # HTTP handlers
│   ├── infrastructure/   # External dependencies
│   └── services/         # Business logic implementation
│       ├── auth/         # JWT authentication
│       ├── tenant/       # Tenant filtering and PromQL parsing
│       ├── proxy/        # HTTP proxying
│       └── access/       # Authorization logic
├── config.example.yaml   # Example configuration
└── README.md
```

### Running Tests
```bash
go test ./...
```

### Building for Production
```bash
# Build with version info
go build -ldflags "-X main.version=1.0.0 -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) -X main.gitCommit=$(git rev-parse HEAD)" -o vm-proxy-auth ./cmd/gateway
```

## Deployment

### Docker
```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o vm-proxy-auth ./cmd/gateway

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/vm-proxy-auth .
COPY config.yaml .
CMD ["./vm-proxy-auth", "--config", "config.yaml"]
```

### Kubernetes
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vm-proxy-auth
spec:
  replicas: 3
  selector:
    matchLabels:
      app: vm-proxy-auth
  template:
    metadata:
      labels:
        app: vm-proxy-auth
    spec:
      containers:
      - name: gateway
        image: vm-proxy-auth:latest
        ports:
        - containerPort: 8080
        env:
        - name: LOG_LEVEL
          value: "info"
        volumeMounts:
        - name: config
          mountPath: /etc/gateway
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
      volumes:
      - name: config
        configMap:
          name: gateway-config
```

## Security Considerations

- ✅ **JWT Validation**: Comprehensive token validation with JWKS
- ✅ **Tenant Isolation**: Strict query filtering prevents cross-tenant data access
- ✅ **No Secret Logging**: Sensitive data is never logged
- ✅ **Graceful Error Handling**: Fails secure with proper error messages
- ✅ **HTTPS Ready**: TLS termination at load balancer level
- ✅ **Rate Limiting**: Can be implemented at reverse proxy level

## Performance

- **Low Latency**: Efficient PromQL parsing with minimal overhead
- **Memory Efficient**: Streaming proxy with bounded memory usage
- **Concurrent**: Handles multiple requests simultaneously
- **Production Ready**: Tested with complex real-world PromQL queries

## Contributing

1. Follow clean architecture principles
2. Add tests for new features
3. Update documentation
4. Use structured logging
5. Ensure VictoriaMetrics compatibility

## License

MIT License - see LICENSE file for details.

---

**Built with ❤️ for secure, multi-tenant Prometheus/VictoriaMetrics deployments**