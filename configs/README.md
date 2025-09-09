# VM Proxy Auth Configuration

This directory contains configuration files and templates for vm-proxy-auth deployment.

## Directory Structure

```
configs/
├── scenarios/              # Ready-to-use configuration scenarios
│   ├── development/        # Development configurations
│   ├── production/         # Production configurations
│   └── testing/           # Testing configurations
├── reference/             # Reference configurations
│   ├── full-example.yaml # Complete configuration with all options
│   └── minimal.yaml      # Minimal required configuration
└── templates/            # Deployment templates
    ├── docker/           # Docker Compose templates
    ├── kubernetes/       # Kubernetes manifests
    └── systemd/          # Systemd service files
```

## New Configuration Structure

The configuration has been reorganized with a cleaner, more intuitive structure:

### Top-level Sections
- **metadata**: Node identification and shared service data
- **logging**: Logging configuration (moved to top level)
- **metrics**: Metrics configuration (moved to top level)
- **server**: HTTP server configuration (renamed from network)
- **proxy**: Proxy and load balancing configuration
- **auth**: Authentication configuration (simplified from security.authentication)
- **tenants**: Tenant management and authorization
- **storage**: State storage configuration (local/redis/raft)
- **cluster**: Cluster configuration (only active when storage.type=raft)

### Key Improvements
- **Optional fields**: `raft.peers` and `discovery.static` are now optional
- **Full addresses**: Memberlist uses complete addresses (`host:port` format)
- **Conditional activation**: Cluster configuration only active with Raft storage
- **Environment variables**: All settings can be overridden via environment variables

## Quick Start

### Development
```bash
# Basic single-node development
./bin/vm-proxy-auth --config configs/scenarios/development/basic.yaml

# Multi-node with auto-discovery
./bin/vm-proxy-auth --config configs/scenarios/development/with-discovery.yaml
```

### Production
```bash
# Single instance with Redis
./bin/vm-proxy-auth --config configs/scenarios/production/single-instance.yaml

# HA cluster with Raft
./bin/vm-proxy-auth --config configs/scenarios/production/ha-cluster.yaml
```

## Configuration Examples

### Minimal Configuration
```yaml
metadata:
  nodeId: "vm-proxy-auth-1"
  role: "gateway"
  environment: "development"
  region: "local"

server:
  address: "0.0.0.0:8080"

proxy:
  upstreams:
    - url: "http://localhost:8428"
      weight: 1

auth:
  jwt:
    algorithm: "HS256"
    secret: "dev-secret-key-32-chars-long"

tenants:
  mappings:
    - groups: ["admin"]
      vmTenants:
        - account: "0"
      permissions: ["read", "write"]

storage:
  type: "local"
```

### Production HA Cluster
```yaml
metadata:
  nodeId: "vm-proxy-auth-node-1"
  role: "gateway"
  environment: "production"
  region: "us-east-1"

storage:
  type: "raft"
  raft:
    bindAddress: "10.0.1.5:9000"
    dataDir: "/data/raft"
    bootstrapExpected: 3

cluster:
  memberlist:
    bindAddress: "10.0.1.5:7946"
    advertiseAddress: "10.0.1.5:7946"
  
  discovery:
    static:
      peers:
        - "vm-proxy-auth-seed-1:7946"
        - "vm-proxy-auth-seed-2:7946"
    mdns:
      enabled: false  # Disabled in production
```

## Environment Variables

All configuration options can be overridden via environment variables using the pattern:
`VM_PROXY_AUTH_<SECTION>_<SUBSECTION>_<KEY>`

### Common Environment Variables
```bash
# Node identification
export VM_PROXY_AUTH_METADATA_NODE_ID="unique-node-id"
export VM_PROXY_AUTH_METADATA_REGION="us-east-1"

# Server
export VM_PROXY_AUTH_SERVER_ADDRESS="0.0.0.0:8080"

# Authentication
export VM_PROXY_AUTH_AUTH_JWT_ALGORITHM="RS256"
export VM_PROXY_AUTH_AUTH_JWT_JWKS_URL="https://auth.company.com/.well-known/jwks.json"
export VM_PROXY_AUTH_AUTH_JWT_SECRET="hs256-secret-key"

# Storage
export VM_PROXY_AUTH_STORAGE_TYPE="raft"
export VM_PROXY_AUTH_STORAGE_REDIS_ADDRESS="redis:6379"
export VM_PROXY_AUTH_STORAGE_RAFT_BIND_ADDRESS="10.0.1.5:9000"

# Clustering
export VM_PROXY_AUTH_CLUSTER_MEMBERLIST_BIND_ADDRESS="10.0.1.5:7946"
export VM_PROXY_AUTH_CLUSTER_MEMBERLIST_ADVERTISE_ADDRESS="10.0.1.5:7946"
export VM_PROXY_AUTH_CLUSTER_MEMBERLIST_PEERS_JOIN="10.0.1.6:7946,10.0.1.7:7946"
export VM_PROXY_AUTH_CLUSTER_MEMBERLIST_PEERS_ENCRYPTION="base64-key"

# Logging
export VM_PROXY_AUTH_LOGGING_LEVEL="info"
export VM_PROXY_AUTH_LOGGING_FORMAT="json"
```

## Migration from Old Configuration

The new configuration structure is **not backward compatible** with the old `internal/config/viper_config.go` structure. Key changes:

### Renamed Sections
- `network` → `server`
- `security.authentication` → `auth`
- `tenantMapping` → `tenants.mappings`

### Restructured Sections
- `logging` and `metrics` moved to top level
- `cluster` section only active with Raft storage
- `discovery.static.peers` is now optional

### New Optional Fields
- `storage.raft.peers` - can be omitted (filled by memberlist)
- `cluster.discovery.static` - optional static peer discovery

## Deployment Templates

### Docker Compose
```bash
cd configs/templates/docker
docker-compose up -d
```

### Kubernetes
```bash
cd configs/templates/kubernetes
kubectl apply -f .
```

### Systemd
```bash
sudo cp configs/templates/systemd/vm-proxy-auth.service /etc/systemd/system/
sudo systemctl enable --now vm-proxy-auth
```

## Validation

The new configuration system provides comprehensive validation:

- **Syntax validation**: YAML structure and types
- **Semantic validation**: Cross-field dependencies
- **Module validation**: Each section validates independently
- **Environment integration**: Environment variables override file values

## Support

For configuration questions:
1. Check scenario examples for your use case
2. Review reference/full-example.yaml for all options
3. Consult module-specific documentation
4. Use environment variables for runtime overrides