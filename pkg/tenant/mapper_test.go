package tenant

import (
	"reflect"
	"testing"
)

func TestMapper_GetUserAccess(t *testing.T) {
	mappings := []Mapping{
		{
			Groups:   []string{"admin", "platform-admin"},
			Tenants:  []string{"*"},
			ReadOnly: false,
		},
		{
			Groups:   []string{"team-alpha", "dev-alpha"},
			Tenants:  []string{"alpha", "alpha-dev", "alpha-staging"},
			ReadOnly: false,
		},
		{
			Groups:   []string{"team-beta"},
			Tenants:  []string{"beta", "beta-prod"},
			ReadOnly: false,
		},
		{
			Groups:   []string{"readonly-users", "auditors"},
			Tenants:  []string{"alpha", "beta"},
			ReadOnly: true,
		},
	}

	mapper := NewMapper(mappings)

	tests := []struct {
		name       string
		userGroups []string
		want       *AccessInfo
		wantErr    bool
	}{
		{
			name:       "admin user gets all access",
			userGroups: []string{"admin", "other-group"},
			want: &AccessInfo{
				Tenants:  []string{"*"},
				ReadOnly: false,
			},
			wantErr: false,
		},
		{
			name:       "team-alpha user gets alpha tenants",
			userGroups: []string{"team-alpha"},
			want: &AccessInfo{
				Tenants:  []string{"alpha", "alpha-dev", "alpha-staging"},
				ReadOnly: false,
			},
			wantErr: false,
		},
		{
			name:       "readonly user gets limited access",
			userGroups: []string{"readonly-users"},
			want: &AccessInfo{
				Tenants:  []string{"alpha", "beta"},
				ReadOnly: true,
			},
			wantErr: false,
		},
		{
			name:       "multiple matching groups - write access wins",
			userGroups: []string{"team-alpha", "readonly-users"},
			want: &AccessInfo{
				Tenants:  []string{"alpha", "alpha-dev", "alpha-staging", "beta"},
				ReadOnly: false,
			},
			wantErr: false,
		},
		{
			name:       "user with no matching groups",
			userGroups: []string{"unknown-group"},
			want: &AccessInfo{
				Tenants:  []string{},
				ReadOnly: true,
			},
			wantErr: false,
		},
		{
			name:       "empty user groups",
			userGroups: []string{},
			want: &AccessInfo{
				Tenants:  []string{},
				ReadOnly: true,
			},
			wantErr: false,
		},
		{
			name:       "nil user groups",
			userGroups: nil,
			want: &AccessInfo{
				Tenants:  []string{},
				ReadOnly: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mapper.GetUserAccess(tt.userGroups)
			if (err != nil) != tt.wantErr {
				t.Errorf("Mapper.GetUserAccess() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got.ReadOnly != tt.want.ReadOnly {
				t.Errorf("Mapper.GetUserAccess() ReadOnly = %v, want %v", got.ReadOnly, tt.want.ReadOnly)
			}

			if !reflect.DeepEqual(got.Tenants, tt.want.Tenants) {
				t.Errorf("Mapper.GetUserAccess() Tenants = %v, want %v", got.Tenants, tt.want.Tenants)
			}
		})
	}
}

func TestMapper_CanAccessTenant(t *testing.T) {
	mappings := []Mapping{
		{
			Groups:   []string{"admin"},
			Tenants:  []string{"*"},
			ReadOnly: false,
		},
		{
			Groups:   []string{"team-alpha"},
			Tenants:  []string{"alpha", "alpha-dev"},
			ReadOnly: false,
		},
	}

	mapper := NewMapper(mappings)

	tests := []struct {
		name       string
		userGroups []string
		tenantID   string
		want       bool
		wantErr    bool
	}{
		{
			name:       "admin can access any tenant",
			userGroups: []string{"admin"},
			tenantID:   "any-tenant",
			want:       true,
			wantErr:    false,
		},
		{
			name:       "team-alpha can access alpha tenant",
			userGroups: []string{"team-alpha"},
			tenantID:   "alpha",
			want:       true,
			wantErr:    false,
		},
		{
			name:       "team-alpha cannot access beta tenant",
			userGroups: []string{"team-alpha"},
			tenantID:   "beta",
			want:       false,
			wantErr:    false,
		},
		{
			name:       "unknown user cannot access any tenant",
			userGroups: []string{"unknown"},
			tenantID:   "alpha",
			want:       false,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mapper.CanAccessTenant(tt.userGroups, tt.tenantID)
			if (err != nil) != tt.wantErr {
				t.Errorf("Mapper.CanAccessTenant() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Mapper.CanAccessTenant() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapper_FilterTenantsInQuery(t *testing.T) {
	mappings := []Mapping{
		{
			Groups:   []string{"admin"},
			Tenants:  []string{"*"},
			ReadOnly: false,
		},
		{
			Groups:   []string{"team-alpha"},
			Tenants:  []string{"alpha", "alpha-dev"},
			ReadOnly: false,
		},
		{
			Groups:   []string{"team-beta"},
			Tenants:  []string{"beta"},
			ReadOnly: false,
		},
	}

	mapper := NewMapper(mappings)

	tests := []struct {
		name       string
		userGroups []string
		query      string
		want       string
		wantErr    bool
	}{
		{
			name:       "admin user query unchanged",
			userGroups: []string{"admin"},
			query:      "up",
			want:       "up",
			wantErr:    false,
		},
		{
			name:       "single tenant user gets filtered query",
			userGroups: []string{"team-beta"},
			query:      "up",
			want:       "up{vm_account_id=~\"beta\"}",
			wantErr:    false,
		},
		{
			name:       "multiple tenants user gets regex filter",
			userGroups: []string{"team-alpha"},
			query:      "http_requests_total",
			want:       "http_requests_total{vm_account_id=~\"alpha|alpha-dev\"}",
			wantErr:    false,
		},
		{
			name:       "query with existing labels",
			userGroups: []string{"team-beta"},
			query:      "up{instance=\"localhost:9090\"}",
			want:       "up{vm_account_id=~\"beta\",instance=\"localhost:9090\"}",
			wantErr:    false,
		},
		{
			name:       "complex query with multiple metrics",
			userGroups: []string{"team-beta"},
			query:      "rate(http_requests_total[5m]) + rate(http_errors_total[5m])",
			want:       "rate(http_requests_total{vm_account_id=~\"beta\"}[5m]) + rate(http_errors_total{vm_account_id=~\"beta\"}[5m])",
			wantErr:    false,
		},
		{
			name:       "user with no access gets error",
			userGroups: []string{"unknown"},
			query:      "up",
			want:       "",
			wantErr:    true,
		},
		{
			name:       "empty query returns empty",
			userGroups: []string{"team-alpha"},
			query:      "",
			want:       "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mapper.FilterTenantsInQuery(tt.userGroups, tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("Mapper.FilterTenantsInQuery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Mapper.FilterTenantsInQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildTenantRegex(t *testing.T) {
	tests := []struct {
		name    string
		tenants []string
		want    string
	}{
		{
			name:    "single tenant",
			tenants: []string{"alpha"},
			want:    "alpha",
		},
		{
			name:    "multiple tenants",
			tenants: []string{"alpha", "beta", "gamma"},
			want:    "alpha|beta|gamma",
		},
		{
			name:    "empty tenants",
			tenants: []string{},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildTenantRegex(tt.tenants); got != tt.want {
				t.Errorf("buildTenantRegex() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsMetricName(t *testing.T) {
	tests := []struct {
		name string
		word string
		want bool
	}{
		{
			name: "valid metric name",
			word: "http_requests_total",
			want: true,
		},
		{
			name: "valid metric with underscores",
			word: "prometheus_http_requests_total",
			want: true,
		},
		{
			name: "metric with numbers",
			word: "node_cpu_seconds_total",
			want: true,
		},
		{
			name: "reserved word - sum",
			word: "sum",
			want: false,
		},
		{
			name: "reserved word - rate",
			word: "rate",
			want: false,
		},
		{
			name: "starts with number",
			word: "1metric",
			want: false,
		},
		{
			name: "contains operators",
			word: "metric+other",
			want: false,
		},
		{
			name: "contains brackets",
			word: "metric[5m]",
			want: false,
		},
		{
			name: "empty string",
			word: "",
			want: false,
		},
		{
			name: "contains spaces",
			word: "metric name",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMetricName(tt.word); got != tt.want {
				t.Errorf("isMetricName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractMetricNames(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "simple metric",
			query: "up",
			want:  []string{"up"},
		},
		{
			name:  "multiple metrics",
			query: "up + node_load1",
			want:  []string{"up", "node_load1"},
		},
		{
			name:  "complex query with functions",
			query: "rate(http_requests_total[5m]) + increase(http_errors_total[1h])",
			want:  []string{"http_requests_total", "http_errors_total"},
		},
		{
			name:  "query with reserved words",
			query: "sum by (instance) (up)",
			want:  []string{"up"},
		},
		{
			name:  "query with duplicates",
			query: "up + up",
			want:  []string{"up"},
		},
		{
			name:  "no metrics",
			query: "1 + 2",
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractMetricNames(tt.query); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractMetricNames() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasAnyGroup(t *testing.T) {
	tests := []struct {
		name           string
		userGroups     []string
		requiredGroups []string
		want           bool
	}{
		{
			name:           "user has required group",
			userGroups:     []string{"admin", "user"},
			requiredGroups: []string{"admin"},
			want:           true,
		},
		{
			name:           "user has one of required groups",
			userGroups:     []string{"editor", "viewer"},
			requiredGroups: []string{"admin", "editor", "owner"},
			want:           true,
		},
		{
			name:           "user has no required groups",
			userGroups:     []string{"guest"},
			requiredGroups: []string{"admin", "editor"},
			want:           false,
		},
		{
			name:           "empty user groups",
			userGroups:     []string{},
			requiredGroups: []string{"admin"},
			want:           false,
		},
		{
			name:           "empty required groups",
			userGroups:     []string{"admin"},
			requiredGroups: []string{},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasAnyGroup(tt.userGroups, tt.requiredGroups); got != tt.want {
				t.Errorf("hasAnyGroup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRemoveDuplicates(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		want  []string
	}{
		{
			name:  "no duplicates",
			slice: []string{"a", "b", "c"},
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "with duplicates",
			slice: []string{"a", "b", "a", "c", "b"},
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "empty slice",
			slice: []string{},
			want:  []string{},
		},
		{
			name:  "single element",
			slice: []string{"a"},
			want:  []string{"a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := removeDuplicates(tt.slice); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("removeDuplicates() = %v, want %v", got, tt.want)
			}
		})
	}
}