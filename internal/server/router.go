package server

import (
	"strings"

	"github.com/finlego/prometheus-oauth-gateway/internal/middleware"
	"github.com/finlego/prometheus-oauth-gateway/pkg/tenant"
)

// EndpointType represents different types of VictoriaMetrics endpoints
type EndpointType int

const (
	// Read endpoints - require tenant filtering
	EndpointTypeQuery EndpointType = iota
	EndpointTypeSeries
	EndpointTypeLabels
	EndpointTypeMetadata

	// Write endpoints - require tenant injection
	EndpointTypeWrite
	EndpointTypeImport

	// Admin endpoints - require admin permissions
	EndpointTypeAdmin
	EndpointTypeDelete

	// Passthrough endpoints - no special handling
	EndpointTypePassthrough
)

// EndpointConfig defines how to handle different endpoints
type EndpointConfig struct {
	Type              EndpointType
	RequiresTenantFilter bool
	RequiresWriteAccess  bool
	RequiresAdminAccess  bool
	AllowedMethods       []string
}

// VictoriaMetrics API endpoints mapping
var vmEndpoints = map[string]EndpointConfig{
	// Prometheus-compatible query endpoints
	"/api/v1/query":                    {EndpointTypeQuery, true, false, false, []string{"GET", "POST"}},
	"/api/v1/query_range":              {EndpointTypeQuery, true, false, false, []string{"GET", "POST"}},
	"/api/v1/query_exemplars":          {EndpointTypeQuery, true, false, false, []string{"GET", "POST"}},
	
	// Series and labels endpoints  
	"/api/v1/series":                   {EndpointTypeSeries, true, false, false, []string{"GET", "POST", "DELETE"}},
	"/api/v1/labels":                   {EndpointTypeLabels, true, false, false, []string{"GET"}},
	"/api/v1/label/*/values":           {EndpointTypeLabels, true, false, false, []string{"GET"}},
	"/api/v1/metadata":                 {EndpointTypeMetadata, true, false, false, []string{"GET"}},
	"/api/v1/targets":                  {EndpointTypeMetadata, false, false, false, []string{"GET"}},
	"/api/v1/targets/metadata":         {EndpointTypeMetadata, false, false, false, []string{"GET"}},

	// Write endpoints
	"/api/v1/write":                    {EndpointTypeWrite, false, true, false, []string{"POST"}},
	"/api/v1/import":                   {EndpointTypeImport, false, true, false, []string{"POST"}},
	"/api/v1/import/csv":               {EndpointTypeImport, false, true, false, []string{"POST"}},
	"/api/v1/import/prometheus":        {EndpointTypeImport, false, true, false, []string{"POST"}},
	"/api/v1/import/native":            {EndpointTypeImport, false, true, false, []string{"POST"}},

	// VictoriaMetrics-specific endpoints
	"/select/*/vmui":                   {EndpointTypePassthrough, false, false, false, []string{"GET"}},
	"/select/*/vmui/*":                 {EndpointTypePassthrough, false, false, false, []string{"GET"}},
	"/select/*/prometheus/api/v1/query": {EndpointTypeQuery, true, false, false, []string{"GET", "POST"}},
	"/select/*/prometheus/api/v1/query_range": {EndpointTypeQuery, true, false, false, []string{"GET", "POST"}},
	"/select/*/prometheus/api/v1/series": {EndpointTypeSeries, true, false, false, []string{"GET", "POST"}},
	"/select/*/prometheus/api/v1/labels": {EndpointTypeLabels, true, false, false, []string{"GET"}},
	"/select/*/prometheus/api/v1/label/*/values": {EndpointTypeLabels, true, false, false, []string{"GET"}},

	// Admin endpoints
	"/api/v1/admin/tsdb/delete_series":  {EndpointTypeDelete, false, false, true, []string{"POST"}},
	"/api/v1/admin/tsdb/clean_tombstones": {EndpointTypeAdmin, false, false, true, []string{"POST"}},
	"/api/v1/admin/tsdb/snapshot":       {EndpointTypeAdmin, false, false, true, []string{"POST"}},
	"/api/v1/status/tsdb":               {EndpointTypeAdmin, false, false, true, []string{"GET"}},
	"/api/v1/status/flags":              {EndpointTypeAdmin, false, false, true, []string{"GET"}},
	"/api/v1/status/config":             {EndpointTypeAdmin, false, false, true, []string{"GET"}},

	// Health and status endpoints (no auth required)
	"/health":                          {EndpointTypePassthrough, false, false, false, []string{"GET"}},
	"/metrics":                         {EndpointTypePassthrough, false, false, false, []string{"GET"}},
	"/api/v1/status/buildinfo":         {EndpointTypePassthrough, false, false, false, []string{"GET"}},
	"/api/v1/status/runtimeinfo":       {EndpointTypePassthrough, false, false, false, []string{"GET"}},
}

// RouterConfig holds configuration for the API router
type RouterConfig struct {
	TenantMapper *tenant.Mapper
	Logger       interface{}
}

// GetEndpointConfig returns the configuration for a given path
func GetEndpointConfig(path string) (*EndpointConfig, bool) {
	// Direct match
	if config, exists := vmEndpoints[path]; exists {
		return &config, true
	}

	// Pattern matching for paths with wildcards
	for pattern, config := range vmEndpoints {
		if matchesPattern(pattern, path) {
			return &config, true
		}
	}

	return nil, false
}

// matchesPattern checks if a path matches a pattern with wildcards
func matchesPattern(pattern, path string) bool {
	patternParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")

	if len(patternParts) != len(pathParts) {
		return false
	}

	for i, patternPart := range patternParts {
		if patternPart == "*" {
			continue
		}
		if patternPart != pathParts[i] {
			return false
		}
	}

	return true
}

// IsReadOnlyEndpoint checks if an endpoint is read-only
func IsReadOnlyEndpoint(path, method string) bool {
	config, exists := GetEndpointConfig(path)
	if !exists {
		return true // Default to read-only for unknown endpoints
	}

	// Write endpoints are not read-only
	if config.Type == EndpointTypeWrite || config.Type == EndpointTypeImport {
		return false
	}

	// DELETE operations are not read-only
	if method == "DELETE" {
		return false
	}

	// Admin operations that modify state are not read-only
	if config.Type == EndpointTypeAdmin || config.Type == EndpointTypeDelete {
		return false
	}

	return true
}

// RequiresTenantFiltering checks if an endpoint requires tenant filtering
func RequiresTenantFiltering(path string) bool {
	config, exists := GetEndpointConfig(path)
	if !exists {
		return false
	}
	return config.RequiresTenantFilter
}

// RequiresWriteAccess checks if an endpoint requires write access
func RequiresWriteAccess(path string) bool {
	config, exists := GetEndpointConfig(path)
	if !exists {
		return false
	}
	return config.RequiresWriteAccess
}

// RequiresAdminAccess checks if an endpoint requires admin access
func RequiresAdminAccess(path string) bool {
	config, exists := GetEndpointConfig(path)
	if !exists {
		return false
	}
	return config.RequiresAdminAccess
}

// IsMethodAllowed checks if HTTP method is allowed for endpoint
func IsMethodAllowed(path, method string) bool {
	config, exists := GetEndpointConfig(path)
	if !exists {
		return true // Allow unknown endpoints
	}

	for _, allowedMethod := range config.AllowedMethods {
		if allowedMethod == method {
			return true
		}
	}

	return false
}

// CheckAccess validates user access to an endpoint
func CheckAccess(userCtx *middleware.UserContext, path, method string) error {
	if userCtx == nil {
		return &AccessError{Type: "authentication", Message: "User not authenticated"}
	}

	// Check if method is allowed
	if !IsMethodAllowed(path, method) {
		return &AccessError{Type: "method", Message: "HTTP method not allowed for this endpoint"}
	}

	// Check admin access
	if RequiresAdminAccess(path) {
		if !hasAdminAccess(userCtx) {
			return &AccessError{Type: "authorization", Message: "Admin access required"}
		}
	}

	// Check write access
	if RequiresWriteAccess(path) || !IsReadOnlyEndpoint(path, method) {
		if userCtx.ReadOnly {
			return &AccessError{Type: "authorization", Message: "Write access denied - user has read-only permissions"}
		}
	}

	return nil
}

// hasAdminAccess checks if user has admin permissions
func hasAdminAccess(userCtx *middleware.UserContext) bool {
	adminGroups := []string{"admin", "platform-admin", "developers"}
	for _, userGroup := range userCtx.Groups {
		for _, adminGroup := range adminGroups {
			if userGroup == adminGroup {
				return true
			}
		}
	}
	return false
}

// AccessError represents access control errors
type AccessError struct {
	Type    string
	Message string
}

func (e *AccessError) Error() string {
	return e.Message
}