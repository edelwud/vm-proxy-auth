package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/health"
	"github.com/edelwud/vm-proxy-auth/internal/services/loadbalancer"
	"github.com/edelwud/vm-proxy-auth/internal/services/queue"
)

// EnhancedServiceConfig holds configuration for the enhanced proxy service.
type EnhancedServiceConfig struct {
	Backends       []BackendConfig      `yaml:"backends"`
	LoadBalancing  LoadBalancingConfig  `yaml:"load_balancing"`
	HealthCheck    health.CheckerConfig `yaml:"health_check"`
	Queue          QueueConfig          `yaml:"queue"`
	Timeout        time.Duration        `yaml:"timeout" default:"30s"`
	MaxRetries     int                  `yaml:"max_retries" default:"3"`
	RetryBackoff   time.Duration        `yaml:"retry_backoff" default:"100ms"`
	EnableQueueing bool                 `yaml:"enable_queueing" default:"false"`
}

// BackendConfig represents configuration for a single backend.
type BackendConfig struct {
	URL    string `yaml:"url"`
	Weight int    `yaml:"weight" default:"1"`
}

// LoadBalancingConfig holds load balancing configuration.
type LoadBalancingConfig struct {
	Strategy domain.LoadBalancingStrategy `yaml:"strategy" default:"round-robin"`
}

// QueueConfig holds request queue configuration.
type QueueConfig struct {
	MaxSize int           `yaml:"max_size" default:"1000"`
	Timeout time.Duration `yaml:"timeout" default:"5s"`
}

// EnhancedService provides enhanced proxy functionality with multiple upstream support.
type EnhancedService struct {
	config        EnhancedServiceConfig
	loadBalancer  domain.LoadBalancer
	healthChecker domain.HealthChecker
	requestQueue  domain.RequestQueue
	httpClient    *http.Client
	logger        domain.Logger
	metrics       domain.MetricsService
	mu            sync.RWMutex
	closed        bool
}

// NewEnhancedService creates a new enhanced proxy service with multiple upstream support.
func NewEnhancedService(
	config EnhancedServiceConfig,
	logger domain.Logger,
	metrics domain.MetricsService,
) (*EnhancedService, error) {
	// Validate configuration
	if len(config.Backends) == 0 {
		return nil, fmt.Errorf("at least one backend must be configured")
	}

	// Set defaults
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryBackoff == 0 {
		config.RetryBackoff = 100 * time.Millisecond
	}

	// Convert backend configs to domain backends
	backends := make([]domain.Backend, len(config.Backends))
	for i, backendConfig := range config.Backends {
		weight := backendConfig.Weight
		if weight <= 0 {
			weight = 1
		}
		backends[i] = domain.Backend{
			URL:    backendConfig.URL,
			Weight: weight,
			State:  domain.BackendHealthy, // Start as healthy
		}
	}

	service := &EnhancedService{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		logger:  logger.With(domain.Field{Key: "component", Value: "enhanced_proxy"}),
		metrics: metrics,
	}

	// Create load balancer using factory
	lbFactory := loadbalancer.NewFactory(logger)
	loadBalancer, err := lbFactory.CreateLoadBalancer(config.LoadBalancing.Strategy, backends)
	if err != nil {
		return nil, fmt.Errorf("failed to create load balancer: %w", err)
	}
	service.loadBalancer = loadBalancer

	// Create health checker
	healthChecker := health.NewChecker(
		config.HealthCheck,
		backends,
		service.onBackendStateChange,
		logger,
	)
	service.healthChecker = healthChecker

	// Create request queue if enabled
	if config.EnableQueueing {
		requestQueue := queue.NewMemoryQueue(
			config.Queue.MaxSize,
			config.Queue.Timeout,
			logger,
		)
		service.requestQueue = requestQueue
	}

	service.logger.Info("Enhanced proxy service created",
		domain.Field{Key: "backends", Value: len(backends)},
		domain.Field{Key: "strategy", Value: string(config.LoadBalancing.Strategy)},
		domain.Field{Key: "queueing_enabled", Value: config.EnableQueueing})

	return service, nil
}


// Start initializes and starts the enhanced proxy service.
func (es *EnhancedService) Start(ctx context.Context) error {
	es.mu.Lock()
	defer es.mu.Unlock()

	if es.closed {
		return fmt.Errorf("service is closed")
	}

	// Start health checker
	if err := es.healthChecker.StartMonitoring(ctx); err != nil {
		return fmt.Errorf("failed to start health checker: %w", err)
	}

	es.logger.Info("Enhanced proxy service started")
	return nil
}

// Forward forwards a request to an upstream backend using load balancing.
func (es *EnhancedService) Forward(ctx context.Context, req *domain.ProxyRequest) (*domain.ProxyResponse, error) {
	start := time.Now()

	// Check if service is closed
	es.mu.RLock()
	if es.closed {
		es.mu.RUnlock()
		return nil, fmt.Errorf("service is closed")
	}
	es.mu.RUnlock()

	// Queue request if queueing is enabled and we're under heavy load
	if es.requestQueue != nil {
		// For now, we'll process directly, but queue is available for future enhancement
		// In a full implementation, you might queue during backend failures or high load
	}

	// Try to forward request with retries
	var lastErr error
	for attempt := 0; attempt < es.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Linear backoff (not exponential to avoid excessive delays)
			backoff := time.Duration(attempt) * es.config.RetryBackoff
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}

			es.logger.Debug("Retrying request",
				domain.Field{Key: "attempt", Value: attempt + 1},
				domain.Field{Key: "user_id", Value: req.User.ID},
				domain.Field{Key: "backoff", Value: backoff})
		}

		response, err := es.forwardToBackend(ctx, req)
		if err == nil {
			// Record successful request metrics
			es.metrics.RecordLoadBalancerSelection(
				ctx,
				es.config.LoadBalancing.Strategy,
				"", // backend URL will be recorded in forwardToBackend
				time.Since(start),
			)
			return response, nil
		}

		lastErr = err
		es.logger.Debug("Request attempt failed",
			domain.Field{Key: "attempt", Value: attempt + 1},
			domain.Field{Key: "user_id", Value: req.User.ID},
			domain.Field{Key: "error", Value: err.Error()})
	}

	// All attempts failed
	es.logger.Error("All request attempts failed",
		domain.Field{Key: "user_id", Value: req.User.ID},
		domain.Field{Key: "attempts", Value: es.config.MaxRetries},
		domain.Field{Key: "total_duration", Value: time.Since(start)},
		domain.Field{Key: "final_error", Value: lastErr.Error()})

	return nil, fmt.Errorf("request failed after %d attempts: %w", es.config.MaxRetries, lastErr)
}

// forwardToBackend forwards a request to a specific backend selected by the load balancer.
func (es *EnhancedService) forwardToBackend(ctx context.Context, req *domain.ProxyRequest) (*domain.ProxyResponse, error) {
	// Select backend using load balancer
	backend, err := es.loadBalancer.NextBackend(ctx)
	if err != nil {
		es.logger.Warn("Failed to select backend",
			domain.Field{Key: "user_id", Value: req.User.ID},
			domain.Field{Key: "error", Value: err.Error()})
		return nil, fmt.Errorf("no available backend: %w", err)
	}

	start := time.Now()
	es.logger.Debug("Forwarding request to backend",
		domain.Field{Key: "backend_url", Value: backend.URL},
		domain.Field{Key: "user_id", Value: req.User.ID},
		domain.Field{Key: "method", Value: req.OriginalRequest.Method},
		domain.Field{Key: "path", Value: req.OriginalRequest.URL.Path})

	// Build target URL
	targetURL, err := url.Parse(backend.URL)
	if err != nil {
		es.loadBalancer.ReportResult(backend, err, 0)
		return nil, fmt.Errorf("invalid backend URL %s: %w", backend.URL, err)
	}

	// Create new request for backend
	targetURL.Path = req.OriginalRequest.URL.Path
	targetURL.RawQuery = req.OriginalRequest.URL.RawQuery

	// Override query if we have a filtered version
	if req.FilteredQuery != "" {
		// For PromQL queries, replace the query parameter
		if req.OriginalRequest.URL.Query().Get("query") != "" {
			values := targetURL.Query()
			values.Set("query", req.FilteredQuery)
			targetURL.RawQuery = values.Encode()
		}
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, req.OriginalRequest.Method, targetURL.String(), req.OriginalRequest.Body)
	if err != nil {
		es.loadBalancer.ReportResult(backend, err, 0)
		return nil, fmt.Errorf("failed to create backend request: %w", err)
	}

	// Copy headers
	for name, values := range req.OriginalRequest.Header {
		for _, value := range values {
			httpReq.Header.Add(name, value)
		}
	}

	// Add tenant header if specified
	if req.TargetTenant != "" {
		httpReq.Header.Set("X-Prometheus-Tenant", req.TargetTenant)
	}

	// Execute request
	resp, err := es.httpClient.Do(httpReq)
	duration := time.Since(start)

	if err != nil {
		es.loadBalancer.ReportResult(backend, err, 0)

		// Record metrics for failed request
		es.metrics.RecordUpstreamBackend(
			ctx,
			backend.URL,
			req.OriginalRequest.Method,
			req.OriginalRequest.URL.Path,
			"error",
			duration,
			[]string{req.TargetTenant},
		)

		return nil, fmt.Errorf("backend request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		es.loadBalancer.ReportResult(backend, err, resp.StatusCode)
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check for HTTP error status codes (4xx, 5xx)
	var reportErr error
	if resp.StatusCode >= 500 {
		// 5xx errors should trigger retries
		reportErr = fmt.Errorf("upstream returned server error status: %d", resp.StatusCode)
	} else if resp.StatusCode >= 400 {
		// 4xx errors are client errors and should not trigger retries in load balancer
		// but we still report them for monitoring
		reportErr = fmt.Errorf("upstream returned client error status: %d", resp.StatusCode)
	}

	// Report result to load balancer (only 5xx errors for retry logic)
	var lbErr error
	if resp.StatusCode >= 500 {
		lbErr = reportErr
	}
	es.loadBalancer.ReportResult(backend, lbErr, resp.StatusCode)

	// Record metrics
	statusStr := fmt.Sprintf("%d", resp.StatusCode)
	es.metrics.RecordUpstreamBackend(
		ctx,
		backend.URL,
		req.OriginalRequest.Method,
		req.OriginalRequest.URL.Path,
		statusStr,
		duration,
		[]string{req.TargetTenant},
	)

	// Return error for 5xx status codes to trigger retries
	if resp.StatusCode >= 500 {
		return nil, reportErr
	}

	es.logger.Debug("Request forwarded successfully",
		domain.Field{Key: "backend_url", Value: backend.URL},
		domain.Field{Key: "user_id", Value: req.User.ID},
		domain.Field{Key: "status_code", Value: resp.StatusCode},
		domain.Field{Key: "duration", Value: duration})

	return &domain.ProxyResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       body,
	}, nil
}

// onBackendStateChange handles backend state changes from the health checker.
func (es *EnhancedService) onBackendStateChange(backendURL string, oldState, newState domain.BackendState) {
	es.logger.Info("Backend state changed via health check",
		domain.Field{Key: "backend_url", Value: backendURL},
		domain.Field{Key: "old_state", Value: oldState.String()},
		domain.Field{Key: "new_state", Value: newState.String()})

	// Record metrics for state change
	es.metrics.RecordBackendStateChange(context.Background(), backendURL, oldState, newState)

	// Update load balancer backend state
	if lb, ok := es.loadBalancer.(interface {
		UpdateBackendState(backendURL string, newState domain.BackendState)
	}); ok {
		lb.UpdateBackendState(backendURL, newState)
	}
}

// GetBackendsStatus returns the current status of all backends for monitoring.
func (es *EnhancedService) GetBackendsStatus() []*domain.BackendStatus {
	return es.loadBalancer.BackendsStatus()
}

// SetMaintenanceMode enables or disables maintenance mode for a specific backend.
func (es *EnhancedService) SetMaintenanceMode(backend string, enabled bool) error {
	es.mu.Lock()
	defer es.mu.Unlock()

	var newState domain.BackendState
	if enabled {
		newState = domain.BackendMaintenance
	} else {
		newState = domain.BackendHealthy
	}

	// Update load balancer
	if lb, ok := es.loadBalancer.(interface {
		UpdateBackendState(backendURL string, newState domain.BackendState)
	}); ok {
		lb.UpdateBackendState(backend, newState)
	}

	es.logger.Info("Backend maintenance mode changed",
		domain.Field{Key: "backend_url", Value: backend},
		domain.Field{Key: "maintenance_enabled", Value: enabled})

	return nil
}

// GetHealthStats returns health check statistics for all backends.
func (es *EnhancedService) GetHealthStats() map[string]health.BackendHealthStats {
	if hc, ok := es.healthChecker.(*health.Checker); ok {
		return hc.GetStats()
	}
	return nil
}

// GetQueueStats returns queue statistics if queueing is enabled.
func (es *EnhancedService) GetQueueStats() *queue.QueueStats {
	if es.requestQueue != nil {
		if mq, ok := es.requestQueue.(*queue.MemoryQueue); ok {
			stats := mq.Stats()
			return &stats
		}
	}
	return nil
}

// Close performs cleanup and graceful shutdown.
func (es *EnhancedService) Close() error {
	es.mu.Lock()
	defer es.mu.Unlock()

	if es.closed {
		return nil // Already closed
	}

	es.closed = true

	// Stop health checker
	if es.healthChecker != nil {
		if err := es.healthChecker.Stop(); err != nil {
			es.logger.Warn("Error stopping health checker",
				domain.Field{Key: "error", Value: err.Error()})
		}
	}

	// Close load balancer
	if es.loadBalancer != nil {
		if err := es.loadBalancer.Close(); err != nil {
			es.logger.Warn("Error closing load balancer",
				domain.Field{Key: "error", Value: err.Error()})
		}
	}

	// Close request queue
	if es.requestQueue != nil {
		if err := es.requestQueue.Close(); err != nil {
			es.logger.Warn("Error closing request queue",
				domain.Field{Key: "error", Value: err.Error()})
		}
	}

	es.logger.Info("Enhanced proxy service closed")
	return nil
}
