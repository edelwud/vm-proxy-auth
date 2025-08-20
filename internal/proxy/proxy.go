package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/finlego/prometheus-oauth-gateway/pkg/tenant"
	"github.com/sirupsen/logrus"
)

type Config struct {
	UpstreamURL  string
	Timeout      time.Duration
	MaxRetries   int
	RetryDelay   time.Duration
	TenantHeader string
}

type Proxy struct {
	config         *Config
	client         *http.Client
	upstreamURL    *url.URL
	logger         *logrus.Logger
	writeProcessor *WriteProcessor
	tenantMapper   *tenant.Mapper
}

func New(config *Config, logger *logrus.Logger) (*Proxy, error) {
	return NewWithTenantMapper(config, logger, nil)
}

func NewWithTenantMapper(config *Config, logger *logrus.Logger, tenantMapper *tenant.Mapper) (*Proxy, error) {
	upstreamURL, err := url.Parse(config.UpstreamURL)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream URL: %w", err)
	}

	client := &http.Client{
		Timeout: config.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	writeProcessor := NewWriteProcessor(logger, "vm_account_id")

	return &Proxy{
		config:         config,
		client:         client,
		upstreamURL:    upstreamURL,
		logger:         logger,
		writeProcessor: writeProcessor,
		tenantMapper:   tenantMapper,
	}, nil
}

type ProxyRequest struct {
	OriginalRequest *http.Request
	ModifiedQuery   string
	AllowedTenants  []string
	UserID          string
	ReadOnly        bool
	TargetTenant    string // For write operations
	UserGroups      []string // For tenant filtering
}

func (p *Proxy) ForwardRequest(ctx context.Context, proxyReq *ProxyRequest) (*http.Response, error) {
	targetURL := *p.upstreamURL
	targetURL.Path = strings.TrimSuffix(targetURL.Path, "/") + proxyReq.OriginalRequest.URL.Path
	if targetURL.RawQuery != "" && proxyReq.OriginalRequest.URL.RawQuery != "" {
		targetURL.RawQuery = targetURL.RawQuery + "&" + proxyReq.OriginalRequest.URL.RawQuery
	} else if proxyReq.OriginalRequest.URL.RawQuery != "" {
		targetURL.RawQuery = proxyReq.OriginalRequest.URL.RawQuery
	}

	if proxyReq.ModifiedQuery != "" {
		queryValues := targetURL.Query()
		queryValues.Set("query", proxyReq.ModifiedQuery)
		targetURL.RawQuery = queryValues.Encode()
	}

	var bodyReader io.Reader
	if proxyReq.OriginalRequest.Body != nil {
		bodyBytes, err := io.ReadAll(proxyReq.OriginalRequest.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		proxyReq.OriginalRequest.Body.Close()

		// Handle different content types
		contentType := proxyReq.OriginalRequest.Header.Get("Content-Type")

		if strings.Contains(contentType, "application/x-www-form-urlencoded") {
			bodyStr := string(bodyBytes)
			
			// If we have a tenant mapper and user groups, try to filter the query from form data
			if p.tenantMapper != nil && len(proxyReq.UserGroups) > 0 {
				formValues, err := url.ParseQuery(bodyStr)
				if err == nil {
					if originalQuery := formValues.Get("query"); originalQuery != "" && proxyReq.ModifiedQuery == "" {
						// Apply tenant filtering if no modified query was provided
						if filteredQuery, err := p.tenantMapper.FilterTenantsInQuery(proxyReq.UserGroups, originalQuery); err == nil {
							p.logger.WithFields(logrus.Fields{
								"user_id": proxyReq.UserID,
								"original_query": originalQuery,
								"filtered_query": filteredQuery,
								"source": "post_form_data",
							}).Debug("Applied tenant filtering to POST query")
							bodyStr = modifyFormQuery(bodyStr, filteredQuery)
						} else {
							p.logger.WithError(err).WithFields(logrus.Fields{
								"user_id": proxyReq.UserID,
								"query": originalQuery,
							}).Warn("Failed to filter query from POST form data")
						}
					}
				}
			}
			
			if proxyReq.ModifiedQuery != "" {
				bodyStr = modifyFormQuery(bodyStr, proxyReq.ModifiedQuery)
			}
			bodyReader = strings.NewReader(bodyStr)
		} else if p.isWriteEndpoint(proxyReq.OriginalRequest.URL.Path) {
			// Handle write operations with tenant injection
			processedData, err := p.processWriteData(bodyBytes, proxyReq, contentType)
			if err != nil {
				return nil, fmt.Errorf("failed to process write data: %w", err)
			}
			bodyReader = bytes.NewReader(processedData)
		} else {
			bodyReader = bytes.NewReader(bodyBytes)
		}
	}

	p.logger.WithFields(logrus.Fields{
		"target_url":     targetURL.String(),
		"original_query": proxyReq.OriginalRequest.URL.RawQuery,
		"modified_query": proxyReq.ModifiedQuery != "",
		"method":         proxyReq.OriginalRequest.Method,
		"path":           proxyReq.OriginalRequest.URL.Path,
	}).Debug("Forwarding request to upstream")

	req, err := http.NewRequestWithContext(ctx, proxyReq.OriginalRequest.Method, targetURL.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create upstream request: %w", err)
	}

	copyHeaders(proxyReq.OriginalRequest.Header, req.Header)

	if len(proxyReq.AllowedTenants) > 0 && !contains(proxyReq.AllowedTenants, "*") {
		req.Header.Set(p.config.TenantHeader, strings.Join(proxyReq.AllowedTenants, ","))
	}

	req.Header.Set("X-Forwarded-User", proxyReq.UserID)
	req.Header.Set("X-User-Read-Only", fmt.Sprintf("%t", proxyReq.ReadOnly))

	var resp *http.Response
	var lastErr error

	for attempt := 0; attempt <= p.config.MaxRetries; attempt++ {
		if attempt > 0 {
			p.logger.WithFields(logrus.Fields{
				"attempt": attempt,
				"url":     targetURL.String(),
				"error":   lastErr,
			}).Warn("Retrying request to upstream")

			select {
			case <-time.After(p.config.RetryDelay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		resp, lastErr = p.client.Do(req)
		if lastErr == nil {
			break
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("failed to forward request after %d attempts: %w", p.config.MaxRetries+1, lastErr)
	}

	return resp, nil
}

func modifyFormQuery(body, newQuery string) string {
	values, err := url.ParseQuery(body)
	if err != nil {
		return body
	}

	values.Set("query", newQuery)
	return values.Encode()
}

func copyHeaders(src, dst http.Header) {
	for key, values := range src {
		if shouldCopyHeader(key) {
			for _, value := range values {
				dst.Add(key, value)
			}
		}
	}
}

func shouldCopyHeader(key string) bool {
	key = strings.ToLower(key)
	skipHeaders := []string{
		"connection",
		"proxy-connection",
		"proxy-authenticate",
		"proxy-authorization",
		"te",
		"trailers",
		"transfer-encoding",
		"upgrade",
	}

	return !contains(skipHeaders, key)
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// isWriteEndpoint checks if the endpoint is a write endpoint
func (p *Proxy) isWriteEndpoint(path string) bool {
	writeEndpoints := []string{
		"/api/v1/write",
		"/api/v1/import",
		"/insert/",
	}

	for _, endpoint := range writeEndpoints {
		if strings.HasPrefix(path, endpoint) {
			return true
		}
	}

	return false
}

// processWriteData handles tenant injection for write operations
func (p *Proxy) processWriteData(data []byte, proxyReq *ProxyRequest, contentType string) ([]byte, error) {
	// Determine target tenant for write operation
	targetTenant := proxyReq.TargetTenant

	// If no specific tenant is set, use the first allowed tenant
	// (assuming single-tenant write for simplicity)
	if targetTenant == "" && len(proxyReq.AllowedTenants) > 0 {
		if len(proxyReq.AllowedTenants) == 1 || contains(proxyReq.AllowedTenants, "*") {
			targetTenant = proxyReq.AllowedTenants[0]
		} else {
			// Multiple tenants - this should be handled at handler level
			p.logger.Warn("Write operation without specified tenant - using first allowed")
			targetTenant = proxyReq.AllowedTenants[0]
		}
	}

	// Detect data format
	format := p.writeProcessor.DetectWriteFormat(data, contentType)

	p.logger.WithFields(logrus.Fields{
		"user_id":       proxyReq.UserID,
		"target_tenant": targetTenant,
		"format":        format,
		"path":          proxyReq.OriginalRequest.URL.Path,
	}).Debug("Processing write data")

	// Process the data with tenant injection
	return p.writeProcessor.ProcessWriteData(data, targetTenant, format)
}
