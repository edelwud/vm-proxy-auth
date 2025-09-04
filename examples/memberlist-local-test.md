# Memberlist Local Testing Guide

## Prerequisites
1. Build the application: `make build`
2. Ensure VictoriaMetrics is running on localhost:8428 or remove backend validation

## Test Scenario: 2-Node Cluster

### Terminal 1 (First Node - Seed Node)
```bash
# Create data directory
mkdir -p ./data/raft-node-1

# Start first node
./bin/vm-proxy-auth --config examples/config.memberlist-local.yaml
```

### Terminal 2 (Second Node)
```bash
# Create data directory  
mkdir -p ./data/raft-node-2

# Start second node
./bin/vm-proxy-auth --config examples/config.memberlist-local-node2.yaml
```

## Expected Behavior

When both nodes are running, you should see:

**Node 1 logs:**
```
INFO[...] Starting memberlist service
INFO[...] Successfully joined memberlist cluster nodes_contacted=1
INFO[...] Memberlist service initialized cluster_size=2
```

**Node 2 logs:**
```
INFO[...] Starting memberlist service  
INFO[...] Joining memberlist cluster join_nodes=[127.0.0.1:7946 127.0.0.1:7947 127.0.0.1:7948]
INFO[...] Successfully joined memberlist cluster nodes_contacted=1
INFO[...] Memberlist service initialized cluster_size=2
```

## Verification Commands

Check cluster membership:
```bash
# Node 1 metrics
curl localhost:8080/metrics | grep memberlist

# Node 2 metrics  
curl localhost:8081/metrics | grep memberlist
```

## Troubleshooting

If nodes don't find each other:
1. Check that ports 7946, 7947 are not in use: `netstat -an | grep 794[6-7]`
2. Check firewall settings
3. Verify bind addresses in config
4. Look for "Successfully joined" or "Failed to join" messages in logs