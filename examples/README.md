# VM Proxy Auth - Configuration Examples

This directory contains configuration examples for different deployment scenarios and modes of operation.

## 🚀 Quick Start

Choose the configuration that matches your deployment needs:

```bash
# Development/testing with shared secret
./vm-proxy-auth --config examples/config.test.yaml

# Production deployment with JWKS authentication  
./vm-proxy-auth --config examples/config.example.yaml

# VictoriaMetrics multi-tenancy setup
./vm-proxy-auth --config examples/config.vm-multitenancy.yaml

# Observability-focused with metrics
./vm-proxy-auth --config examples/config.metrics.example.yaml

# Minimal testing setup
./vm-proxy-auth --config examples/test-metrics-config.yaml
```

## 📋 Available Configurations

### `config.test.yaml` - Development/Testing
**Use Case**: Local development and CI/CD testing environments

**Features**:
- HS256 JWT authentication with shared secret
- Group-based access control (admin, developers, viewers)
- Comprehensive tenant mappings for testing
- Debug-friendly logging configuration

**Best For**:
- Local development environments
- Integration testing and CI/CD
- Getting started quickly

### `config.example.yaml` - Production Ready
**Use Case**: Production environments with JWKS authentication

**Features**:
- RS256 JWT with JWKS endpoint (Keycloak integration)
- Group-based tenant access control  
- Read-only and read-write permission levels
- Production logging and metrics

**Best For**:
- Production deployments
- Integration with OAuth2/OIDC providers  
- Standard tenant isolation requirements

### `config.vm-multitenancy.yaml` - VictoriaMetrics Multi-Tenant
**Use Case**: Production VictoriaMetrics with advanced multi-tenancy

**Features**:
- RS256 JWT with JWKS (Keycloak integration)
- VictoriaMetrics account and project-level isolation
- vm_account_id and vm_project_id label injection
- Enhanced tenant security with use_project_id

**Best For**:
- Enterprise VictoriaMetrics deployments
- Multi-tenant SaaS with strict isolation
- Account/project-based organizational structures

### `config.metrics.example.yaml` - Metrics & Observability
**Use Case**: Focused on monitoring and observability

**Features**:
- VictoriaMetrics integration with tenant mapping
- User-specific tenant configurations
- Prometheus metrics endpoint enabled
- Account and project ID support

**Best For**:
- Production monitoring setups
- Performance analysis and debugging
- Comprehensive observability requirements

### `test-metrics-config.yaml` - Minimal Testing
**Use Case**: Testing metrics endpoint with minimal configuration

**Features**:
- Minimal configuration for CI/CD
- Metrics endpoint enabled only
- No authentication for testing
- Basic server configuration

**Best For**:
- Automated testing pipelines
- Metrics endpoint validation
- Minimal resource testing

## 🔧 Configuration Modes

### Authentication Modes

#### 1. Shared Secret (HS256)
```yaml
auth:
  jwt_secret: "your-secret-key-here"
  jwt_algorithm: "HS256"
```
- Simple setup
- Shared key between services
- Good for development/testing

#### 2. Public Key (RS256/JWKS)
```yaml
auth:
  jwks_url: "https://your-auth-provider.com/.well-known/jwks.json"
  jwt_issuer: "https://your-auth-provider.com/"
  jwt_audience: "your-app-audience"
```
- Production-ready security
- Automatic key rotation
- OAuth2/OIDC integration

### Tenant Isolation Modes

#### 1. Simple Account-Level
```yaml
upstream:
  tenant_label: "vm_account_id"
  use_project_id: false

tenant_maps:
  - user_claim: "sub"
    user_value: "user@company.com"
    vm_tenants:
      - account_id: "1000"
```
- Single-level isolation
- Simple tenant structure
- Account-based separation

#### 2. Project-Level Multi-Tenancy
```yaml
upstream:
  tenant_label: "vm_account_id"
  project_label: "vm_project_id"
  use_project_id: true

tenant_maps:
  - user_claim: "sub"
    user_value: "user@company.com"
    vm_tenants:
      - account_id: "1000"
        project_id: "production"
      - account_id: "1000"
        project_id: "staging"
```
- Two-level isolation
- Environment separation
- Fine-grained access control

### Logging Modes

#### 1. Development Logging
```yaml
logging:
  level: "debug"
  format: "text"
```
- Human-readable output
- Detailed debugging information
- Console-friendly format

#### 2. Production Logging
```yaml
logging:
  level: "info"
  format: "json"
```
- Structured JSON output
- Log aggregation friendly
- Production monitoring ready

### Metrics & Monitoring

#### 1. Metrics Disabled (Default)
```yaml
metrics:
  enabled: false
```
- Minimal resource usage
- Simple deployments
- No observability overhead

#### 2. Full Observability
```yaml
metrics:
  enabled: true
```
- Prometheus metrics endpoint
- Comprehensive monitoring
- Performance insights

## 🎯 Usage Patterns

### Development Setup
```bash
# Start with test config for development
./vm-proxy-auth --config examples/config.test.yaml --log-level debug

# Test with metrics enabled
./vm-proxy-auth --config examples/config.metrics.example.yaml
```

### Production Deployment
```bash
# Standard production setup
./vm-proxy-auth --config examples/config.example.yaml

# Multi-tenant production with VictoriaMetrics
./vm-proxy-auth --config examples/config.vm-multitenancy.yaml
```

### Testing & Validation
```bash
# Validate configuration files
./vm-proxy-auth --validate-config --config examples/config.test.yaml
./vm-proxy-auth --validate-config --config examples/config.vm-multitenancy.yaml

# Test minimal metrics setup
./vm-proxy-auth --config examples/test-metrics-config.yaml
```

## 🛠️ Customization Guide

### 1. Group-Based Tenant Mappings (Standard)
For most deployments, use group-based mappings in `tenant_mappings`:

```yaml
tenant_mappings:
  - groups: ["platform-admin", "sre"]
    tenants: ["*"]  # Access to all tenants
    read_only: false
    
  - groups: ["team-alpha-dev"]
    tenants: ["alpha-dev", "alpha-staging"] 
    read_only: false
    
  - groups: ["auditors", "read-only-users"]
    tenants: ["alpha", "beta", "prod"]
    read_only: true
```

### 2. VictoriaMetrics VM Tenant Mappings (Advanced)
For VictoriaMetrics multi-tenancy, use `vm_tenants` in `tenant_mappings`:

```yaml
tenant_mappings:
  - groups: ["developers"]
    vm_tenants:
      - account_id: "2000"
        project_id: "dev"  # Optional project isolation
      - account_id: "2000" 
        project_id: "staging"
    read_only: false
```

### 3. Configure VictoriaMetrics Integration
Adjust upstream settings for your VictoriaMetrics setup:

```yaml
upstream:
  url: "http://your-victoriametrics:8428"
  timeout: "30s"
  tenant_label: "vm_account_id"      # Your tenant label
  project_label: "vm_project_id"     # Optional project label
  tenant_header: "X-Tenant-ID"       # Custom tenant header
```

### 4. Security Configuration
Configure authentication for your environment:

```yaml
auth:
  # For OIDC/OAuth2 integration
  jwks_url: "https://your-provider.com/.well-known/jwks.json"
  jwt_issuer: "https://your-provider.com/"
  jwt_audience: "vm-proxy-auth"
  
  # Cache settings for performance
  cache_ttl: "5m"
```

### 5. Performance Tuning
Optimize for your load patterns:

```yaml
server:
  read_timeout: "30s"    # Adjust based on query complexity
  write_timeout: "30s"   # Adjust based on write volume
  idle_timeout: "60s"    # Connection management

upstream:
  timeout: "30s"         # VictoriaMetrics response timeout
```

## 🚨 Security Considerations

### Production Checklist
- [ ] Use RS256 JWT with JWKS for production
- [ ] Configure proper CORS settings if needed
- [ ] Set up TLS termination at load balancer
- [ ] Use strong JWT secrets (if using HS256)
- [ ] Regularly rotate JWT signing keys
- [ ] Monitor authentication failures
- [ ] Configure rate limiting at reverse proxy level

### Network Security
- [ ] Deploy behind reverse proxy (nginx/envoy)
- [ ] Configure firewall rules
- [ ] Use private networks for VictoriaMetrics communication
- [ ] Enable audit logging for compliance

## 📊 Monitoring Setup

When using metrics-enabled configuration:

### Prometheus Configuration
```yaml
scrape_configs:
  - job_name: 'vm-proxy-auth'
    static_configs:
      - targets: ['vm-proxy-auth:8080']
    metrics_path: /metrics
    scrape_interval: 15s
```

### Key Metrics to Monitor
- `vm_proxy_auth_http_requests_total` - Request volume
- `vm_proxy_auth_auth_attempts_total{status="failed"}` - Security monitoring
- `vm_proxy_auth_upstream_request_duration_seconds` - Performance
- `vm_proxy_auth_query_filtering_duration_seconds` - Query complexity

### Grafana Dashboard Queries
```promql
# Request rate by status
sum by (status_code) (rate(vm_proxy_auth_http_requests_total[5m]))

# Authentication failure rate  
rate(vm_proxy_auth_auth_attempts_total{status="failed"}[5m])

# Top users by request volume
topk(10, sum by (user_id) (rate(vm_proxy_auth_http_requests_total[5m])))
```

## 🔄 Migration Guide

### From Basic to Production
1. Switch from `config.example.yaml` to `config.rs256.example.yaml`
2. Set up JWKS endpoint in your auth provider
3. Update tenant mappings for your organization
4. Enable metrics for monitoring
5. Configure structured logging

### Adding Multi-Tenancy
1. Start with `config.vm-multitenancy.yaml`
2. Configure VictoriaMetrics with multi-tenant support
3. Update client applications to use tenant headers
4. Test tenant isolation thoroughly
5. Monitor tenant access patterns

## 🆘 Troubleshooting

### Common Issues
1. **Authentication failures**: Check JWT configuration and keys
2. **Query filtering not working**: Verify tenant mappings
3. **Upstream connection errors**: Check VictoriaMetrics connectivity
4. **Metrics not appearing**: Ensure `metrics.enabled: true`

### Debug Mode
```bash
./vm-proxy-auth --config examples/config.example.yaml --log-level debug
```

This will provide detailed logging for troubleshooting configuration issues.