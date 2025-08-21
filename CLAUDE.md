# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

### Building and Running
```bash
# Build application
go build -o vm-proxy-auth ./cmd/gateway
# Or use Make for production build with version info
make build

# Run with config
./vm-proxy-auth --config examples/config.example.yaml

# Run in development mode (with auto-reload if air is installed)
make dev

# Validate configuration without starting server
./vm-proxy-auth --validate-config --config examples/config.example.yaml
```

### Testing
```bash
# Run all tests
go test ./...
# Or with Make
make test

# Run tests with coverage
make coverage

# Run tests with race detector  
make test-race

# Run benchmarks (important for PromQL parser performance)
make benchmark

# Run single test
go test -v ./internal/services/tenant -run TestPromQLTenantInjector_ComplexQuery
```

### Code Quality
```bash
# Run linter (uses extensive .golangci.yml config)
make lint
# Or directly
golangci-lint run

# Format code
make fmt

# Security scan
make security

# Check for vulnerabilities
make vuln-check
```

## Architecture Overview

This is a **production-ready authentication proxy for VictoriaMetrics** built using **Clean Architecture** principles. The codebase separates concerns into distinct layers:

### Core Architecture Layers

1. **Domain Layer** (`internal/domain/`)
   - Contains business interfaces, types, and core business rules
   - No dependencies on external packages
   - Defines contracts: `AuthService`, `TenantService`, `ProxyService`, `MetricsService`

2. **Services Layer** (`internal/services/`)
   - Business logic implementation
   - **Auth Service**: JWT token validation with RS256/JWKS support
   - **Tenant Service**: **Critical** - handles PromQL query filtering using official Prometheus parser
   - **Proxy Service**: HTTP proxying to VictoriaMetrics with tenant injection
   - **Metrics Service**: Prometheus metrics collection with encapsulated metrics (no global vars)

3. **Handlers Layer** (`internal/handlers/`)
   - HTTP request handling and routing
   - Gateway handler orchestrates auth → tenant filtering → proxying flow

4. **Infrastructure Layer** (`internal/infrastructure/`)
   - External dependencies (logger, config)
   - No business logic

### Critical: PromQL Query Filtering Architecture

**The most complex part of this system** is the tenant filtering mechanism in `internal/services/tenant/`:

- **PromQLTenantInjector** (`promql_parser.go`) uses the **official Prometheus parser** to parse PromQL into AST
- **AST traversal** systematically injects tenant labels (`vm_account_id`, `vm_project_id`) into ALL metric references
- **Supports complex queries**: binary operations, functions, aggregations, subqueries
- **VictoriaMetrics specific**: handles both account-level and project-level tenant isolation

**Example transformation:**
```promql
# Input
sum(cpu_usage{cluster="prod"}) / sum(memory_total{cluster="prod"})

# Output  
sum(cpu_usage{vm_account_id="1000",cluster="prod"}) / sum(memory_total{vm_account_id="1000",cluster="prod"})
```

### Request Processing Flow

1. **Authentication**: JWT validation → extract user claims
2. **Authorization**: Check user access to requested path/method  
3. **Query Parsing**: Parse PromQL using Prometheus AST parser
4. **Tenant Injection**: Inject tenant labels into ALL metrics in query
5. **Proxying**: Forward filtered query to VictoriaMetrics
6. **Metrics**: Record comprehensive metrics throughout the flow

## Code Patterns & Conventions

### Error Handling
- Use custom `domain.AppError` for business errors with HTTP status codes
- Always wrap errors with context: `fmt.Errorf("operation failed: %w", err)`
- Never log sensitive information (tokens, credentials)

### Logging
- Use structured logging with `domain.Logger` interface
- Log levels: DEBUG (parsing details), INFO (request flow), WARN (auth failures), ERROR (system errors)  
- Always include relevant context fields: `user_id`, `query`, `tenant_id`

### Testing
- Shared mock logger in `internal/testutils/mock_logger.go`
- Use `t.Setenv()` and `t.TempDir()` for test isolation (Go 1.17+)
- Comprehensive PromQL parser benchmarks in `promql_parser_test.go`
- Use table-driven tests for query filtering scenarios

### Configuration
- YAML-based config in `internal/config/`
- Environment variable overrides supported
- Example configurations in `examples/` directory for different deployment scenarios
- Always validate configuration on startup

### Metrics
- Prometheus metrics encapsulated in service struct (no global variables)
- Comprehensive metrics: HTTP requests, upstream calls, auth attempts, query filtering performance
- All metrics prefixed with `vm_proxy_auth_`

## Important Technical Details

### JWT Authentication
- Supports both RS256 (with JWKS) and HS256 (with shared secret)
- JWT verifier refactored into multiple methods to reduce cognitive complexity
- JWKS caching with TTL for performance
- User claims map to VictoriaMetrics tenants via configurable tenant mappings

### VictoriaMetrics Integration
- Tenant isolation using `vm_account_id` and optional `vm_project_id` labels
- Write operations support tenant headers (`X-Prometheus-Tenant`)
- Query filtering ensures ALL metrics get tenant labels to prevent data leakage
- Supports both single-tenant and multi-tenant deployments

### Performance Considerations
- PromQL parsing is optimized - uses official Prometheus parser for accuracy and performance
- HTTP proxying with streaming to minimize memory usage
- Connection pooling and timeouts properly configured
- Extensive benchmarks for query filtering performance

## Linting and Code Quality

Uses **extremely strict** golangci-lint configuration (`.golangci.yml`):
- 80+ enabled linters including security, performance, and style checks
- Cognitive complexity limit: 20 (functions broken down if exceeded)
- Function length limit: 100 lines
- Comprehensive error checking and code formatting rules
- Special exclusions for test files to avoid over-strict checks

When running `golangci-lint run`:
- **forbidigo**: CLI functions (version, config validation) are allowed to use `fmt.Print*`
- **gochecknoglobals**: Build metadata variables are acceptable globals
- **funlen**: Main function length is architectural decision
- **intrange**: Benchmark loops with `b.N` cannot use new Go 1.22+ range syntax

## Configuration Examples

The `examples/` directory contains production-ready configurations:
- `config.example.yaml` - Basic HS256 JWT setup
- `config.rs256.example.yaml` - Production RS256 with JWKS
- `config.vm-multitenancy.yaml` - Multi-tenant VictoriaMetrics setup
- `config.metrics.example.yaml` - Full observability configuration

## Docker and Deployment

- Multi-stage Dockerfile for minimal production image
- Docker Compose setup with VictoriaMetrics
- Kubernetes deployment examples in README
- Health check endpoints: `/health`, `/ready`
- Graceful shutdown with proper resource cleanup
- TLS termination handled at load balancer level

## Development Workflow

1. **Make changes** to code following clean architecture principles
2. **Run tests**: `make test` (ensure all pass)
3. **Check quality**: `make lint` (fix any issues)
4. **Run locally**: `make run` with example config
5. **Performance**: Run benchmarks if touching PromQL parsing code
6. **Security**: `make security` for security scanning

When adding new features:
- Add interfaces to `domain/` layer first
- Implement in `services/` layer
- Add comprehensive tests with table-driven approach
- Update configuration examples if needed
- Consider metrics collection for observability