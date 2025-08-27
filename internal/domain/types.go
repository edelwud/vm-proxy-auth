// Package domain contains core business types and interfaces
package domain

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// User represents an authenticated user with their permissions.
type User struct {
	ID             string     `json:"id"`
	Email          string     `json:"email,omitempty"`
	Username       string     `json:"username,omitempty"`
	Groups         []string   `json:"groups"`
	AllowedTenants []string   `json:"allowed_tenants"`
	VMTenants      []VMTenant `json:"vm_tenants,omitempty"` // VictoriaMetrics specific
	ReadOnly       bool       `json:"read_only"`
	ExpiresAt      time.Time  `json:"expires_at"`
}

// VMTenant represents a VictoriaMetrics tenant (account:project).
type VMTenant struct {
	AccountID string `json:"account_id"`
	ProjectID string `json:"project_id,omitempty"` // Optional
}

// String returns the tenant identifier for VictoriaMetrics.
func (t VMTenant) String() string {
	if t.ProjectID != "" {
		return fmt.Sprintf("%s:%s", t.AccountID, t.ProjectID)
	}

	return t.AccountID
}

// ProxyRequest represents a request to be proxied to upstream.
type ProxyRequest struct {
	User            *User
	OriginalRequest *http.Request
	FilteredQuery   string
	TargetTenant    string
}

// ProxyResponse wraps the upstream response.
type ProxyResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// RequestContext contains request-specific data.
type RequestContext struct {
	User      *User
	RequestID string
	StartTime time.Time
}

// AuthResult represents the result of authentication.
type AuthResult struct {
	User  *User
	Error error
}

// AuthService handles authentication logic.
type AuthService interface {
	Authenticate(ctx context.Context, token string) (*User, error)
}

// TenantService handles tenant-related operations.
type TenantService interface {
	FilterQuery(ctx context.Context, user *User, query string) (string, error)
	CanAccessTenant(ctx context.Context, user *User, tenantID string) bool
	DetermineTargetTenant(ctx context.Context, user *User, r *http.Request) (string, error)
}

// ProxyService handles request proxying.
type ProxyService interface {
	Forward(ctx context.Context, req *ProxyRequest) (*ProxyResponse, error)
}

// WriteService handles write operations with tenant injection.
type WriteService interface {
	ProcessWrite(
		ctx context.Context,
		data []byte,
		tenantID string,
		contentType string,
	) ([]byte, error)
}

// AccessControlService handles authorization.
type AccessControlService interface {
	CanAccess(ctx context.Context, user *User, path string, method string) error
}

// MetricsService handles metrics collection.
type MetricsService interface {
	RecordRequest(
		ctx context.Context,
		method, path, status string,
		duration time.Duration,
		user *User,
	)
	RecordUpstream(
		ctx context.Context,
		method, path, status string,
		duration time.Duration,
		tenants []string,
	)
	RecordQueryFilter(
		ctx context.Context,
		userID string,
		tenantCount int,
		filterApplied bool,
		duration time.Duration,
	)
	RecordAuthAttempt(ctx context.Context, userID, status string)
	RecordTenantAccess(ctx context.Context, userID, tenantID string, allowed bool)
	Handler() http.Handler
}

// Logger provides structured logging interface.
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	With(fields ...Field) Logger
}

// Field represents a structured log field.
type Field struct {
	Key   string
	Value interface{}
}

// LogFormat represents the output format for logs.
type LogFormat string

const (
	// LogFormatJSON outputs logs in JSON format (default for production).
	LogFormatJSON LogFormat = "json"
	// LogFormatLogFmt outputs logs in logfmt format (structured key=value pairs).
	LogFormatLogFmt LogFormat = "logfmt"
	// LogFormatPretty outputs logs in human-readable format with colors (development).
	LogFormatPretty LogFormat = "pretty"
	// LogFormatConsole outputs logs in simple console format (minimal output).
	LogFormatConsole LogFormat = "console"
)

// LogLevel represents the logging level.
type LogLevel string

const (
	// LogLevelDebug enables debug-level logging.
	LogLevelDebug LogLevel = "debug"
	// LogLevelInfo enables info-level logging.
	LogLevelInfo LogLevel = "info"
	// LogLevelWarn enables warning-level logging.
	LogLevelWarn LogLevel = "warn"
	// LogLevelError enables error-level logging.
	LogLevelError LogLevel = "error"
)

// ContextualLogger provides contextual logging capabilities for specific components.
type ContextualLogger interface {
	Logger
	// WithComponent creates a logger with pre-configured component context.
	WithComponent(component string) Logger
	// WithRequestID creates a logger with request ID context.
	WithRequestID(requestID string) Logger
	// WithUser creates a logger with user context.
	WithUser(userID string) Logger
	// WithTenant creates a logger with tenant context.
	WithTenant(accountID, projectID string) Logger
}

// TenantFilterStrategy defines how tenant filtering should be applied to PromQL queries.
type TenantFilterStrategy string

const (
	// TenantFilterStrategyOR uses separate OR conditions for each tenant pair.
	// This ensures exact tenant isolation and prevents cross-tenant data leakage.
	// Example: {vm_account_id="1000"} or {vm_account_id="2000",vm_project_id="20"}.
	TenantFilterStrategyOR TenantFilterStrategy = "or_conditions"
)

// TenantFilterConfig contains configuration for tenant filtering strategy.
type TenantFilterConfig struct {
	Strategy TenantFilterStrategy `yaml:"strategy" default:"or_conditions"`
}

// IsValid validates the tenant filter strategy.
func (s TenantFilterStrategy) IsValid() bool {
	return s == TenantFilterStrategyOR
}

// TenantFilter represents a filter that can be applied to PromQL queries.
type TenantFilter interface {
	// ApplyToVectorSelector applies tenant filtering to a single vector selector.
	// Returns true if the selector was modified, false otherwise.
	ApplyToVectorSelector(vs VectorSelector, tenants []VMTenant, config TenantLabelsConfig) (bool, error)
}

// VectorSelector represents a PromQL vector selector that can be modified.
// This interface abstracts away the Prometheus parser dependency from domain layer.
type VectorSelector interface {
	// GetName returns the metric name.
	GetName() string
	// HasLabel checks if a label matcher exists.
	HasLabel(name string) bool
	// AddLabel adds a label matcher.
	AddLabel(name, value string, matchType LabelMatchType) error
}

// LabelMatchType defines the type of label matching.
type LabelMatchType int

const (
	// LabelMatchEqual represents exact equality (=).
	LabelMatchEqual LabelMatchType = iota
	// LabelMatchRegexp represents regex matching (=~).
	LabelMatchRegexp
)

// TenantLabelsConfig contains configuration for tenant label names.
type TenantLabelsConfig struct {
	TenantLabel  string
	ProjectLabel string
	UseProjectID bool
}
