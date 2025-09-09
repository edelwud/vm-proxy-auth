package filterstrategies

import (
	"fmt"
	"sort"
	"strings"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/edelwud/vm-proxy-auth/internal/config/modules/tenant"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

const (
	defaultTenantLabel  = "vm_account_id"
	defaultProjectLabel = "vm_project_id"
)

// ORQueryBuilder creates secure OR-based queries for multi-tenant filtering.
// This builder ensures exact tenant isolation by creating separate conditions
// for each tenant pair, preventing cross-tenant data leakage.
type ORQueryBuilder struct {
	logger domain.Logger
}

// NewORQueryBuilder creates a new OR query builder.
func NewORQueryBuilder(logger domain.Logger) *ORQueryBuilder {
	return &ORQueryBuilder{
		logger: logger,
	}
}

// BuildSecureQuery takes a PromQL query and rewrites it to use secure OR conditions
// for multiple tenants instead of regex patterns that create security vulnerabilities.
func (b *ORQueryBuilder) BuildSecureQuery(
	originalQuery string,
	tenants []domain.VMTenant,
	tenantCfg *tenant.FilterConfig,
) (string, error) {
	if len(tenants) == 0 {
		return "", domain.ErrNoVMTenantsForFiltering
	}

	if len(tenants) == 1 {
		// Single tenant - inject labels directly without OR
		promqlInjector := NewPromQLTenantInjector(b.logger)
		return promqlInjector.InjectTenantLabels(originalQuery, tenants, tenantCfg)
	}

	// Parse the original query
	expr, err := parser.ParseExpr(originalQuery)
	if err != nil {
		return "", fmt.Errorf("failed to parse original query: %w", err)
	}

	// Create OR expression for multiple tenants
	orExpr := b.createSecureORExpression(expr, tenants, tenantCfg)

	result := orExpr.String()

	b.logger.Info("Created secure OR query",
		domain.Field{Key: "original_query", Value: originalQuery},
		domain.Field{Key: "secure_query", Value: result},
		domain.Field{Key: "tenant_count", Value: len(tenants)})

	return result, nil
}

// createSecureORExpression creates a secure OR expression where each metric
// in the original query is duplicated for each tenant with exact tenant labels.
func (b *ORQueryBuilder) createSecureORExpression(
	originalExpr parser.Expr,
	tenants []domain.VMTenant,
	tenantCfg *tenant.FilterConfig,
) parser.Expr {
	// Group tenants for optimization
	tenantGroups := b.optimizeGroups(tenants, tenantCfg)

	if len(tenantGroups) == 1 {
		// Only one effective tenant group - no OR needed
		// Still need to inject tenant to the expression
		return b.injectTenantToExpression(originalExpr, tenantGroups[0], tenantCfg)
	}

	// Create OR expressions for each tenant group
	var orExpressions []parser.Expr

	for _, group := range tenantGroups {
		clonedExpr := b.cloneExpression(originalExpr)
		modifiedExpr := b.injectTenantToExpression(clonedExpr, group, tenantCfg)
		orExpressions = append(orExpressions, modifiedExpr)
	}

	// Create binary OR expression tree
	result := orExpressions[0]
	for i := 1; i < len(orExpressions); i++ {
		result = &parser.BinaryExpr{
			Op:  parser.LOR,
			LHS: result,
			RHS: orExpressions[i],
		}
	}

	return result
}

// Group represents an optimized group of tenants.
type Group struct {
	AccountID  string
	ProjectIDs []string // empty slice means all projects (wildcard)
	IsWildcard bool     // true if this group allows all projects for the account
}

// optimizeGroups optimizes tenant list by grouping and handling wildcards.
// If a tenant has project_id=".*", it grants access to all projects in that account.
func (b *ORQueryBuilder) optimizeGroups(
	tenants []domain.VMTenant,
	_ *tenant.FilterConfig,
) []Group {
	accountGroups := make(map[string][]string)
	accountWildcards := make(map[string]bool)

	// Group tenants by account ID
	for _, tenant := range tenants {
		if tenant.ProjectID == ".*" || tenant.ProjectID == "" {
			accountWildcards[tenant.AccountID] = true
		} else if !accountWildcards[tenant.AccountID] {
			accountGroups[tenant.AccountID] = append(accountGroups[tenant.AccountID], tenant.ProjectID)
		}
	}

	var groups []Group

	// Create groups for wildcard accounts
	for accountID := range accountWildcards {
		groups = append(groups, Group{
			AccountID:  accountID,
			ProjectIDs: nil,
			IsWildcard: true,
		})
	}

	// Create groups for specific project access
	for accountID, projectIDs := range accountGroups {
		if !accountWildcards[accountID] {
			// Sort for deterministic output
			sort.Strings(projectIDs)
			groups = append(groups, Group{
				AccountID:  accountID,
				ProjectIDs: b.deduplicateStrings(projectIDs),
				IsWildcard: false,
			})
		}
	}

	// Sort groups for deterministic output
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].AccountID < groups[j].AccountID
	})

	b.logger.Debug("Optimized tenant groups",
		domain.Field{Key: "original_tenant_count", Value: len(tenants)},
		domain.Field{Key: "optimized_group_count", Value: len(groups)})

	return groups
}

// deduplicateStrings removes duplicates from string slice while preserving order.
func (b *ORQueryBuilder) deduplicateStrings(input []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(input))

	for _, item := range input {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}

// cloneExpression creates a deep copy of a PromQL expression.
func (b *ORQueryBuilder) cloneExpression(expr parser.Expr) parser.Expr {
	// For now, we'll use string representation to clone
	// This is not the most efficient but works for proof of concept
	cloneStr := expr.String()
	cloned, err := parser.ParseExpr(cloneStr)
	if err != nil {
		// This should never happen with a valid expression
		b.logger.Error("Failed to clone expression", domain.Field{Key: "error", Value: err.Error()})
		return expr
	}
	return cloned
}

// injectTenantToExpression injects tenant filters into all vector selectors in an expression.
func (b *ORQueryBuilder) injectTenantToExpression(
	expr parser.Expr,
	group Group,
	tenantCfg *tenant.FilterConfig,
) parser.Expr {
	tenantLabel := tenantCfg.Labels.Account
	if tenantLabel == "" {
		tenantLabel = defaultTenantLabel
	}

	projectLabel := tenantCfg.Labels.Project
	if projectLabel == "" {
		projectLabel = defaultProjectLabel
	}

	// Walk the AST and inject tenant labels
	parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
		if vs, ok := node.(*parser.VectorSelector); ok {
			b.injectTenantToVectorSelector(vs, group, tenantLabel, projectLabel, tenantCfg.Labels.UseProjectID)
		}
		return nil
	})

	return expr
}

// injectTenantToVectorSelector injects tenant labels into a single vector selector.
func (b *ORQueryBuilder) injectTenantToVectorSelector(
	vs *parser.VectorSelector,
	group Group,
	tenantLabel, projectLabel string,
	useProjectID bool,
) {
	// Check if tenant label already exists
	for _, matcher := range vs.LabelMatchers {
		if matcher.Name == tenantLabel {
			return // Already has tenant filtering
		}
	}

	// Add account ID filter
	accountMatcher := &labels.Matcher{
		Type:  labels.MatchEqual,
		Name:  tenantLabel,
		Value: group.AccountID,
	}
	vs.LabelMatchers = append(vs.LabelMatchers, accountMatcher)

	// Add project ID filter if needed
	if useProjectID && !group.IsWildcard && len(group.ProjectIDs) > 0 {
		if len(group.ProjectIDs) == 1 {
			// Single project - exact match
			projectMatcher := &labels.Matcher{
				Type:  labels.MatchEqual,
				Name:  projectLabel,
				Value: group.ProjectIDs[0],
			}
			vs.LabelMatchers = append(vs.LabelMatchers, projectMatcher)
		} else {
			// Multiple projects - regex match (safe because same account)
			projectPattern := "(" + strings.Join(group.ProjectIDs, "|") + ")"
			projectMatcher := &labels.Matcher{
				Type:  labels.MatchRegexp,
				Name:  projectLabel,
				Value: projectPattern,
			}
			vs.LabelMatchers = append(vs.LabelMatchers, projectMatcher)
		}
	}

	b.logger.Debug("Injected tenant to vector selector",
		domain.Field{Key: "metric", Value: vs.Name},
		domain.Field{Key: "account_id", Value: group.AccountID},
		domain.Field{Key: "project_count", Value: len(group.ProjectIDs)},
		domain.Field{Key: "is_wildcard", Value: group.IsWildcard})
}
