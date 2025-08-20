// Package domain contains core business types and interfaces
package domain

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// User represents an authenticated user with their permissions
type User struct {
	ID             string       `json:"id"`
	Email          string       `json:"email,omitempty"`
	Username       string       `json:"username,omitempty"`
	Groups         []string     `json:"groups"`
	AllowedTenants []string     `json:"allowed_tenants"`
	VMTenants      []VMTenant   `json:"vm_tenants,omitempty"` // VictoriaMetrics specific
	ReadOnly       bool         `json:"read_only"`
	ExpiresAt      time.Time    `json:"expires_at"`
}

// VMTenant represents a VictoriaMetrics tenant (account:project)
type VMTenant struct {
	AccountID string `json:"account_id"`
	ProjectID string `json:"project_id,omitempty"` // Optional
}

// String returns the tenant identifier for VictoriaMetrics
func (t VMTenant) String() string {
	if t.ProjectID != "" {
		return fmt.Sprintf("%s:%s", t.AccountID, t.ProjectID)
	}
	return t.AccountID
}

// ProxyRequest represents a request to be proxied to upstream
type ProxyRequest struct {
	User            *User
	OriginalRequest *http.Request
	FilteredQuery   string
	TargetTenant    string
}

// ProxyResponse wraps the upstream response
type ProxyResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// RequestContext contains request-specific data
type RequestContext struct {
	User      *User
	RequestID string
	StartTime time.Time
}

// AuthResult represents the result of authentication
type AuthResult struct {
	User  *User
	Error error
}

// AuthService handles authentication logic
type AuthService interface {
	Authenticate(ctx context.Context, token string) (*User, error)
}

// TenantService handles tenant-related operations
type TenantService interface {
	FilterQuery(ctx context.Context, user *User, query string) (string, error)
	CanAccessTenant(ctx context.Context, user *User, tenantID string) bool
	DetermineTargetTenant(ctx context.Context, user *User, r *http.Request) (string, error)
}

// ProxyService handles request proxying
type ProxyService interface {
	Forward(ctx context.Context, req *ProxyRequest) (*ProxyResponse, error)
}

// WriteService handles write operations with tenant injection
type WriteService interface {
	ProcessWrite(ctx context.Context, data []byte, tenantID string, contentType string) ([]byte, error)
}

// AccessControlService handles authorization
type AccessControlService interface {
	CanAccess(ctx context.Context, user *User, path string, method string) error
}

// MetricsService handles metrics collection
type MetricsService interface {
	RecordRequest(ctx context.Context, method, path, status string, duration time.Duration, user *User)
	RecordUpstream(ctx context.Context, method, path, status string, duration time.Duration, tenants []string)
	RecordQueryFilter(ctx context.Context, userID string, tenantCount int, filterApplied bool, duration time.Duration)
	RecordAuthAttempt(ctx context.Context, userID, status string)
	RecordTenantAccess(ctx context.Context, userID, tenantID string, allowed bool)
	Handler() http.Handler
}


// Logger provides structured logging interface
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	With(fields ...Field) Logger
}

// Field represents a structured log field
type Field struct {
	Key   string
	Value interface{}
}