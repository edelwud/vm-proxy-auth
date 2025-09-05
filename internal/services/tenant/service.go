package tenant

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"

	"github.com/edelwud/vm-proxy-auth/internal/services/tenant/filterstrategies"
)

// Service implements domain.TenantService using secure tenant filtering.
type Service struct {
	tenantConfig   config.TenantFilterSettings
	logger         domain.Logger
	orQueryBuilder *filterstrategies.ORQueryBuilder
	metrics        domain.MetricsService
}

// NewService creates a new tenant service.
func NewService(
	tenantCfg *config.TenantFilterSettings,
	logger domain.Logger,
	metrics domain.MetricsService,
) domain.TenantService {
	return &Service{
		tenantConfig: *tenantCfg,
		logger:       logger.With(domain.Field{Key: "component", Value: "tenant"}),
		orQueryBuilder: filterstrategies.NewORQueryBuilder(
			logger.With(domain.Field{Key: "component", Value: "tenant.or_query"}),
		),
		metrics: metrics,
	}
}

// FilterQuery adds tenant filtering to PromQL query for VictoriaMetrics.
func (s *Service) FilterQuery(
	ctx context.Context,
	user *domain.User,
	query string,
) (string, error) {
	startTime := time.Now()

	// Support VM tenants
	if len(user.VMTenants) == 0 {
		return "", domain.ErrNoVMTenants
	}

	s.logger.Info("Tenant filtering",
		domain.Field{Key: "user_id", Value: user.ID},
		domain.Field{Key: "original_query", Value: query},
		domain.Field{Key: "vm_tenants", Value: fmt.Sprintf("%v", user.VMTenants)},
		domain.Field{Key: "vm_tenant_count", Value: len(user.VMTenants)},
		domain.Field{Key: "use_project_id", Value: s.tenantConfig.Labels.UseProjectID},
	)

	// Validate strategy configuration
	strategy := domain.TenantFilterStrategy(s.tenantConfig.Strategy)
	if !strategy.IsValid() {
		s.logger.Error("Invalid tenant filter strategy configured",
			domain.Field{Key: "configured_strategy", Value: s.tenantConfig.Strategy},
			domain.Field{Key: "valid_strategies", Value: string(domain.TenantFilterStrategyOrConditions)})
		return "", fmt.Errorf(
			"invalid tenant filter strategy: %s (only '%s' is supported)",
			s.tenantConfig.Strategy, domain.TenantFilterStrategyOrConditions,
		)
	}

	var filteredQuery string
	var err error

	filteredQuery, err = s.orQueryBuilder.BuildSecureQuery(query, user.VMTenants, &s.tenantConfig)
	duration := time.Since(startTime)

	if err != nil {
		// Record failed query filtering
		s.metrics.RecordQueryFilter(ctx, user.ID, len(user.VMTenants), false, duration)

		s.logger.Error("PromQL filtering failed",
			domain.Field{Key: "error", Value: err.Error()},
			domain.Field{Key: "original_query", Value: query})

		return "", domain.ErrPromQLParsing
	}

	filterApplied := query != filteredQuery

	// Record successful query filtering metrics
	s.metrics.RecordQueryFilter(ctx, user.ID, len(user.VMTenants), filterApplied, duration)

	s.logger.Info("Tenant filtering result",
		domain.Field{Key: "user_id", Value: user.ID},
		domain.Field{Key: "original_query", Value: query},
		domain.Field{Key: "filtered_query", Value: filteredQuery},
		domain.Field{Key: "filter_applied", Value: filterApplied},
		domain.Field{Key: "strategy", Value: string(domain.TenantFilterStrategyOrConditions)},
		domain.Field{Key: "duration_ms", Value: duration.Milliseconds()},
	)

	return filteredQuery, nil
}

// CanAccessTenant checks if user can access a specific tenant.
func (s *Service) CanAccessTenant(ctx context.Context, user *domain.User, tenantID string) bool {
	// Check VM tenants
	for _, vmTenant := range user.VMTenants {
		if vmTenant.AccountID == tenantID || vmTenant.String() == tenantID {
			// Record successful tenant access
			s.metrics.RecordTenantAccess(ctx, user.ID, tenantID, true)

			return true
		}
	}

	// Record denied tenant access
	s.metrics.RecordTenantAccess(ctx, user.ID, tenantID, false)

	return false
}

// DetermineTargetTenant determines which tenant to use for write operations.
func (s *Service) DetermineTargetTenant(
	ctx context.Context,
	user *domain.User,
	r *http.Request,
) (string, error) {
	// Support VM tenants
	if len(user.VMTenants) == 0 {
		return "", domain.ErrNoVMTenants
	}

	// Check for explicit tenant header (X-Prometheus-Tenant or standard headers)
	tenantHeaders := []string{
		"X-Prometheus-Tenant",
		"X-Tenant-ID",
	}

	for _, headerName := range tenantHeaders {
		if tenantHeader := r.Header.Get(headerName); tenantHeader != "" {
			if s.CanAccessTenant(ctx, user, tenantHeader) {
				s.logger.Debug("Using tenant from header",
					domain.Field{Key: "header", Value: headerName},
					domain.Field{Key: "tenant_length", Value: len(tenantHeader)},
				)

				return tenantHeader, nil
			}

			return "", &domain.AppError{
				Code:       domain.ErrCodeForbidden,
				Message:    "User cannot access tenant: " + tenantHeader,
				HTTPStatus: http.StatusForbidden,
			}
		}
	}

	// Use first VM tenant as default
	defaultTenant := user.VMTenants[0].String()
	s.logger.Debug("Using first VM tenant as default",
		domain.Field{Key: "tenant", Value: defaultTenant},
	)

	return defaultTenant, nil
}
