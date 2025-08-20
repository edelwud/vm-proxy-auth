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
	config      *Config
	client      *http.Client
	upstreamURL *url.URL
	logger      *logrus.Logger
}

func New(config *Config, logger *logrus.Logger) (*Proxy, error) {
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

	return &Proxy{
		config:      config,
		client:      client,
		upstreamURL: upstreamURL,
		logger:      logger,
	}, nil
}

type ProxyRequest struct {
	OriginalRequest *http.Request
	ModifiedQuery   string
	AllowedTenants  []string
	UserID          string
	ReadOnly        bool
}

func (p *Proxy) ForwardRequest(ctx context.Context, proxyReq *ProxyRequest) (*http.Response, error) {
	targetURL := *p.upstreamURL
	targetURL.Path = targetURL.Path + proxyReq.OriginalRequest.URL.Path
	targetURL.RawQuery = targetURL.RawQuery + proxyReq.OriginalRequest.URL.RawQuery

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

		if strings.Contains(proxyReq.OriginalRequest.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			bodyStr := string(bodyBytes)
			if proxyReq.ModifiedQuery != "" {
				bodyStr = modifyFormQuery(bodyStr, proxyReq.ModifiedQuery)
			}
			bodyReader = strings.NewReader(bodyStr)
		} else {
			bodyReader = bytes.NewReader(bodyBytes)
		}
	}

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
