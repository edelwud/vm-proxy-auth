package metrics

import (
	"context"
	"time"

	"github.com/finlego/prometheus-oauth-gateway/internal/domain"
)

// Service implements domain.MetricsService
type Service struct {
	logger domain.Logger
}

// NewService creates a new metrics service
func NewService(logger domain.Logger) *Service {
	return &Service{
		logger: logger,
	}
}

// RecordRequest records metrics for incoming requests
func (s *Service) RecordRequest(ctx context.Context, method, path, status string, duration time.Duration, user *domain.User) {
	// For now, just log the metrics. In the future, this could send to Prometheus, etc.
	fields := []domain.Field{
		{Key: "method", Value: method},
		{Key: "path", Value: path},
		{Key: "status", Value: status},
		{Key: "duration_ms", Value: duration.Milliseconds()},
	}

	if user != nil {
		fields = append(fields, domain.Field{Key: "user_id", Value: user.ID})
	}

	s.logger.Info("Request metrics", fields...)
}

// RecordUpstream records metrics for upstream requests
func (s *Service) RecordUpstream(ctx context.Context, method, path, status string, duration time.Duration, tenants []string) {
	fields := []domain.Field{
		{Key: "method", Value: method},
		{Key: "path", Value: path},
		{Key: "status", Value: status},
		{Key: "duration_ms", Value: duration.Milliseconds()},
		{Key: "tenants", Value: tenants},
	}

	s.logger.Info("Upstream metrics", fields...)
}

// RecordQueryFilter records metrics for query filtering operations
func (s *Service) RecordQueryFilter(ctx context.Context, userID string, tenants string) {
	fields := []domain.Field{
		{Key: "user_id", Value: userID},
		{Key: "tenants", Value: tenants},
	}

	s.logger.Info("Query filter metrics", fields...)
}