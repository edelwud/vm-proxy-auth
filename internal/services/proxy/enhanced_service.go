package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/health"
	"github.com/edelwud/vm-proxy-auth/internal/services/loadbalancer"
	"github.com/edelwud/vm-proxy-auth/internal/services/queue"
)

// Default configuration constants.
const (
	defaultTimeoutSeconds = 30
	defaultMaxRetries     = 3
	defaultRetryBackoffMs = 100
	queueCheckInterval    = 100 * time.Millisecond
	// HTTP status code constants.
	statusInternalServerError = 500
	statusBadRequest          = 400
)

// EnhancedServiceConfig holds configuration for the enhanced proxy service.
type EnhancedServiceConfig struct {
	Backends       []BackendConfig      `yaml:"backends"`
	LoadBalancing  LoadBalancingConfig  `yaml:"load_balancing"`
	HealthCheck    health.CheckerConfig `yaml:"health_check"`
	Queue          QueueConfig          `yaml:"queue"`
	Timeout        time.Duration        `yaml:"timeout"         default:"30s"`
	MaxRetries     int                  `yaml:"max_retries"     default:"3"`
	RetryBackoff   time.Duration        `yaml:"retry_backoff"   default:"100ms"`
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
	Timeout time.Duration `yaml:"timeout"  default:"5s"`
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
	stateStorage domain.StateStorage,
) (*EnhancedService, error) {
	// Validate configuration
	if len(config.Backends) == 0 {
		return nil, errors.New("at least one backend must be configured")
	}

	// Set defaults
	if config.Timeout == 0 {
		config.Timeout = defaultTimeoutSeconds * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = defaultMaxRetries
	}
	if config.RetryBackoff == 0 {
		config.RetryBackoff = defaultRetryBackoffMs * time.Millisecond
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
		stateStorage,
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
		return errors.New("service is closed")
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
	// Check if service is closed
	es.mu.RLock()
	if es.closed {
		es.mu.RUnlock()
		return nil, errors.New("service is closed")
	}
	es.mu.RUnlock()

	// Use queue processing if queueing is enabled
	if es.config.EnableQueueing && es.requestQueue != nil {
		return es.forwardWithQueue(ctx, req)
	}

	// Direct processing without queue (backward compatibility)
	return es.forwardToBackendDirect(ctx, req)
}

// forwardWithQueue handles request processing using the queue for resilience.
func (es *EnhancedService) forwardWithQueue(
	ctx context.Context,
	req *domain.ProxyRequest,
) (*domain.ProxyResponse, error) {
	// Check if any backends are available
	backends := es.loadBalancer.BackendsStatus()
	availableBackends := 0
	for _, backend := range backends {
		if backend.Backend.State == domain.BackendHealthy {
			availableBackends++
		}
	}

	// If no healthy backends are available, queue the request
	if availableBackends == 0 {
		es.logger.Info("No healthy backends available, queueing request",
			domain.Field{Key: "user_id", Value: req.User.ID},
			domain.Field{Key: "queue_size", Value: es.requestQueue.Size()})

		enqueueStart := time.Now()
		if err := es.requestQueue.Enqueue(ctx, req); err != nil {
			es.metrics.RecordQueueOperation(ctx, "enqueue_failed", time.Since(enqueueStart), es.requestQueue.Size())
			return nil, fmt.Errorf("failed to queue request: %w", err)
		}
		es.metrics.RecordQueueOperation(ctx, "enqueue_success", time.Since(enqueueStart), es.requestQueue.Size())

		// Process the queue to handle this and any pending requests
		return es.processQueuedRequest(ctx, req)
	}

	// If backends are available, try direct processing first
	response, err := es.forwardToBackendDirect(ctx, req)
	if err == nil {
		return response, nil
	}

	// If direct processing fails and queue has space, queue the request for retry
	if es.requestQueue.Size() < es.config.Queue.MaxSize {
		es.logger.Debug("Direct processing failed, queueing for retry",
			domain.Field{Key: "user_id", Value: req.User.ID},
			domain.Field{Key: "error", Value: err.Error()})

		enqueueStart := time.Now()
		if queueErr := es.requestQueue.Enqueue(ctx, req); queueErr != nil {
			es.metrics.RecordQueueOperation(ctx, "enqueue_failed", time.Since(enqueueStart), es.requestQueue.Size())
			return nil, fmt.Errorf("direct processing failed and queue full: %w", err)
		}
		es.metrics.RecordQueueOperation(ctx, "enqueue_retry", time.Since(enqueueStart), es.requestQueue.Size())

		return es.processQueuedRequest(ctx, req)
	}

	// Queue is full, return the original error
	return nil, err
}

// processQueuedRequest processes a request from the queue with backend availability checking.
func (es *EnhancedService) processQueuedRequest(
	ctx context.Context,
	originalReq *domain.ProxyRequest,
) (*domain.ProxyResponse, error) {
	maxWaitTime := es.config.Queue.Timeout
	startTime := time.Now()

	for time.Since(startTime) < maxWaitTime {
		if es.hasHealthyBackends() {
			return es.processFromQueue(ctx, originalReq, startTime, maxWaitTime)
		}

		// Wait before checking again
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(queueCheckInterval):
			// Continue checking
		}
	}

	return nil, fmt.Errorf("request timed out in queue after %v", maxWaitTime)
}

// hasHealthyBackends checks if any backends are currently healthy.
func (es *EnhancedService) hasHealthyBackends() bool {
	backends := es.loadBalancer.BackendsStatus()
	for _, backend := range backends {
		if backend.Backend.State == domain.BackendHealthy {
			return true
		}
	}
	return false
}

// processFromQueue attempts to dequeue and process a request when backends are available.
func (es *EnhancedService) processFromQueue(
	ctx context.Context,
	originalReq *domain.ProxyRequest,
	startTime time.Time,
	maxWaitTime time.Duration,
) (*domain.ProxyResponse, error) {
	dequeueStart := time.Now()
	queuedReq, err := es.requestQueue.Dequeue(ctx)
	if err != nil {
		return es.handleDequeueError(ctx, originalReq, err, dequeueStart)
	}

	es.metrics.RecordQueueOperation(ctx, "dequeue_success", time.Since(dequeueStart), es.requestQueue.Size())

	// Process the dequeued request
	response, err := es.forwardToBackendDirect(ctx, queuedReq)
	if err == nil {
		es.metrics.RecordQueueOperation(ctx, "process_success", time.Since(dequeueStart), es.requestQueue.Size())
		return response, nil
	}

	// If processing failed, re-queue if there's space and time
	es.handleProcessingFailure(ctx, queuedReq, startTime, maxWaitTime)
	return nil, err
}

// handleDequeueError handles errors from queue dequeue operations.
func (es *EnhancedService) handleDequeueError(
	ctx context.Context,
	originalReq *domain.ProxyRequest,
	err error,
	dequeueStart time.Time,
) (*domain.ProxyResponse, error) {
	if errors.Is(err, domain.ErrQueueEmpty) {
		es.metrics.RecordQueueOperation(ctx, "dequeue_empty", time.Since(dequeueStart), es.requestQueue.Size())
		return es.forwardToBackendDirect(ctx, originalReq)
	}

	es.metrics.RecordQueueOperation(ctx, "dequeue_failed", time.Since(dequeueStart), es.requestQueue.Size())
	return nil, fmt.Errorf("failed to dequeue request: %w", err)
}

// handleProcessingFailure handles re-queueing after processing failures.
func (es *EnhancedService) handleProcessingFailure(
	ctx context.Context,
	queuedReq *domain.ProxyRequest,
	startTime time.Time,
	maxWaitTime time.Duration,
) {
	if es.requestQueue.Size() >= es.config.Queue.MaxSize || time.Since(startTime) >= maxWaitTime {
		return // Cannot re-queue
	}

	requeueStart := time.Now()
	if requeueErr := es.requestQueue.Enqueue(ctx, queuedReq); requeueErr != nil {
		es.metrics.RecordQueueOperation(ctx, "requeue_failed", time.Since(requeueStart), es.requestQueue.Size())
		es.logger.Warn("Failed to re-queue request after processing failure",
			domain.Field{Key: "user_id", Value: queuedReq.User.ID},
			domain.Field{Key: "error", Value: requeueErr.Error()})
	} else {
		es.metrics.RecordQueueOperation(ctx, "requeue_success", time.Since(requeueStart), es.requestQueue.Size())
	}
}

// forwardToBackendDirect performs direct backend forwarding without queue processing.
func (es *EnhancedService) forwardToBackendDirect(
	ctx context.Context,
	req *domain.ProxyRequest,
) (*domain.ProxyResponse, error) {
	start := time.Now()

	var lastErr error
	for attempt := range es.config.MaxRetries {
		if attempt > 0 {
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
			es.logger.Debug("Request processed successfully",
				domain.Field{Key: "user_id", Value: req.User.ID},
				domain.Field{Key: "total_duration", Value: time.Since(start)})
			return response, nil
		}

		lastErr = err
		es.logger.Debug("Request attempt failed",
			domain.Field{Key: "attempt", Value: attempt + 1},
			domain.Field{Key: "user_id", Value: req.User.ID},
			domain.Field{Key: "error", Value: err.Error()})
	}

	es.logger.Error("All request attempts failed",
		domain.Field{Key: "user_id", Value: req.User.ID},
		domain.Field{Key: "attempts", Value: es.config.MaxRetries},
		domain.Field{Key: "total_duration", Value: time.Since(start)},
		domain.Field{Key: "final_error", Value: lastErr.Error()})

	return nil, fmt.Errorf("request failed after %d attempts: %w", es.config.MaxRetries, lastErr)
}

// forwardToBackend forwards a request to a specific backend selected by the load balancer.
func (es *EnhancedService) forwardToBackend(
	ctx context.Context,
	req *domain.ProxyRequest,
) (*domain.ProxyResponse, error) {
	start := time.Now()

	// Select backend using load balancer
	backend, err := es.selectBackend(ctx, req)
	if err != nil {
		return nil, err
	}

	// Create HTTP request for backend
	httpReq, reqStart, err := es.createBackendRequest(ctx, req, backend)
	if err != nil {
		return nil, err
	}

	// Execute request and process response
	response, err := es.executeBackendRequest(ctx, req, backend, httpReq, reqStart)
	if err == nil {
		// Record successful load balancer selection
		es.metrics.RecordLoadBalancerSelection(
			ctx,
			es.config.LoadBalancing.Strategy,
			backend.URL,
			time.Since(start),
		)
	}
	return response, err
}

// Helper functions for forwardToBackend to reduce complexity.

func (es *EnhancedService) selectBackend(
	ctx context.Context,
	req *domain.ProxyRequest,
) (*domain.Backend, error) {
	backend, err := es.loadBalancer.NextBackend(ctx)
	if err != nil {
		es.logger.Warn("Failed to select backend",
			domain.Field{Key: "user_id", Value: req.User.ID},
			domain.Field{Key: "error", Value: err.Error()})
		return nil, fmt.Errorf("no available backend: %w", err)
	}
	return backend, nil
}

func (es *EnhancedService) createBackendRequest(
	ctx context.Context,
	req *domain.ProxyRequest,
	backend *domain.Backend,
) (*http.Request, time.Time, error) {
	start := time.Now()
	es.logger.Debug("Forwarding request to backend",
		domain.Field{Key: "backend_url", Value: backend.URL},
		domain.Field{Key: "user_id", Value: req.User.ID},
		domain.Field{Key: "method", Value: req.OriginalRequest.Method},
		domain.Field{Key: "path", Value: req.OriginalRequest.URL.Path})

	// Build target URL
	targetURL, err := es.buildTargetURL(req, backend)
	if err != nil {
		es.loadBalancer.ReportResult(backend, err, 0)
		return nil, start, err
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(
		ctx,
		req.OriginalRequest.Method,
		targetURL.String(),
		req.OriginalRequest.Body,
	)
	if err != nil {
		es.loadBalancer.ReportResult(backend, err, 0)
		return nil, start, fmt.Errorf("failed to create backend request: %w", err)
	}

	// Copy headers and add tenant info
	es.prepareRequestHeaders(httpReq, req)

	return httpReq, start, nil
}

func (es *EnhancedService) buildTargetURL(
	req *domain.ProxyRequest,
	backend *domain.Backend,
) (*url.URL, error) {
	targetURL, err := url.Parse(backend.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid backend URL %s: %w", backend.URL, err)
	}

	targetURL.Path = req.OriginalRequest.URL.Path
	targetURL.RawQuery = req.OriginalRequest.URL.RawQuery

	// Override query if we have a filtered version
	if req.FilteredQuery != "" && req.OriginalRequest.URL.Query().Get("query") != "" {
		values := targetURL.Query()
		values.Set("query", req.FilteredQuery)
		targetURL.RawQuery = values.Encode()
	}

	return targetURL, nil
}

func (es *EnhancedService) prepareRequestHeaders(httpReq *http.Request, req *domain.ProxyRequest) {
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
}

func (es *EnhancedService) executeBackendRequest(
	ctx context.Context,
	req *domain.ProxyRequest,
	backend *domain.Backend,
	httpReq *http.Request,
	start time.Time,
) (*domain.ProxyResponse, error) {
	// Execute request
	resp, err := es.httpClient.Do(httpReq)
	duration := time.Since(start)

	if err != nil {
		return nil, es.handleRequestError(ctx, req, backend, err, duration)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		es.loadBalancer.ReportResult(backend, err, resp.StatusCode)
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return es.processBackendResponse(ctx, req, backend, resp, body, duration)
}

func (es *EnhancedService) handleRequestError(
	ctx context.Context,
	req *domain.ProxyRequest,
	backend *domain.Backend,
	err error,
	duration time.Duration,
) error {
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

	return fmt.Errorf("backend request failed: %w", err)
}

func (es *EnhancedService) processBackendResponse(
	ctx context.Context,
	req *domain.ProxyRequest,
	backend *domain.Backend,
	resp *http.Response,
	body []byte,
	duration time.Duration,
) (*domain.ProxyResponse, error) {
	// Check for HTTP error status codes (4xx, 5xx)
	var reportErr error
	if resp.StatusCode >= statusInternalServerError {
		reportErr = fmt.Errorf("upstream returned server error status: %d", resp.StatusCode)
	} else if resp.StatusCode >= statusBadRequest {
		reportErr = fmt.Errorf("upstream returned client error status: %d", resp.StatusCode)
	}

	// Report result to load balancer (only 5xx errors for retry logic)
	var lbErr error
	if resp.StatusCode >= statusInternalServerError {
		lbErr = reportErr
	}
	es.loadBalancer.ReportResult(backend, lbErr, resp.StatusCode)

	// Record metrics
	statusStr := strconv.Itoa(resp.StatusCode)
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
	if resp.StatusCode >= statusInternalServerError {
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
func (es *EnhancedService) onBackendStateChange(
	backendURL string,
	oldState, newState domain.BackendState,
) {
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
func (es *EnhancedService) GetQueueStats() *queue.Stats {
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
