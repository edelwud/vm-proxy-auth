package server

import (
	"testing"

	"github.com/finlego/prometheus-oauth-gateway/internal/middleware"
)

func TestGetEndpointConfig(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		wantExists     bool
		wantType       EndpointType
		wantTenantFilter bool
		wantWriteAccess bool
		wantAdminAccess bool
	}{
		{
			name:             "query endpoint",
			path:             "/api/v1/query",
			wantExists:       true,
			wantType:         EndpointTypeQuery,
			wantTenantFilter: true,
			wantWriteAccess:  false,
			wantAdminAccess:  false,
		},
		{
			name:             "write endpoint",
			path:             "/api/v1/write",
			wantExists:       true,
			wantType:         EndpointTypeWrite,
			wantTenantFilter: false,
			wantWriteAccess:  true,
			wantAdminAccess:  false,
		},
		{
			name:             "admin endpoint",
			path:             "/api/v1/admin/tsdb/delete_series",
			wantExists:       true,
			wantType:         EndpointTypeDelete,
			wantTenantFilter: false,
			wantWriteAccess:  false,
			wantAdminAccess:  true,
		},
		{
			name:             "multi-tenant query endpoint",
			path:             "/select/0/prometheus/api/v1/query",
			wantExists:       true,
			wantType:         EndpointTypeQuery,
			wantTenantFilter: true,
			wantWriteAccess:  false,
			wantAdminAccess:  false,
		},
		{
			name:       "unknown endpoint",
			path:       "/unknown/endpoint",
			wantExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, exists := GetEndpointConfig(tt.path)
			
			if exists != tt.wantExists {
				t.Errorf("GetEndpointConfig() exists = %v, want %v", exists, tt.wantExists)
				return
			}
			
			if tt.wantExists && config != nil {
				if config.Type != tt.wantType {
					t.Errorf("EndpointConfig.Type = %v, want %v", config.Type, tt.wantType)
				}
				if config.RequiresTenantFilter != tt.wantTenantFilter {
					t.Errorf("EndpointConfig.RequiresTenantFilter = %v, want %v", config.RequiresTenantFilter, tt.wantTenantFilter)
				}
				if config.RequiresWriteAccess != tt.wantWriteAccess {
					t.Errorf("EndpointConfig.RequiresWriteAccess = %v, want %v", config.RequiresWriteAccess, tt.wantWriteAccess)
				}
				if config.RequiresAdminAccess != tt.wantAdminAccess {
					t.Errorf("EndpointConfig.RequiresAdminAccess = %v, want %v", config.RequiresAdminAccess, tt.wantAdminAccess)
				}
			}
		})
	}
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{
			name:    "exact match",
			pattern: "/api/v1/query",
			path:    "/api/v1/query",
			want:    true,
		},
		{
			name:    "wildcard match",
			pattern: "/api/v1/label/*/values",
			path:    "/api/v1/label/job/values",
			want:    true,
		},
		{
			name:    "multiple wildcards",
			pattern: "/select/*/prometheus/api/v1/query",
			path:    "/select/0/prometheus/api/v1/query",
			want:    true,
		},
		{
			name:    "no match - different path",
			pattern: "/api/v1/query",
			path:    "/api/v1/series",
			want:    false,
		},
		{
			name:    "no match - different length",
			pattern: "/api/v1/query",
			path:    "/api/v1/query/range",
			want:    false,
		},
		{
			name:    "wildcard no match",
			pattern: "/api/v1/label/*/values",
			path:    "/api/v1/label/job/metadata",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesPattern(tt.pattern, tt.path); got != tt.want {
				t.Errorf("matchesPattern() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsReadOnlyEndpoint(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		method string
		want   bool
	}{
		{
			name:   "query endpoint GET",
			path:   "/api/v1/query",
			method: "GET",
			want:   true,
		},
		{
			name:   "query endpoint POST",
			path:   "/api/v1/query",
			method: "POST",
			want:   true,
		},
		{
			name:   "write endpoint",
			path:   "/api/v1/write",
			method: "POST",
			want:   false,
		},
		{
			name:   "series DELETE",
			path:   "/api/v1/series",
			method: "DELETE",
			want:   false,
		},
		{
			name:   "admin endpoint",
			path:   "/api/v1/admin/tsdb/delete_series",
			method: "POST",
			want:   false,
		},
		{
			name:   "unknown endpoint",
			path:   "/unknown",
			method: "GET",
			want:   true, // Default to read-only
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReadOnlyEndpoint(tt.path, tt.method); got != tt.want {
				t.Errorf("IsReadOnlyEndpoint() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckAccess(t *testing.T) {
	tests := []struct {
		name    string
		userCtx *middleware.UserContext
		path    string
		method  string
		wantErr bool
		errType string
	}{
		{
			name:    "nil user context",
			userCtx: nil,
			path:    "/api/v1/query",
			method:  "GET",
			wantErr: true,
			errType: "authentication",
		},
		{
			name: "valid query access",
			userCtx: &middleware.UserContext{
				UserID:         "user1",
				Groups:         []string{"team-alpha"},
				AllowedTenants: []string{"alpha"},
				ReadOnly:       false,
			},
			path:    "/api/v1/query",
			method:  "GET",
			wantErr: false,
		},
		{
			name: "read-only user write denied",
			userCtx: &middleware.UserContext{
				UserID:         "user1",
				Groups:         []string{"readonly"},
				AllowedTenants: []string{"alpha"},
				ReadOnly:       true,
			},
			path:    "/api/v1/write",
			method:  "POST",
			wantErr: true,
			errType: "authorization",
		},
		{
			name: "non-admin admin access denied",
			userCtx: &middleware.UserContext{
				UserID:         "user1",
				Groups:         []string{"team-alpha"},
				AllowedTenants: []string{"alpha"},
				ReadOnly:       false,
			},
			path:    "/api/v1/admin/tsdb/delete_series",
			method:  "POST",
			wantErr: true,
			errType: "authorization",
		},
		{
			name: "admin user admin access allowed",
			userCtx: &middleware.UserContext{
				UserID:         "admin",
				Groups:         []string{"admin"},
				AllowedTenants: []string{"*"},
				ReadOnly:       false,
			},
			path:    "/api/v1/admin/tsdb/delete_series",
			method:  "POST",
			wantErr: false,
		},
		{
			name: "method not allowed",
			userCtx: &middleware.UserContext{
				UserID:         "user1",
				Groups:         []string{"team-alpha"},
				AllowedTenants: []string{"alpha"},
				ReadOnly:       false,
			},
			path:    "/api/v1/query",
			method:  "DELETE",
			wantErr: true,
			errType: "method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckAccess(tt.userCtx, tt.path, tt.method)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("CheckAccess() expected error but got none")
					return
				}
				
				if accessErr, ok := err.(*AccessError); ok {
					if accessErr.Type != tt.errType {
						t.Errorf("CheckAccess() error type = %v, want %v", accessErr.Type, tt.errType)
					}
				} else {
					t.Errorf("CheckAccess() expected AccessError but got %T", err)
				}
			} else if err != nil {
				t.Errorf("CheckAccess() unexpected error: %v", err)
			}
		})
	}
}

func TestHasAdminAccess(t *testing.T) {
	tests := []struct {
		name    string
		userCtx *middleware.UserContext
		want    bool
	}{
		{
			name: "admin group",
			userCtx: &middleware.UserContext{
				Groups: []string{"admin"},
			},
			want: true,
		},
		{
			name: "platform-admin group",
			userCtx: &middleware.UserContext{
				Groups: []string{"platform-admin"},
			},
			want: true,
		},
		{
			name: "developers group",
			userCtx: &middleware.UserContext{
				Groups: []string{"developers"},
			},
			want: true,
		},
		{
			name: "regular user",
			userCtx: &middleware.UserContext{
				Groups: []string{"team-alpha", "users"},
			},
			want: false,
		},
		{
			name: "no groups",
			userCtx: &middleware.UserContext{
				Groups: []string{},
			},
			want: false,
		},
		{
			name: "mixed groups with admin",
			userCtx: &middleware.UserContext{
				Groups: []string{"team-alpha", "admin", "users"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasAdminAccess(tt.userCtx); got != tt.want {
				t.Errorf("hasAdminAccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsMethodAllowed(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		method string
		want   bool
	}{
		{
			name:   "query GET allowed",
			path:   "/api/v1/query",
			method: "GET",
			want:   true,
		},
		{
			name:   "query POST allowed",
			path:   "/api/v1/query",
			method: "POST",
			want:   true,
		},
		{
			name:   "query DELETE not allowed",
			path:   "/api/v1/query",
			method: "DELETE",
			want:   false,
		},
		{
			name:   "write POST allowed",
			path:   "/api/v1/write",
			method: "POST",
			want:   true,
		},
		{
			name:   "write GET not allowed",
			path:   "/api/v1/write",
			method: "GET",
			want:   false,
		},
		{
			name:   "unknown endpoint defaults to allowed",
			path:   "/unknown",
			method: "GET",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsMethodAllowed(tt.path, tt.method); got != tt.want {
				t.Errorf("IsMethodAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}