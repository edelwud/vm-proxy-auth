# Development Scenarios

Development configurations for local testing and development.

## Available Configurations

### `basic.yaml`
Simple single-node development setup:
- **Storage**: Local (in-memory)
- **Auth**: HS256 with shared secret
- **Proxy**: Single upstream (localhost:8428)
- **Logging**: Debug level, pretty format
- **Use case**: Local development, testing

**Usage:**
```bash
./bin/vm-proxy-auth --config configs/scenarios/development/basic.yaml
```

### `with-discovery.yaml`
Multi-node development with auto-discovery:
- **Storage**: Raft consensus
- **Clustering**: mDNS auto-discovery
- **Auth**: HS256 with shared secret
- **Use case**: Testing distributed features locally

**Usage:**
```bash
# Terminal 1 (Node 1)
mkdir -p ./data/raft-node-1
./bin/vm-proxy-auth --config configs/scenarios/development/with-discovery.yaml

# Terminal 2 (Node 2) - requires separate config with different ports
mkdir -p ./data/raft-node-2
./bin/vm-proxy-auth --config configs/scenarios/development/with-discovery-node2.yaml
```

## Environment Variables

Common environment variables for development:

```bash
# Node identification
export VM_PROXY_AUTH_METADATA_NODE_ID="dev-node-1"

# Logging
export VM_PROXY_AUTH_LOGGING_LEVEL="debug"
export VM_PROXY_AUTH_LOGGING_FORMAT="pretty"

# Development auth
export VM_PROXY_AUTH_AUTH_JWT_SECRET="your-dev-secret-32-chars-long"
```

## Prerequisites

1. **VictoriaMetrics**: Running on localhost:8428
   ```bash
   docker run -p 8428:8428 victoriametrics/victoria-metrics
   ```

2. **Build application**: 
   ```bash
   make build
   ```

## Testing

Test the development setup:

```bash
# Generate test JWT token (for HS256)
curl -X POST http://localhost:8080/auth/token \
  -H "Content-Type: application/json" \
  -d '{"groups": ["admin"]}'

# Test proxy with authentication
curl -H "Authorization: Bearer <token>" \
  http://localhost:8080/api/v1/query?query=up
```

## Network Ports

Development configurations use these ports:

- **8080**: HTTP API
- **7946**: Memberlist (clustering)
- **9010**: Raft consensus
- **8428**: VictoriaMetrics upstream