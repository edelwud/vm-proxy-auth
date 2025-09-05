# Auto-Discovery Testing Guide

## Overview
This guide demonstrates automatic peer discovery using mDNS. No manual `joinNodes` configuration needed!

## Prerequisites
1. Build the application: `make build`
2. Ensure VictoriaMetrics is running on localhost:8428 or remove backend validation

## Test Scenario: Auto-Discovery with mDNS

### Terminal 1 (First Node)
```bash
# Create data directory
mkdir -p ./data/raft-node-1

# Start first node with auto-discovery
./bin/vm-proxy-auth --config examples/config.auto-discovery.yaml
```

Expected logs:
```
INFO[...] Discovery service configured providers=["mdns","static"]
INFO[...] mDNS provider started service_name="_vm-proxy-auth._tcp" hostname="vm-proxy-auth-1"
INFO[...] Starting auto-discovery providers_count=2
```

### Terminal 2 (Second Node) 
```bash
# Create data directory  
mkdir -p ./data/raft-node-2

# Start second node - it will automatically discover first node!
./bin/vm-proxy-auth --config examples/config.auto-discovery-node2.yaml
```

Expected logs:
```
INFO[...] Discovery service configured providers=["mdns","static"]  
INFO[...] mDNS provider started service_name="_vm-proxy-auth._tcp" hostname="vm-proxy-auth-2"
INFO[...] Discovered peers via mDNS service="_vm-proxy-auth._tcp" peers_count=1
INFO[...] Attempting to join discovered peers peers_count=1 peers=["127.0.0.1:7946"]
INFO[...] Successfully joined memberlist cluster nodes_contacted=1
```

## Key Benefits

✅ **No manual joinNodes** - nodes find each other automatically  
✅ **mDNS announcement** - each node announces itself on startup  
✅ **mDNS discovery** - periodic scanning for new peers  
✅ **Static fallback** - configuration backup if mDNS fails  
✅ **Automatic integration** - discovery → memberlist → raft

## Configuration Features

```yaml
discovery:
  enabled: true
  providers: ["mdns", "static"]  # Multiple strategies
  interval: "15s"                # Discovery frequency
  mdns:
    serviceName: "_vm-proxy-auth._tcp"
    hostname: "vm-proxy-auth-1"  # Unique per node
  static:
    peers: []                    # Fallback list
    
memberlist:
  # joinNodes: []               # No longer needed!
  bindPort: 7946               # Must be unique per node
```

## Verification Commands

Check cluster membership:
```bash
# Node 1 
curl localhost:8080/metrics | grep memberlist

# Node 2  
curl localhost:8081/metrics | grep memberlist
```

Check discovery status:
```bash
# Look for discovery logs in application output
grep -i "discovery\|mdns" logs.txt
```

## Troubleshooting

If auto-discovery doesn't work:

1. **Check mDNS availability**: `dns-sd -B _vm-proxy-auth._tcp`
2. **Verify firewall**: Allow UDP 5353 (mDNS) and TCP 7946/7947 (memberlist)
3. **Check network interfaces**: Ensure non-loopback interfaces available
4. **Enable debug logging**: Set `logging.level: "debug"`
5. **Fallback to static**: Add peers to `discovery.static.peers`

## Network Requirements

- **UDP 5353**: mDNS multicast
- **TCP 7946-7948**: Memberlist gossip protocol  
- **TCP 9000-9002**: Raft consensus
- **TCP 8080-8082**: HTTP API

## Advanced Features

- **Service TXT records**: Contains version, role metadata
- **Multiple providers**: Can combine mDNS + static + future providers
- **Health integration**: Unhealthy nodes excluded from discovery
- **Cross-region support**: Filter by metadata (region, environment)