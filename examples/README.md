# Configuration Files

This directory contains configuration examples for the VictoriaMetrics Proxy Authentication service.

## Directory Structure

- `viper/` - New Viper-based configuration files with camelCase naming
- `legacy/` - Legacy configuration files (snake_case naming, deprecated)

## Viper Configuration Files

The new Viper-based configuration system provides better structure, validation, and environment variable support.

### Available Configurations

- `config.example.yaml` - Basic RS256 JWT configuration
- `config.rs256.example.yaml` - RS256 with JWKS endpoint
- `config.hs256.example.yaml` - HS256 with shared secret
- `config.vm-multitenancy.yaml` - VictoriaMetrics multi-tenancy setup
- `config.test.yaml` - Testing/development configuration

### Key Features

- **camelCase naming** for all configuration keys
- **Nested structure** for better organization
- **Environment variable support** with `VM_PROXY_AUTH_` prefix
- **Comprehensive validation** with detailed error messages
- **Modern Go naming conventions** in struct fields

### Configuration Structure

```yaml
server:
  address: "0.0.0.0:8080"
  timeouts:
    readTimeout: "30s"
    writeTimeout: "30s"
    idleTimeout: "60s"

upstream:
  url: "https://vmselect.example.com"
  timeout: "30s"
  retry:
    maxRetries: 3
    retryDelay: "1s"

auth:
  jwt:
    algorithm: "RS256"  # or "HS256"
    jwksUrl: "https://..."  # for RS256
    secret: "..."           # for HS256
    validation:
      validateAudience: false
      validateIssuer: true
      requiredIssuer: "https://..."
      requiredAudience: []
    claims:
      userGroupsClaim: "groups"
    tokenTtl: "1h"
    cacheTtl: "5m"

tenantMapping:
  - groups: ["developers"]
    vmTenants:
      - accountId: "1000"
        projectId: "10"  # optional
    readOnly: false

tenantFilter:
  strategy: "orConditions"  # secure OR-based filtering (recommended)
  # strategy: "regex"       # legacy regex filtering (deprecated)
  labels:
    accountLabel: "vm_account_id"
    projectLabel: "vm_project_id"
    useProjectId: true

metrics:
  enabled: true
  path: "/metrics"

logging:
  level: "info"    # debug, info, warn, error, fatal
  format: "json"   # json, text
```

### Environment Variables

All configuration values can be overridden using environment variables with the `VM_PROXY_AUTH_` prefix:

```bash
# Server configuration
export VM_PROXY_AUTH_SERVER_ADDRESS="127.0.0.1:9090"

# Upstream configuration  
export VM_PROXY_AUTH_UPSTREAM_URL="https://vmselect.example.com"

# JWT configuration
export VM_PROXY_AUTH_AUTH_JWT_ALGORITHM="HS256"
export VM_PROXY_AUTH_AUTH_JWT_SECRET="your-secret-key"
export VM_PROXY_AUTH_AUTH_JWT_JWKSURL="https://auth.example.com/.well-known/jwks.json"

# Tenant filtering
export VM_PROXY_AUTH_TENANTFILTER_STRATEGY="orConditions"

# Logging
export VM_PROXY_AUTH_LOGGING_LEVEL="debug"
```

### Usage

```bash
# Using specific config file
./vm-proxy-auth --config configs/viper/config.example.yaml

# Using environment variables only
export VM_PROXY_AUTH_UPSTREAM_URL="https://vmselect.example.com"
export VM_PROXY_AUTH_AUTH_JWT_JWKSURL="https://auth.example.com/.well-known/jwks.json"
./vm-proxy-auth

# Validate configuration
./vm-proxy-auth --validate-config --config configs/viper/config.example.yaml
```

## Migration from Legacy Configuration

### Key Changes

1. **Naming Convention**: `snake_case` → `camelCase`
   - `read_timeout` → `readTimeout`
   - `jwks_url` → `jwksUrl`
   - `tenant_mappings` → `tenantMapping`

2. **Nested Structure**:
   - `server.timeouts.readTimeout` instead of `server.read_timeout`
   - `auth.jwt.validation.validateIssuer` instead of `auth.validate_issuer`

3. **Removed Deprecated Fields**:
   - `tenant_header` (replaced by tenant filtering strategies)
   - `tenant_mappings` (replaced by `tenantMapping` with `vmTenants`)

4. **Enhanced Validation**:
   - Comprehensive error messages
   - Validation of enum values (algorithms, strategies, levels)
   - Required field validation

### Migration Example

**Legacy:**
```yaml
server:
  read_timeout: "30s"
auth:
  jwks_url: "https://..."
  validate_issuer: true
tenant_mappings:
  - groups: ["dev"]
    tenants: ["1000"]
```

**New Viper:**
```yaml
server:
  timeouts:
    readTimeout: "30s"
auth:
  jwt:
    jwksUrl: "https://..."
    validation:
      validateIssuer: true
tenantMapping:
  - groups: ["dev"]
    vmTenants:
      - accountId: "1000"
```

## Security Best Practices

1. **Use RS256 with JWKS** for production environments
2. **Enable issuer validation** (`validateIssuer: true`)
3. **Use OR-based filtering strategy** (`strategy: "orConditions"`)
4. **Set appropriate token TTL** (`tokenTtl: "1h"`)
5. **Use structured logging** (`format: "json"`)
6. **Store secrets in environment variables**, not config files