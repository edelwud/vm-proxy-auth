# Production Scenarios

Production-ready configurations for different deployment patterns.

## Available Configurations

### `single-instance.yaml`
Production single-node deployment:
- **Storage**: Redis for state management
- **Auth**: RS256 JWT with JWKS
- **Proxy**: Multiple weighted upstreams
- **Security**: Full JWT validation enabled
- **Use case**: Single-instance production deployments

### `ha-cluster.yaml`
High-availability cluster deployment:
- **Storage**: Raft consensus for HA
- **Clustering**: Memberlist with static discovery
- **Auth**: RS256 JWT with JWKS
- **Discovery**: Static peers for enterprise environments
- **Use case**: Enterprise HA deployments

## Required Environment Variables

### Single Instance (`single-instance.yaml`)

```bash
# Redis connection
export VM_PROXY_AUTH_STORAGE_REDIS_ADDRESS="redis:6379"
export VM_PROXY_AUTH_STORAGE_REDIS_PASSWORD="secure-redis-password"

# JWT authentication
export VM_PROXY_AUTH_AUTH_JWT_JWKS_URL="https://auth.company.com/.well-known/jwks.json"
```

### HA Cluster (`ha-cluster.yaml`)

```bash
# Node identification (unique per node)
export VM_PROXY_AUTH_METADATA_NODE_ID="vm-proxy-auth-node-1"
export VM_PROXY_AUTH_METADATA_REGION="us-east-1"

# Raft storage
export VM_PROXY_AUTH_STORAGE_RAFT_BIND_ADDRESS="10.0.1.5:9000"

# Cluster networking
export VM_PROXY_AUTH_CLUSTER_MEMBERLIST_BIND_ADDRESS="10.0.1.5:7946"
export VM_PROXY_AUTH_CLUSTER_MEMBERLIST_ADVERTISE_ADDRESS="10.0.1.5:7946"
export VM_PROXY_AUTH_CLUSTER_MEMBERLIST_PEERS_JOIN="10.0.1.6:7946,10.0.1.7:7946"
export VM_PROXY_AUTH_CLUSTER_MEMBERLIST_PEERS_ENCRYPTION="base64-encoded-32-byte-key"

# JWT authentication
export VM_PROXY_AUTH_AUTH_JWT_JWKS_URL="https://auth.company.com/.well-known/jwks.json"
```

## Deployment Patterns

### Single Instance with Load Balancer

```yaml
# Use single-instance.yaml
# Deploy behind ALB/NGINX with:
# - SSL termination
# - Health checks on /metrics
# - Session affinity if needed
```

### HA Cluster (3+ nodes)

```yaml
# Use ha-cluster.yaml with:
# - 3 or 5 nodes for Raft quorum
# - Static discovery for reliable bootstrapping
# - Shared persistent storage for Raft data
# - Load balancer across all nodes
```

## Security Considerations

### Network Security
- **Encrypt cluster traffic**: Set encryption key for memberlist
- **Firewall rules**: Restrict access to cluster ports
- **TLS termination**: Use HTTPS for client connections

### Authentication
- **JWKS rotation**: Ensure JWKS endpoint is accessible
- **Token validation**: Enable audience and issuer validation
- **Tenant isolation**: Configure proper tenant mappings

## Monitoring

### Health Checks
```bash
# Application health
curl http://localhost:8080/metrics

# Cluster health (HA setup)
curl http://localhost:8080/metrics | grep memberlist
curl http://localhost:8080/metrics | grep raft
```

### Key Metrics
- `vm_proxy_auth_requests_total`: Request metrics
- `vm_proxy_auth_memberlist_members`: Cluster size
- `vm_proxy_auth_raft_leader`: Raft leadership
- `vm_proxy_auth_backend_health`: Backend status

## Troubleshooting

### Single Instance Issues
1. **Redis connectivity**: Check `storage.redis.address`
2. **JWKS access**: Verify `auth.jwt.jwksUrl` reachability
3. **Backend health**: Monitor upstream health checks

### HA Cluster Issues
1. **Split brain**: Ensure odd number of nodes (3, 5, 7)
2. **Network partitions**: Check memberlist connectivity
3. **Raft leadership**: Only one leader should exist
4. **Discovery failures**: Verify static peer addresses

### Common Solutions
- **DNS resolution**: Ensure all hostnames resolve correctly
- **Port conflicts**: Verify no port conflicts on cluster nodes  
- **Persistent storage**: Use persistent volumes for Raft data
- **Load balancer**: Configure proper health checks