# CLAUDE.md

This file provides strict guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Zero Tolerance Quality Standards

**MANDATORY**: All code changes MUST achieve and maintain:
- ✅ **0 golangci-lint issues** - No exceptions, no warnings ignored
- ✅ **100% test pass rate** - All tests must pass, no skipped tests in CI  
- ✅ **No legacy code** - Remove deprecated/backward compatibility code immediately
- ✅ **No binary files in git** - All build artifacts excluded via .gitignore
- ✅ **Standard patterns only** - Use established Go patterns, not custom solutions

## Development Commands

### Primary Quality Commands
```bash
# ALWAYS run before any commit - comprehensive quality check
make quality          # lint + test + format + security + vuln-check

# Individual quality checks
make lint             # golangci-lint with zero-tolerance policy  
make test             # Run all tests with race detector
make format           # goimports + gofmt
make security         # gosec security scanner
make vuln-check       # govulncheck for known vulnerabilities

# Clean build artifacts and temporary files
make clean            # Remove all generated files, binaries, coverage reports
```

### Build and Run
```bash
# Production build with version info
make build            # Creates versioned binary in bin/

# Development
make dev              # Live reload development server
make run              # Run with example config

# Configuration validation
make validate-config CONFIG=examples/config.example.yaml
```

### Dependency Management  
```bash
# Update dependencies safely
make deps-update      # Update go.mod + security check + test
make deps-audit       # Check for known vulnerabilities
```

## Project Structure & Architecture

### Clean Architecture Layers (STRICTLY ENFORCED)

```
internal/
├── domain/           # Business logic interfaces & types (NO dependencies)
│   ├── types.go      # Core business types
│   └── errors.go     # Domain-specific errors
├── services/         # Business logic implementations  
│   ├── auth/         # JWT authentication
│   ├── tenant/       # PromQL query filtering (CRITICAL)
│   ├── proxy/        # HTTP proxying with load balancing
│   ├── metrics/      # Prometheus metrics (encapsulated, no globals)
│   └── statestorage/ # Distributed state management (Raft/Redis)
├── handlers/         # HTTP request handling
├── infrastructure/   # External dependencies (logger, config)
└── testutils/        # Shared test utilities (MANDATORY USE)
```

### Interface Design Principles
- **Small, focused interfaces** - Single responsibility principle
- **Dependency injection via constructors** - No global variables  
- **Context-aware operations** - All I/O operations accept context.Context
- **Proper error handling** - Use domain.AppError for business errors

## MANDATORY Code Standards

### Testing (NON-NEGOTIABLE)
```go
// ✅ ALWAYS use testutils for mocks
logger := testutils.NewMockLogger()
storage := testutils.NewMockStateStorage()

// ✅ ALWAYS use require for critical assertions
require.NoError(t, err)
require.NotNil(t, result)

// ✅ Table-driven tests for all scenarios  
tests := []struct {
    name     string
    input    string  
    expected string
    wantErr  bool
}{
    // test cases
}

// ❌ FORBIDDEN: Custom mock implementations
// ❌ FORBIDDEN: assert.X() for error checking
// ❌ FORBIDDEN: Single test functions for multiple scenarios
```

### Error Handling (STRICT)
```go
// ✅ Domain errors with HTTP status codes
return domain.NewAppError("invalid token", 401, err)

// ✅ Context propagation with proper wrapping
if err != nil {
    return fmt.Errorf("failed to validate JWT: %w", err)
}

// ❌ FORBIDDEN: Generic errors without context
// ❌ FORBIDDEN: Ignoring errors (_, err patterns without handling)
```

### Logging (STANDARDIZED)  
```go
// ✅ Structured logging with domain.Logger interface
logger.Info("Processing request", 
    domain.Field{Key: "user_id", Value: userID},
    domain.Field{Key: "query", Value: query})

// ❌ FORBIDDEN: Direct fmt.Printf, log.Print calls
// ❌ FORBIDDEN: Logging sensitive data (tokens, passwords)
```

### Git Workflow (MANDATORY)

#### Branch Strategy
- `main` - Production-ready code only
- `feature/description` - Feature branches from main
- `fix/description` - Bug fix branches

#### Commit Standards
```bash
# ✅ Conventional commit format (ENFORCED)
feat: add DNS-based service discovery for local development
fix: resolve golangci-lint issues in tenant service  
docs: update API documentation for new endpoints
refactor: extract helper functions to reduce complexity

# ✅ ALWAYS include generation attribution
🤖 Generated with [Claude Code](https://claude.ai/code)
Co-Authored-By: Claude <noreply@anthropic.com>
```

#### Pre-commit Hooks (AUTOMATIC)
- **Format check**: goimports, gofmt
- **Lint check**: golangci-lint (must pass with 0 issues)
- **Test execution**: All tests must pass  
- **Security scan**: gosec, govulncheck
- **Binary detection**: Prevent accidental commits of build artifacts

#### .gitignore (COMPREHENSIVE)
```gitignore
# Build artifacts (STRICTLY ENFORCED)
vm-proxy-auth
gateway  
prometheus-oauth-gateway
bin/
build/
dist/

# Development files
*.log
.env*
air_tmp/
coverage.*
lint_output.json
```

## Architecture-Specific Guidelines

### PromQL Query Filtering (CRITICAL COMPONENT)
- **Official Prometheus parser ONLY** - Never implement custom parsers
- **AST traversal for tenant injection** - Modify all metric references
- **Comprehensive test coverage** - Include complex nested queries
- **Performance benchmarks** - Monitor parsing overhead

### State Storage (DISTRIBUTED)
- **Raft consensus for cluster coordination** - Leader election, log replication
- **Redis for high-performance caching** - With proper failover handling  
- **Local storage for single-instance deployments** - Development/testing only
- **Factory pattern for storage creation** - Clean abstraction layer

### Load Balancing (PRODUCTION-READY)
- **Multiple strategies**: Round-robin, weighted, least-connections
- **Health checking with circuit breaker** - Automatic failover/recovery
- **Metrics collection** - Request distribution, backend health  
- **Queue-based request handling** - For high availability

## Code Quality Automation

### golangci-lint Configuration (ZERO-TOLERANCE)
```yaml
# .golangci.yml - 80+ enabled linters
run:
  timeout: 10m
  issues-exit-code: 1  # Fail CI on any issues

issues:
  max-issues-per-linter: 0  # Show all issues
  max-same-issues: 0        # Show duplicate issues
```

### Security Standards
- **gosec**: Static security analysis (no exceptions)
- **govulncheck**: Known vulnerability scanning
- **Dependency auditing**: Regular updates with security validation
- **No hardcoded secrets**: Use environment variables or config files

### Performance Requirements  
- **Benchmark tests for critical paths** - Especially PromQL parsing
- **Memory allocation monitoring** - Prevent excessive allocations
- **HTTP streaming for large responses** - Minimize memory usage
- **Connection pooling** - Efficient resource utilization

## Deployment Guidelines

### Configuration Management
- **Environment-specific configs** in `examples/` directory
- **Validation on startup** - Fail fast for invalid configurations  
- **Environment variable overrides** - 12-factor app compliance
- **No secrets in config files** - Use external secret management

### Containerization
```dockerfile
# Multi-stage builds for minimal production images
FROM golang:1.22-alpine AS builder
# ... build stage

FROM alpine:latest AS production  
# Minimal production image with only required dependencies
```

### Health Checks
- `/health` - Application health status
- `/ready` - Readiness for traffic
- `/metrics` - Prometheus metrics endpoint

## Development Workflow (ENFORCED)

### Before Starting Work
1. `git pull` - Get latest changes
2. `make clean` - Clean build artifacts  
3. `make quality` - Ensure baseline quality

### During Development
1. **Write tests first** - TDD approach encouraged
2. **Use testutils** - No custom mocks
3. **Run `make quality` frequently** - Catch issues early

### Before Committing
1. `make quality` - MUST pass with 0 issues
2. `git add` - Stage changes
3. Pre-commit hooks will run automatically
4. Only commit if all checks pass

### Pull Request Requirements  
- **All tests passing** in CI/CD pipeline
- **0 golangci-lint issues** 
- **No decrease in code coverage**
- **Updated documentation** if APIs changed
- **Security scan passed**

## Forbidden Practices (IMMEDIATE REJECTION)

❌ **Legacy/Backward Compatibility Code**: Remove immediately, use feature flags if needed
❌ **Custom Mock Implementations**: Use testutils only
❌ **Global Variables**: Use dependency injection  
❌ **assert.X() in tests**: Use require.X() for critical checks
❌ **Binary files in git**: All build artifacts must be gitignored
❌ **Hardcoded secrets**: Use environment variables or secure config
❌ **Custom logging**: Use domain.Logger interface only
❌ **Ignoring golangci-lint issues**: Fix immediately, no exceptions
❌ **Skipped tests in CI**: All tests must pass
❌ **Custom error types without HTTP status**: Use domain.AppError

## Best Practices (MANDATORY ADOPTION)

### Factory Pattern Usage
```go
// ✅ Standardized factory pattern
func NewServiceFactory(deps Dependencies) *ServiceFactory {
    return &ServiceFactory{deps: deps}
}

func (f *ServiceFactory) CreateAuthService() (domain.AuthService, error) {
    return auth.NewService(f.deps.Logger, f.deps.Config.Auth)
}
```

### Interface Segregation
```go
// ✅ Small, focused interfaces
type TokenValidator interface {
    ValidateToken(ctx context.Context, token string) (*Claims, error)
}

type UserProvider interface {  
    GetUser(ctx context.Context, userID string) (*User, error)
}

// ❌ FORBIDDEN: Large interfaces with multiple responsibilities
```

### Context Propagation
```go
// ✅ Always accept and forward context
func (s *Service) ProcessRequest(ctx context.Context, req *Request) error {
    user, err := s.auth.ValidateToken(ctx, req.Token)
    if err != nil {
        return fmt.Errorf("auth failed: %w", err)  
    }
    return s.proxy.ForwardRequest(ctx, user, req)
}
```

This CLAUDE.md establishes non-negotiable standards for code quality, testing, and development practices. Any deviation from these standards requires explicit justification and must be approved through the PR review process.