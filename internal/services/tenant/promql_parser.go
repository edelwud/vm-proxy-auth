package tenant

import (
	"fmt"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// Remove local error as we'll use domain error

// PromQLTenantInjector provides production-ready PromQL parsing and tenant injection.
type PromQLTenantInjector struct {
	logger domain.Logger
}

// NewPromQLTenantInjector creates a new production ready PromQL tenant injector.
func NewPromQLTenantInjector(logger domain.Logger) *PromQLTenantInjector {
	return &PromQLTenantInjector{
		logger: logger,
	}
}

// InjectTenantLabels parses PromQL using Prometheus parser and injects tenant labels.
func (p *PromQLTenantInjector) InjectTenantLabels(query string, vmTenants []domain.VMTenant, cfg *config.UpstreamConfig) (string, error) {
	p.logger.Debug("Starting production PromQL tenant injection",
		domain.Field{Key: "original_query", Value: query},
		domain.Field{Key: "vm_tenants", Value: fmt.Sprintf("%v", vmTenants)})

	if len(vmTenants) == 0 {
		p.logger.Warn("No VM tenants provided for query filtering")

		return query, domain.ErrNoVMTenantsForFiltering
	}

	// Parse the query using Prometheus parser
	expr, err := parser.ParseExpr(query)
	if err != nil {
		p.logger.Error("Failed to parse PromQL query",
			domain.Field{Key: "query", Value: query},
			domain.Field{Key: "error", Value: err.Error()})

		return "", fmt.Errorf("failed to parse PromQL query: %w", err)
	}

	// Walk the AST and inject tenant labels
	parser.Inspect(expr, func(node parser.Node, path []parser.Node) error {
		// Find Vector Selectors (metric names with optional labels)
		if vs, ok := node.(*parser.VectorSelector); ok {
			p.injectTenantLabelsToVectorSelector(vs, vmTenants, cfg)
		}

		return nil
	})

	// Convert back to string
	result := expr.String()

	p.logger.Debug("Production PromQL injection completed",
		domain.Field{Key: "original_query", Value: query},
		domain.Field{Key: "filtered_query", Value: result},
		domain.Field{Key: "modified", Value: query != result},
		domain.Field{Key: "vm_tenants_count", Value: len(vmTenants)})

	return result, nil
}

// injectTenantLabelsToVectorSelector injects tenant labels into a vector selector.
func (p *PromQLTenantInjector) injectTenantLabelsToVectorSelector(
	vs *parser.VectorSelector,
	vmTenants []domain.VMTenant,
	cfg *config.UpstreamConfig,
) {
	// Check if tenant labels already exist
	tenantLabelName := cfg.TenantLabel
	if tenantLabelName == "" {
		tenantLabelName = "vm_account_id"
	}

	projectLabelName := cfg.ProjectLabel
	if projectLabelName == "" {
		projectLabelName = "vm_project_id"
	}

	// Check if we already have tenant filtering
	for _, matcher := range vs.LabelMatchers {
		if matcher.Name == tenantLabelName {
			p.logger.Debug("Tenant label already exists, skipping",
				domain.Field{Key: "metric", Value: vs.Name},
				domain.Field{Key: "existing_label", Value: tenantLabelName})

			return
		}
	}

	// Add tenant filtering based on number of VM tenants
	if len(vmTenants) == 1 {
		// Single tenant - simple case
		tenant := vmTenants[0]
		p.addSingleTenantFilter(vs, tenant, tenantLabelName, projectLabelName, cfg.UseProjectID)
	} else {
		// Multiple tenants - create complex filter
		p.addMultipleTenantFilter(vs, vmTenants, tenantLabelName, projectLabelName, cfg.UseProjectID)
	}
}

// addSingleTenantFilter adds a single tenant filter to vector selector.
func (p *PromQLTenantInjector) addSingleTenantFilter(
	vs *parser.VectorSelector,
	tenant domain.VMTenant,
	tenantLabel, projectLabel string,
	useProjectID bool,
) {
	// Add account ID filter
	accountMatcher := &labels.Matcher{
		Type:  labels.MatchEqual,
		Name:  tenantLabel,
		Value: tenant.AccountID,
	}
	vs.LabelMatchers = append(vs.LabelMatchers, accountMatcher)

	// Add project ID filter if configured and present
	if useProjectID && tenant.ProjectID != "" {
		projectMatcher := &labels.Matcher{
			Type:  labels.MatchEqual,
			Name:  projectLabel,
			Value: tenant.ProjectID,
		}
		vs.LabelMatchers = append(vs.LabelMatchers, projectMatcher)
	}

	p.logger.Debug("Added single tenant filter to metric",
		domain.Field{Key: "metric", Value: vs.Name},
		domain.Field{Key: "account_id", Value: tenant.AccountID},
		domain.Field{Key: "project_id", Value: tenant.ProjectID})
}

// addMultipleTenantFilter adds multiple tenant filter using regex.
func (p *PromQLTenantInjector) addMultipleTenantFilter(
	vs *parser.VectorSelector,
	vmTenants []domain.VMTenant,
	tenantLabel, projectLabel string,
	useProjectID bool,
) {
	// For now, use first tenant (simple approach)
	// In future, implement proper OR logic for multiple tenants
	if len(vmTenants) > 0 {
		p.logger.Debug("Multiple tenants found, using first tenant for now",
			domain.Field{Key: "tenant_count", Value: len(vmTenants)},
			domain.Field{Key: "selected_tenant", Value: vmTenants[0].String()})

		p.addSingleTenantFilter(vs, vmTenants[0], tenantLabel, projectLabel, useProjectID)
	}
}
