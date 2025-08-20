# VM Proxy Auth - Configuration Examples

This directory contains configuration examples for different deployment scenarios and modes of operation.

## 🚀 Quick Start

Choose the configuration that matches your deployment needs:

```bash
# Basic JWT authentication with shared secret
./vm-proxy-auth --config examples/config.example.yaml

# RS256 JWT with JWKS endpoint
./vm-proxy-auth --config examples/config.rs256.example.yaml

# VictoriaMetrics multi-tenancy setup
./vm-proxy-auth --config examples/config.vm-multitenancy.yaml

# Development/testing with metrics enabled
./vm-proxy-auth --config examples/config.metrics.example.yaml
```

## 📋 Available Configurations

### `config.example.yaml` - Basic Setup
**Use Case**: Simple deployments with shared JWT secrets

**Features**:
- HS256 JWT authentication with shared secret
- Basic tenant mapping
- Single VictoriaMetrics instance
- Minimal logging configuration

**Best For**:
- Development environments
- Small deployments
- Getting started quickly

### `config.rs256.example.yaml` - Production Security
**Use Case**: Production environments with proper key management

**Features**:
- RS256 JWT with JWKS endpoint
- Automatic key rotation support
- Enhanced security configuration
- Production logging settings

**Best For**:
- Production deployments
- Integration with OAuth2/OIDC providers
- Enterprise security requirements

### `config.vm-multitenancy.yaml` - Multi-Tenant Setup
**Use Case**: Complex multi-tenant VictoriaMetrics deployments

**Features**:
- Multiple tenant configurations
- Project-level isolation (`vm_project_id`)
- Complex tenant mapping rules
- Advanced query filtering

**Best For**:
- Multi-tenant SaaS applications
- Enterprise deployments
- Complex organizational structures

### `config.metrics.example.yaml` - Observability Focus
**Use Case**: Deployments requiring comprehensive monitoring

**Features**:
- Prometheus metrics enabled
- Detailed configuration examples
- Performance monitoring setup
- Complete observability stack

**Best For**:
- Production monitoring
- Performance analysis
- Compliance and auditing

### `config.test.yaml` - Testing Configuration
**Use Case**: Automated testing and validation

**Features**:
- Minimal configuration for tests
- Fast startup settings
- Debug logging enabled
- Test-friendly defaults

**Best For**:
- CI/CD pipelines
- Integration testing
- Development validation

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
# Start with basic config for development
./vm-proxy-auth --config examples/config.example.yaml --log-level debug

# Test with metrics enabled
./vm-proxy-auth --config examples/config.metrics.example.yaml
```

### Production Deployment
```bash
# Secure production setup
./vm-proxy-auth --config examples/config.rs256.example.yaml

# Multi-tenant production
./vm-proxy-auth --config examples/config.vm-multitenancy.yaml
```

### Testing & Validation
```bash
# Validate configuration
./vm-proxy-auth --config examples/config.test.yaml --validate-config

# Test with specific log level
./vm-proxy-auth --config examples/config.example.yaml --log-level debug
```

## 🛠️ Customization Guide

### 1. Modify Tenant Mappings
Edit the `tenant_maps` section to match your organization structure:

```yaml
tenant_maps:
  - user_claim: "email"
    user_value: "admin@company.com"
    vm_tenants:
      - account_id: "admin"
  
  - user_claim: "department" 
    user_value: "engineering"
    vm_tenants:
      - account_id: "1000"
      - account_id: "1001"
```

### 2. Configure VictoriaMetrics Integration
Adjust upstream settings for your VictoriaMetrics setup:

```yaml
upstream:
  url: "http://your-victoriametrics:8428"
  timeout: "30s"
  tenant_label: "vm_account_id"      # Your tenant label
  project_label: "vm_project_id"     # Optional project label
  tenant_header: "X-Tenant-ID"       # Custom tenant header
```

### 3. Security Configuration
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

### 4. Performance Tuning
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