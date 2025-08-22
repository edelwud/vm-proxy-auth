# Security Guide: Multi-Tenant Filtering

## ⚠️ Critical Security Issue: Cartesian Product Vulnerability

### The Problem

The legacy regex-based tenant filtering strategy creates a **cartesian product** security vulnerability when users have access to multiple tenants with mixed project permissions.

#### Example Scenario
User has OAuth scopes granting access to:
- Account `1000` with **all projects** (`.*`)  
- Account `2000` with **project `20` only**

#### Legacy Regex Strategy (UNSAFE)
```promql
# Generated filter
{vm_account_id=~"(1000|2000)",vm_project_id=~"(.*|20)"}

# This creates 4 combinations:
# ✅ account=1000, project=.*     (authorized)
# ✅ account=1000, project=20     (authorized) 
# ❌ account=2000, project=.*     (SECURITY BREACH!)
# ✅ account=2000, project=20     (authorized)
```

**Result**: User gains unauthorized access to ALL projects in account `2000`!

#### Secure OR Strategy (SAFE)
```promql
# Generated filter  
{vm_account_id="1000"} or {vm_account_id="2000",vm_project_id="20"}

# This creates exactly 2 conditions:
# ✅ account=1000 (any project)   (authorized)
# ✅ account=2000, project=20     (authorized)
```

**Result**: Perfect tenant isolation with no cross-tenant leakage.

## 🛡️ Secure Configuration

### Enable OR-Based Filtering

```yaml
upstream:
  tenant_filter:
    strategy: "or_conditions"  # Secure strategy - this is now the only supported strategy
```

### Strategy Comparison

| Strategy | Security | Performance | Use Case |
|----------|----------|-------------|----------|
| `or_conditions` | ✅ Secure | 📊 Moderate | Multi-tenant production environments |

### Migration Guide

1. **Audit Current Tenants**: Review users with multiple tenant access
2. **Validate Queries**: Ensure PromQL queries work correctly with OR-based filtering
3. **Monitor Performance**: Watch for query complexity increases
4. **Optimize Tenant Mappings**: Reduce complex tenant combinations where possible

## 🔍 Query Examples

### Simple Metric
```promql
# Original
up

# Regex Strategy (unsafe)
up{vm_account_id=~"(1000|2000)",vm_project_id=~"(.*|20)"}

# OR Strategy (secure)
up{vm_account_id="1000"} or up{vm_account_id="2000",vm_project_id="20"}
```

### Complex Division
```promql
# Original
rate(http_requests_total[5m]) / rate(http_requests_duration_seconds_count[5m])

# OR Strategy (secure)
rate(http_requests_total{vm_account_id="1000"}[5m]) / rate(http_requests_duration_seconds_count{vm_account_id="1000"}[5m]) 
or 
rate(http_requests_total{vm_account_id="2000",vm_project_id="20"}[5m]) / rate(http_requests_duration_seconds_count{vm_account_id="2000",vm_project_id="20"}[5m])
```

## 📊 Performance Considerations

### Query Complexity
- **Single Tenant**: No performance impact
- **Multiple Tenants**: Linear increase with tenant count
- **Complex Queries**: Each metric duplicated per tenant

### Optimization Features
- **Wildcard Grouping**: `project_id: ".*"` optimized to no project filter
- **Tenant Deduplication**: Duplicate tenants automatically merged
- **Smart Fallback**: Single tenant uses simple filtering

## 🚨 Security Best Practices

### 1. Principle of Least Privilege
```yaml
tenant_mappings:
  - groups: ["dev-team"]
    vm_tenants:
      - account_id: "1000"
        project_id: "frontend"  # Specific project only
    read_only: false
```

### 2. Audit Multi-Tenant Access
```yaml
tenant_mappings:
  - groups: ["security-audit"]
    vm_tenants:
      - account_id: "1000"
        project_id: ".*"
      - account_id: "2000" 
        project_id: "20"
    read_only: true  # Read-only for auditing
```

### 3. Monitor Cross-Tenant Patterns
```bash
# Check for users with wildcard + specific project combinations
grep -E "(.*|[0-9]+)" config.yaml
```

## 🔧 Troubleshooting

### Common Issues

#### Issue: Queries Too Complex
```yaml
# Solution: Use more specific tenant mappings
tenant_mappings:
  - groups: ["team-a"]
    vm_tenants:
      - account_id: "1000"  # Remove project_id for account-level access
```

#### Issue: Performance Degradation  
```yaml
# Solution: Optimize tenant mappings to reduce complex tenant combinations
tenant_mappings:
  - groups: ["team-a"]
    vm_tenants:
      - account_id: "1000"  # Use more specific tenant grants
```

#### Issue: Dashboard Compatibility
- OR strategy may change query semantics
- Test dashboards with new filtering before production
- Consider tenant-specific dashboards

## 📈 Monitoring

### Key Metrics
- `tenant_filter_duration_seconds`: Query filtering performance
- `tenant_filter_applied_total`: Number of filtered queries
- `tenant_access_denied_total`: Access violations

### Alerting Rules
```yaml
groups:
- name: tenant-security
  rules:
  - alert: CrossTenantAccessAttempt
    expr: increase(tenant_access_denied_total[5m]) > 0
    annotations:
      summary: "Potential cross-tenant access attempt detected"
```

## 🔄 Backward Compatibility

The OR strategy is now the **only supported strategy** for tenant filtering:

```yaml
upstream:
  tenant_filter:
    strategy: "or_conditions"  # Only supported strategy
```

Legacy configurations without the `tenant_filter` section will default to the secure OR-based strategy.