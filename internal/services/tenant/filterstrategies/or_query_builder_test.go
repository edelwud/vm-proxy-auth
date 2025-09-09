package filterstrategies_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config/modules/tenant"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/tenant/filterstrategies"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestORQueryBuilder_BuildSecureQuery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		query       string
		tenants     []domain.VMTenant
		useProject  bool
		expectError bool
		contains    []string
		notContains []string
	}{
		{
			name:  "tenant filtering",
			query: "up",
			tenants: []domain.VMTenant{
				{AccountID: "1000", ProjectID: "10"},
			},
			useProject: true,
			contains: []string{
				"vm_account_id=\"1000\"",
				"vm_project_id=\"10\"",
			},
			notContains: []string{
				"or",
				"regex",
			},
		},
		{
			name:  "multiple tenants with or strategy",
			query: "up",
			tenants: []domain.VMTenant{
				{AccountID: "1000", ProjectID: "10"},
				{AccountID: "2000", ProjectID: "20"},
			},
			useProject: true,
			contains: []string{
				"vm_account_id=\"1000\"",
				"vm_project_id=\"10\"",
				"vm_account_id=\"2000\"",
				"vm_project_id=\"20\"",
				" or ",
			},
			notContains: []string{
				"regex",
			},
		},
		{
			name:  "with wildcard project",
			query: "up",
			tenants: []domain.VMTenant{
				{AccountID: "1000", ProjectID: ".*"},
				{AccountID: "2000", ProjectID: "20"},
			},
			useProject: true,
			contains: []string{
				"vm_account_id=\"1000\"",
				"vm_account_id=\"2000\"",
				"vm_project_id=\"20\"",
				" or ",
			},
			notContains: []string{
				"vm_project_id=\".*\"",
			},
		},
		{
			name:  "multiple projects in same account",
			query: "up",
			tenants: []domain.VMTenant{
				{AccountID: "1000", ProjectID: "10"},
				{AccountID: "1000", ProjectID: "20"},
			},
			useProject: true,
			contains: []string{
				"vm_account_id=\"1000\"",
				"vm_project_id=~\"(10|20)\"",
			},
			notContains: []string{
				" or ",
			},
		},
		{
			name:  "complex query with or strategy",
			query: "rate(http_requests_total[5m]) / rate(http_requests_total_count[5m])",
			tenants: []domain.VMTenant{
				{AccountID: "1000", ProjectID: "10"},
				{AccountID: "2000", ProjectID: "20"},
			},
			useProject: true,
			contains: []string{
				"rate(http_requests_total{vm_account_id=\"1000\",vm_project_id=\"10\"}[5m])",
				"rate(http_requests_total{vm_account_id=\"2000\",vm_project_id=\"20\"}[5m])",
				" or ",
			},
		},
		{
			name:        "no tenants",
			query:       "up",
			tenants:     []domain.VMTenant{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger := testutils.NewMockLogger()
			builder := filterstrategies.NewORQueryBuilder(logger)

			tenantCfg := &tenant.FilterConfig{
				Strategy: string(domain.TenantFilterStrategyOrConditions),
				Labels: tenant.LabelsConfig{
					Account:      "vm_account_id",
					Project:      "vm_project_id",
					UseProjectID: tt.useProject,
				},
			}

			result, err := builder.BuildSecureQuery(tt.query, tt.tenants, tenantCfg)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			t.Logf("Result: %s", result)

			for _, s := range tt.contains {
				assert.Contains(t, result, s)
			}

			for _, s := range tt.notContains {
				assert.NotContains(t, result, s)
			}
		})
	}
}
