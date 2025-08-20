package tenant

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// Service implements domain.TenantService using clean architecture with production-ready PromQL parsing.
type Service struct {
	config         config.UpstreamConfig
	logger         domain.Logger
	promqlInjector *PromQLTenantInjector
	metrics        domain.MetricsService
}

// NewService creates a new tenant service.
func NewService(
	cfg *config.UpstreamConfig,
	logger domain.Logger,
	metrics domain.MetricsService,
) domain.TenantService {
	return &Service{
		config:         *cfg,
		logger:         logger,
		promqlInjector: NewPromQLTenantInjector(logger),
		metrics:        metrics,
	}
}

// FilterQuery adds tenant filtering to PromQL query for VictoriaMetrics using production-ready Prometheus parser.
func (s *Service) FilterQuery(
	ctx context.Context,
	user *domain.User,
	query string,
) (string, error) {
	startTime := time.Now()

	// Only support VM tenants
	if len(user.VMTenants) == 0 {
		return "", domain.ErrNoVMTenants
	}

	s.logger.Info("TENANT FILTER DEBUG",
		domain.Field{Key: "user_id", Value: user.ID},
		domain.Field{Key: "original_query", Value: query},
		domain.Field{Key: "vm_tenants", Value: fmt.Sprintf("%v", user.VMTenants)},
		domain.Field{Key: "vm_tenant_count", Value: len(user.VMTenants)},
		domain.Field{Key: "use_project_id", Value: s.config.UseProjectID},
	)

	// Use production ready PromQL parser for VM tenants
	filteredQuery, err := s.promqlInjector.InjectTenantLabels(query, user.VMTenants, &s.config)
	duration := time.Since(startTime)

	if err != nil {
		// Record failed query filtering
		s.metrics.RecordQueryFilter(ctx, user.ID, len(user.VMTenants), false, duration)

		s.logger.Error("PromQL parsing failed",
			domain.Field{Key: "error", Value: err.Error()},
			domain.Field{Key: "original_query", Value: query})

		return "", domain.ErrPromQLParsing
	}

	filterApplied := query != filteredQuery

	// Record successful query filtering metrics
	s.metrics.RecordQueryFilter(ctx, user.ID, len(user.VMTenants), filterApplied, duration)

	s.logger.Info("TENANT FILTER RESULT",
		domain.Field{Key: "user_id", Value: user.ID},
		domain.Field{Key: "original_query", Value: query},
		domain.Field{Key: "filtered_query", Value: filteredQuery},
		domain.Field{Key: "filter_applied", Value: filterApplied},
		domain.Field{Key: "used_production_parser", Value: true},
		domain.Field{Key: "duration_ms", Value: duration.Milliseconds()},
	)

	return filteredQuery, nil
}

// CanAccessTenant checks if user can access a specific tenant.
func (s *Service) CanAccessTenant(ctx context.Context, user *domain.User, tenantID string) bool {
	// Only check VM tenants
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
	// Only support VM tenants
	if len(user.VMTenants) == 0 {
		return "", domain.ErrNoVMTenants
	}

	// Check for explicit tenant header (X-Prometheus-Tenant or custom header)
	tenantHeaders := []string{
		s.config.TenantHeader,
		"X-Prometheus-Tenant",
		"X-Tenant-ID",
	}

	for _, headerName := range tenantHeaders {
		if tenantHeader := r.Header.Get(headerName); tenantHeader != "" {
			if s.CanAccessTenant(ctx, user, tenantHeader) {
				s.logger.Debug("Using tenant from header",
					domain.Field{Key: "header", Value: headerName},
					domain.Field{Key: "tenant", Value: tenantHeader},
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
