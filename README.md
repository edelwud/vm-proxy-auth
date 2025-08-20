# Prometheus OAuth Gateway

A secure authentication and authorization gateway for Prometheus/VictoriaMetrics in multi-tenant environments. This project provides JWT-based authentication with role-based access control and automatic query filtering based on tenant mappings.

## 🚀 Features

- **JWT Authentication**: Support for JWKS and secret-based JWT token verification
- **Multi-tenancy**: Automatic tenant isolation with query filtering
- **Role-based Access Control**: Group-based tenant access management
- **Query Modification**: Automatic injection of tenant filters into Prometheus queries
- **Grafana Integration**: Seamless integration with Grafana's "Forward OAuth Identity" feature
- **Metrics & Monitoring**: Built-in Prometheus metrics and comprehensive logging
- **High Availability**: Retry mechanisms, health checks, and graceful shutdown

## 🏗️ Architecture

```
┌─────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Grafana   │───▶│ OAuth Gateway    │───▶│ VictoriaMetrics │
│             │    │                  │    │                 │
│ Forward     │    │ • JWT Verify     │    │ Multi-tenant    │
│ OAuth       │    │ • Tenant Filter  │    │ Storage         │
│ Identity    │    │ • Query Modify   │    │                 │
└─────────────┘    └──────────────────┘    └─────────────────┘
                              │
                              ▼
                   ┌──────────────────┐
                   │   Keycloak/      │
                   │   OAuth Provider │
                   └──────────────────┘
```

## 📦 Installation

### Using Docker Compose (Recommended)

1. Clone the repository:
```bash
git clone https://github.com/finlego/prometheus-oauth-gateway.git
cd prometheus-oauth-gateway
```

2. Copy and configure the example configuration:
```bash
cp config.example.yaml config.yaml
# Edit config.yaml with your settings
```

3. Start the services:
```bash
docker-compose up -d
```

### Building from Source

Requirements:
- Go 1.21 or later
- Make (optional)

```bash
# Clone and build
git clone https://github.com/finlego/prometheus-oauth-gateway.git
cd prometheus-oauth-gateway

# Using Make
make build

# Or directly with Go
go build -o bin/prometheus-oauth-gateway ./cmd/gateway

# Run
./bin/prometheus-oauth-gateway --config config.yaml
```

## ⚙️ Configuration

### Example Configuration

```yaml
server:
  address: "0.0.0.0:8080"
  read_timeout: "30s"
  write_timeout: "30s"
  idle_timeout: "60s"

upstream:
  url: "http://victoriametrics:8428"
  timeout: "30s"
  max_retries: 3
  retry_delay: "1s"
  tenant_header: "X-Prometheus-Tenant"
  tenant_label: "tenant_id"

auth:
  type: "jwt"
  jwks_url: "https://keycloak.example.com/auth/realms/myrealm/protocol/openid-connect/certs"
  jwt_algorithm: "RS256"
  validate_issuer: true
  required_issuer: "https://keycloak.example.com/auth/realms/myrealm"
  token_ttl: "1h"
  cache_ttl: "5m"
  user_groups_claim: "groups"

tenant_mappings:
  - groups: ["admin", "platform-admin"]
    tenants: ["*"]  # Access to all tenants
    read_only: false
    
  - groups: ["team-alpha", "dev-alpha"]
    tenants: ["alpha", "alpha-dev", "alpha-staging"]
    read_only: false
    
  - groups: ["readonly-users", "auditors"]
    tenants: ["alpha", "beta"]
    read_only: true

metrics:
  enabled: true
  path: "/metrics"

logging:
  level: "info"
  format: "json"
```

### Environment Variables

All configuration options can be overridden using environment variables:

```bash
SERVER_ADDRESS=0.0.0.0:8080
UPSTREAM_URL=http://victoriametrics:8428
AUTH_JWKS_URL=https://keycloak.example.com/jwks
AUTH_REQUIRED_ISSUER=https://keycloak.example.com/auth/realms/myrealm
AUTH_USER_GROUPS_CLAIM=groups
LOG_LEVEL=info
```

## 🔧 Grafana Integration

### Datasource Configuration

1. Create a Prometheus datasource in Grafana
2. Set the URL to your OAuth Gateway: `http://prometheus-oauth-gateway:8080`
3. **Enable "Forward OAuth Identity"** - This is crucial for passing the user's JWT token

### Example Datasource (provisioning):

```yaml
apiVersion: 1
datasources:
  - name: VictoriaMetrics-OAuth
    type: prometheus
    access: proxy
    url: http://prometheus-oauth-gateway:8080
    isDefault: true
    jsonData:
      httpMethod: GET
      timeout: 60s
```

### OAuth Configuration in Grafana

Configure Grafana to use your OAuth provider (Keycloak, Auth0, etc.):

```bash
GF_AUTH_OAUTH_AUTO_LOGIN=true
GF_AUTH_OAUTH_ALLOW_SIGN_UP=true
GF_AUTH_OAUTH_CLIENT_ID=grafana
GF_AUTH_OAUTH_CLIENT_SECRET=grafana-secret
GF_AUTH_OAUTH_SCOPES=openid email profile groups
GF_AUTH_OAUTH_AUTH_URL=https://keycloak.example.com/auth/realms/myrealm/protocol/openid-connect/auth
GF_AUTH_OAUTH_TOKEN_URL=https://keycloak.example.com/auth/realms/myrealm/protocol/openid-connect/token
```

## 🔒 Security Features

### JWT Token Validation

- **Algorithm Support**: RS256, HS256, ES256
- **JWKS Integration**: Automatic public key fetching and caching
- **Claims Validation**: Issuer, audience, expiration checks
- **Token Caching**: Configurable TTL for performance

### Tenant Isolation

- **Automatic Query Filtering**: Injects `tenant_id` filters into PromQL queries
- **Multi-tenant Support**: Users can access multiple tenants based on group membership
- **Read-only Access**: Configurable read-only permissions per group

### Example Query Transformation

Original query from user in `team-alpha` group:
```promql
up
```

Automatically transformed to:
```promql
up{tenant_id=~"alpha|alpha-dev|alpha-staging"}
```

## 📊 Monitoring

The gateway exposes comprehensive metrics at `/metrics`:

### Key Metrics

- `prometheus_oauth_gateway_requests_total` - Total requests processed
- `prometheus_oauth_gateway_request_duration_seconds` - Request latency
- `prometheus_oauth_gateway_authentication_total` - Authentication attempts
- `prometheus_oauth_gateway_query_filters_applied_total` - Query filters applied
- `prometheus_oauth_gateway_tenant_access_denied_total` - Access denied events
- `prometheus_oauth_gateway_upstream_requests_total` - Upstream requests

### Sample Dashboard

A complete Grafana dashboard is included in `grafana/dashboards/oauth-gateway-dashboard.json` with:

- Request rates and response times
- Authentication success rates
- Tenant access patterns
- Query filtering statistics
- JWKS cache performance

## 🧪 Testing

Run the comprehensive test suite:

```bash
# Run all tests
make test

# Run tests with coverage
make coverage

# Run tests with race detector
make test-race

# Run benchmarks
make benchmark
```

### Test Coverage

The project includes extensive tests covering:

- JWT token verification and parsing
- Tenant mapping and access control
- Query filtering and transformation
- HTTP middleware and handlers
- Configuration loading and validation
- Proxy functionality and retry logic

## 🚀 Development

### Prerequisites

- Go 1.21+
- Docker & Docker Compose
- Make

### Development Setup

```bash
# Install development tools
make install-tools

# Run in development mode (with auto-reload)
make dev

# Run linting and security checks
make lint
make security
make vet

# Build for multiple platforms
make build-all
```

### Project Structure

```
├── cmd/gateway/          # Application entrypoint
├── internal/
│   ├── auth/            # JWT authentication logic
│   ├── config/          # Configuration management
│   ├── middleware/      # HTTP middleware components
│   ├── metrics/         # Prometheus metrics
│   ├── proxy/           # Upstream proxy logic
│   └── server/          # HTTP server and handlers
├── pkg/
│   ├── tenant/          # Tenant mapping logic
│   └── utils/           # Utility functions
├── grafana/             # Grafana configuration
├── docker-compose.yml   # Development environment
├── Dockerfile           # Container image
└── Makefile            # Build automation
```

## 🐛 Troubleshooting

### Common Issues

1. **"User context not found"**
   - Ensure Grafana has "Forward OAuth Identity" enabled
   - Check that JWT tokens are being passed in the Authorization header

2. **"Token verification failed"**
   - Verify JWKS URL is accessible
   - Check JWT algorithm matches your OAuth provider
   - Ensure token is not expired

3. **"Query access denied"**
   - Check user groups in JWT token match tenant mappings
   - Verify `user_groups_claim` configuration matches your JWT structure

4. **"Failed to forward request"**
   - Check upstream URL and connectivity
   - Verify retry configuration
   - Check upstream service health

### Debug Mode

Enable debug logging for detailed troubleshooting:

```bash
export LOG_LEVEL=debug
./prometheus-oauth-gateway --config config.yaml
```

### Health Checks

- **Health**: `GET /health` - Basic service health
- **Readiness**: `GET /readiness` - Service readiness including upstream checks

## 📈 Performance

### Benchmarks

Typical performance characteristics:

- **Request Latency**: ~5ms average (excluding upstream)
- **Authentication**: ~2ms average (with JWKS caching)
- **Query Filtering**: ~1ms average
- **Memory Usage**: ~50MB base + JWT cache
- **Throughput**: 1000+ RPS per core

### Optimization Tips

1. **Enable JWKS Caching**: Set appropriate `cache_ttl` (default 5m)
2. **Tune Retry Parameters**: Adjust `max_retries` and `retry_delay` for your network
3. **Connection Pooling**: Configure upstream HTTP client settings
4. **Resource Limits**: Set appropriate Docker memory/CPU limits

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/amazing-feature`
3. Commit your changes: `git commit -m 'Add amazing feature'`
4. Push to the branch: `git push origin feature/amazing-feature`
5. Open a Pull Request

### Development Guidelines

- Follow Go best practices and formatting (`make fmt`)
- Add tests for new functionality (`make test`)
- Update documentation as needed
- Run security scans (`make security`)

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- [VictoriaMetrics](https://victoriametrics.com/) - High-performance monitoring solution
- [Grafana](https://grafana.com/) - Observability platform
- [Keycloak](https://www.keycloak.org/) - Identity and access management
- [Prometheus](https://prometheus.io/) - Monitoring system and time series database