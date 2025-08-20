package proxy

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// Service implements domain.ProxyService
type Service struct {
	upstreamURL string
	client      *http.Client
	logger      domain.Logger
	metrics     domain.MetricsService
}

// NewService creates a new proxy service
func NewService(upstreamURL string, timeout time.Duration, logger domain.Logger, metrics domain.MetricsService) *Service {
	return &Service{
		upstreamURL: upstreamURL,
		client: &http.Client{
			Timeout: timeout,
		},
		logger:  logger,
		metrics: metrics,
	}
}

// Forward forwards a request to the upstream server
func (s *Service) Forward(ctx context.Context, req *domain.ProxyRequest) (*domain.ProxyResponse, error) {
	startTime := time.Now()
	// Prepare query parameters and body for filtering
	queryParams := req.OriginalRequest.URL.Query()
	var body io.Reader
	
	// Apply filtered query if present
	if req.FilteredQuery != "" {
		s.logger.Debug("Applying filtered query",
			domain.Field{Key: "original_query", Value: queryParams.Get("query")},
			domain.Field{Key: "filtered_query", Value: req.FilteredQuery},
			domain.Field{Key: "user_id", Value: req.User.ID})
		
		// Replace query parameter with filtered version
		queryParams.Set("query", req.FilteredQuery)
	}
	
	// Handle request body (for POST requests with form data)
	if req.OriginalRequest.Body != nil {
		bodyBytes, err := io.ReadAll(req.OriginalRequest.Body)
		if err != nil {
			return nil, &domain.AppError{
				Code:       "request_read_error",
				Message:    "Failed to read request body",
				HTTPStatus: http.StatusInternalServerError,
			}
		}
		
		bodyStr := string(bodyBytes)
		
		// If we have a filtered query and this is a POST with form data, update the body
		if req.FilteredQuery != "" && req.OriginalRequest.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
			// Parse form data from body
			bodyValues, parseErr := url.ParseQuery(bodyStr)
			if parseErr == nil && bodyValues.Get("query") != "" {
				s.logger.Debug("Replacing query in POST body",
					domain.Field{Key: "original_body_query", Value: bodyValues.Get("query")},
					domain.Field{Key: "filtered_query", Value: req.FilteredQuery})
				
				// Replace the query in form data
				bodyValues.Set("query", req.FilteredQuery)
				bodyStr = bodyValues.Encode()
			}
		}
		
		body = strings.NewReader(bodyStr)
	}

	// Build target URL with potentially modified query parameters
	targetURL, err := s.buildTargetURL(req.OriginalRequest.URL.Path, queryParams)
	if err != nil {
		return nil, &domain.AppError{
			Code:       "proxy_url_error",
			Message:    "Failed to build target URL",
			HTTPStatus: http.StatusInternalServerError,
		}
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, req.OriginalRequest.Method, targetURL, body)
	if err != nil {
		return nil, &domain.AppError{
			Code:       "proxy_request_error",
			Message:    "Failed to create HTTP request",
			HTTPStatus: http.StatusInternalServerError,
		}
	}

	// Copy headers from original request
	for key, values := range req.OriginalRequest.Header {
		for _, value := range values {
			httpReq.Header.Add(key, value)
		}
	}
	
	// Add tenant headers for write operations
	if req.TargetTenant != "" {
		httpReq.Header.Set("X-Prometheus-Tenant", req.TargetTenant)
		s.logger.Debug("Added tenant header for write operation",
			domain.Field{Key: "target_tenant", Value: req.TargetTenant},
			domain.Field{Key: "user_id", Value: req.User.ID})
	}

	// Execute request
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, &domain.AppError{
			Code:       "upstream_error",
			Message:    "Upstream server unavailable",
			HTTPStatus: http.StatusBadGateway,
		}
	}
	defer resp.Body.Close()

	// Read response body
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &domain.AppError{
			Code:       "response_read_error",
			Message:    "Failed to read upstream response",
			HTTPStatus: http.StatusInternalServerError,
		}
	}

	// Build response
	proxyResponse := &domain.ProxyResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header.Clone(),
		Body:       responseBody,
	}

	// Record upstream metrics
	duration := time.Since(startTime)
	tenants := make([]string, len(req.User.VMTenants))
	for i, tenant := range req.User.VMTenants {
		tenants[i] = tenant.String()
	}
	
	s.metrics.RecordUpstream(
		ctx,
		req.OriginalRequest.Method,
		req.OriginalRequest.URL.Path,
		http.StatusText(resp.StatusCode),
		duration,
		tenants,
	)

	s.logger.Debug("Upstream request completed",
		domain.Field{Key: "method", Value: req.OriginalRequest.Method},
		domain.Field{Key: "path", Value: req.OriginalRequest.URL.Path},
		domain.Field{Key: "status_code", Value: resp.StatusCode},
		domain.Field{Key: "duration_ms", Value: duration.Milliseconds()},
		domain.Field{Key: "user_id", Value: req.User.ID},
	)

	return proxyResponse, nil
}

func (s *Service) buildTargetURL(path string, query url.Values) (string, error) {
	targetURL, err := url.Parse(s.upstreamURL)
	if err != nil {
		return "", err
	}

	// Properly concatenate paths
	if !strings.HasSuffix(targetURL.Path, "/") && !strings.HasPrefix(path, "/") {
		targetURL.Path = targetURL.Path + "/" + path
	} else if strings.HasSuffix(targetURL.Path, "/") && strings.HasPrefix(path, "/") {
		targetURL.Path = targetURL.Path + path[1:]
	} else {
		targetURL.Path = targetURL.Path + path
	}

	// Properly handle query parameters
	if targetURL.RawQuery != "" && query.Encode() != "" {
		targetURL.RawQuery = targetURL.RawQuery + "&" + query.Encode()
	} else if query.Encode() != "" {
		targetURL.RawQuery = query.Encode()
	}

	s.logger.Debug("Built target URL",
		domain.Field{Key: "original_path", Value: path},
		domain.Field{Key: "target_url", Value: targetURL.String()},
	)

	return targetURL.String(), nil
}
