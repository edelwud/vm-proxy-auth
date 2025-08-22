# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2025-01-22

### Added
- **Initial Release** - Production-ready authentication proxy for VictoriaMetrics
- **JWT Authentication** with RS256/JWKS and HS256 support
- **Multi-tenant isolation** using VictoriaMetrics tenant system
- **Advanced PromQL filtering** using official Prometheus parser
- **Clean Architecture** implementation with domain-driven design
- **Comprehensive logging** with structured JSON output
- **Prometheus metrics** for monitoring and observability
- **Health checks** (`/health`, `/ready` endpoints)
- **Configuration validation** with `--validate-config` flag

### Security Features
- **Secure tenant filtering** with OR-based strategy preventing cross-tenant data leakage
- **AST-based PromQL parsing** ensuring ALL metrics in complex queries are filtered
- **JWT validation** with comprehensive error handling
- **No secret logging** - sensitive data never appears in logs

### Architecture
- **Domain Layer** - Core business interfaces and types
- **Services Layer** - Auth, Tenant, Proxy, Metrics services
- **Handlers Layer** - HTTP request handling and routing
- **Infrastructure Layer** - External dependencies (logger, config)

### Supported Query Types
- ✅ Simple metrics: `up`, `cpu_usage`
- ✅ Metrics with labels: `http_requests{job="api"}`
- ✅ Functions: `rate()`, `increase()`, `histogram_quantile()`
- ✅ Aggregations: `sum by (instance) (metric)`
- ✅ Binary operations: `metric1 + metric2`, `rate(a) / rate(b)`
- ✅ Complex nested queries with parentheses
- ✅ Subqueries: `metric[5m:30s]`

### Configuration Examples
- Basic HS256 JWT setup (`examples/config.example.yaml`)
- Production RS256 with JWKS (`examples/config.rs256.example.yaml`)
- Multi-tenant VictoriaMetrics setup (`examples/config.vm-multitenancy.yaml`)
- Full observability configuration (`examples/config.metrics.example.yaml`)

### Development & Deployment
- **Docker support** with multi-stage builds
- **Kubernetes deployment** examples
- **Comprehensive test suite** with 85%+ coverage
- **CI/CD pipeline** with automated testing and security scanning
- **golangci-lint** integration with 80+ linters

### Performance
- **Low latency** PromQL parsing with minimal overhead
- **Memory efficient** streaming proxy
- **Concurrent request handling**
- **Production tested** with complex real-world queries

### API Endpoints
- `GET /health` - Health check
- `GET /ready` - Readiness check
- `GET /metrics` - Prometheus metrics (optional)
- All Prometheus API endpoints proxied with tenant filtering

### Documentation
- Comprehensive README with deployment guides
- Security considerations documentation
- Architecture diagrams and flow explanations
- Configuration examples for different scenarios