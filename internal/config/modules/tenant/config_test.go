package tenant_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config/modules/tenant"
)

func TestTenantConfig_Validate_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config tenant.Config
	}{
		{
			name: "valid minimal config",
			config: tenant.Config{
				Filter: tenant.FilterConfig{
					Strategy: "orConditions",
					Labels: tenant.LabelsConfig{
						Account:      "vm_account_id",
						Project:      "vm_project_id",
						UseProjectID: false,
					},
				},
				Mappings: []tenant.MappingConfig{
					{
						Groups: []string{"admin"},
						VMTenants: []tenant.VMTenantConfig{
							{Account: "0", Project: ""},
						},
						Permissions: []string{"read", "write"},
					},
				},
			},
		},
		{
			name: "valid with multiple mappings and tenants",
			config: tenant.Config{
				Filter: tenant.FilterConfig{
					Strategy: "orConditions",
					Labels: tenant.LabelsConfig{
						Account:      "account_id",
						Project:      "project_id",
						UseProjectID: true,
					},
				},
				Mappings: []tenant.MappingConfig{
					{
						Groups: []string{"admin", "sre"},
						VMTenants: []tenant.VMTenantConfig{
							{Account: "0", Project: "default"},
							{Account: "1", Project: "monitoring"},
						},
						Permissions: []string{"read", "write"},
					},
					{
						Groups: []string{"developer"},
						VMTenants: []tenant.VMTenantConfig{
							{Account: "2", Project: "dev"},
						},
						Permissions: []string{"read"},
					},
					{
						Groups: []string{"viewer"},
						VMTenants: []tenant.VMTenantConfig{
							{Account: "0", Project: "public"},
						},
						Permissions: []string{"read"},
					},
				},
			},
		},
		{
			name: "valid with read-only permissions",
			config: tenant.Config{
				Filter: tenant.FilterConfig{
					Strategy: "orConditions",
					Labels: tenant.LabelsConfig{
						Account:      "vm_account_id",
						Project:      "vm_project_id",
						UseProjectID: false,
					},
				},
				Mappings: []tenant.MappingConfig{
					{
						Groups: []string{"readonly"},
						VMTenants: []tenant.VMTenantConfig{
							{Account: "0", Project: ""},
						},
						Permissions: []string{"read"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestTenantConfig_Validate_Failure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      tenant.Config
		expectedErr string
	}{
		{
			name: "invalid filter strategy",
			config: tenant.Config{
				Filter: tenant.FilterConfig{
					Strategy: "invalid_strategy",
					Labels: tenant.LabelsConfig{
						Account: "vm_account_id",
						Project: "vm_project_id",
					},
				},
				Mappings: []tenant.MappingConfig{
					{
						Groups: []string{"admin"},
						VMTenants: []tenant.VMTenantConfig{
							{Account: "0", Project: ""},
						},
						Permissions: []string{"read", "write"},
					},
				},
			},
			expectedErr: "invalid filter strategy",
		},
		{
			name: "empty account label",
			config: tenant.Config{
				Filter: tenant.FilterConfig{
					Strategy: "orConditions",
					Labels: tenant.LabelsConfig{
						Account: "",
						Project: "vm_project_id",
					},
				},
				Mappings: []tenant.MappingConfig{
					{
						Groups: []string{"admin"},
						VMTenants: []tenant.VMTenantConfig{
							{Account: "0", Project: ""},
						},
						Permissions: []string{"read", "write"},
					},
				},
			},
			expectedErr: "account label is required",
		},
		{
			name: "empty project label",
			config: tenant.Config{
				Filter: tenant.FilterConfig{
					Strategy: "orConditions",
					Labels: tenant.LabelsConfig{
						Account: "vm_account_id",
						Project: "",
					},
				},
				Mappings: []tenant.MappingConfig{
					{
						Groups: []string{"admin"},
						VMTenants: []tenant.VMTenantConfig{
							{Account: "0", Project: ""},
						},
						Permissions: []string{"read", "write"},
					},
				},
			},
			expectedErr: "project label is required",
		},
		{
			name: "no tenant mappings",
			config: tenant.Config{
				Filter: tenant.FilterConfig{
					Strategy: "orConditions",
					Labels: tenant.LabelsConfig{
						Account: "vm_account_id",
						Project: "vm_project_id",
					},
				},
				Mappings: []tenant.MappingConfig{},
			},
			expectedErr: "at least one tenant mapping is required",
		},
		{
			name: "mapping without groups",
			config: tenant.Config{
				Filter: tenant.FilterConfig{
					Strategy: "orConditions",
					Labels: tenant.LabelsConfig{
						Account: "vm_account_id",
						Project: "vm_project_id",
					},
				},
				Mappings: []tenant.MappingConfig{
					{
						Groups: []string{},
						VMTenants: []tenant.VMTenantConfig{
							{Account: "0", Project: ""},
						},
						Permissions: []string{"read", "write"},
					},
				},
			},
			expectedErr: "at least one group is required in mapping",
		},
		{
			name: "mapping without VM tenants",
			config: tenant.Config{
				Filter: tenant.FilterConfig{
					Strategy: "orConditions",
					Labels: tenant.LabelsConfig{
						Account: "vm_account_id",
						Project: "vm_project_id",
					},
				},
				Mappings: []tenant.MappingConfig{
					{
						Groups:      []string{"admin"},
						VMTenants:   []tenant.VMTenantConfig{},
						Permissions: []string{"read", "write"},
					},
				},
			},
			expectedErr: "at least one VM tenant is required in mapping",
		},
		{
			name: "VM tenant without account",
			config: tenant.Config{
				Filter: tenant.FilterConfig{
					Strategy: "orConditions",
					Labels: tenant.LabelsConfig{
						Account: "vm_account_id",
						Project: "vm_project_id",
					},
				},
				Mappings: []tenant.MappingConfig{
					{
						Groups: []string{"admin"},
						VMTenants: []tenant.VMTenantConfig{
							{Account: "", Project: "project1"},
						},
						Permissions: []string{"read", "write"},
					},
				},
			},
			expectedErr: "VM tenant account is required",
		},
		{
			name: "invalid permission",
			config: tenant.Config{
				Filter: tenant.FilterConfig{
					Strategy: "orConditions",
					Labels: tenant.LabelsConfig{
						Account: "vm_account_id",
						Project: "vm_project_id",
					},
				},
				Mappings: []tenant.MappingConfig{
					{
						Groups: []string{"admin"},
						VMTenants: []tenant.VMTenantConfig{
							{Account: "0", Project: ""},
						},
						Permissions: []string{"read", "invalid"},
					},
				},
			},
			expectedErr: "invalid permission",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestTenantConfig_Defaults(t *testing.T) {
	t.Parallel()

	defaults := tenant.GetDefaults()

	assert.Equal(t, "orConditions", defaults.Filter.Strategy)
	assert.Equal(t, "vm_account_id", defaults.Filter.Labels.Account)
	assert.Equal(t, "vm_project_id", defaults.Filter.Labels.Project)
	assert.True(t, defaults.Filter.Labels.UseProjectID)
	assert.Equal(t, []tenant.MappingConfig{}, defaults.Mappings)
}

func TestFilterConfig_StrategyValidation(t *testing.T) {
	t.Parallel()

	validStrategies := []string{"orConditions", "andConditions"}
	invalidStrategies := []string{"or_conditions", "and_conditions", "regex", "invalid", ""}

	// Test valid strategies
	for _, strategy := range validStrategies {
		t.Run("valid_strategy_"+strategy, func(t *testing.T) {
			t.Parallel()
			config := tenant.FilterConfig{
				Strategy: strategy,
				Labels: tenant.LabelsConfig{
					Account: "vm_account_id",
					Project: "vm_project_id",
				},
			}
			err := config.Validate()
			assert.NoError(t, err)
		})
	}

	// Test invalid strategies
	for _, strategy := range invalidStrategies {
		t.Run("invalid_strategy_"+strategy, func(t *testing.T) {
			t.Parallel()
			config := tenant.FilterConfig{
				Strategy: strategy,
				Labels: tenant.LabelsConfig{
					Account: "vm_account_id",
					Project: "vm_project_id",
				},
			}
			err := config.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid filter strategy")
		})
	}
}

func TestMappingConfig_PermissionValidation(t *testing.T) {
	t.Parallel()

	validPermissions := []string{"read", "write"}
	invalidPermissions := []string{"delete", "admin", "execute", ""}

	// Test all valid permissions
	config := tenant.MappingConfig{
		Groups: []string{"test"},
		VMTenants: []tenant.VMTenantConfig{
			{Account: "0", Project: ""},
		},
		Permissions: validPermissions,
	}
	err := config.Validate()
	require.NoError(t, err)

	// Test each invalid permission
	for _, invalidPerm := range invalidPermissions {
		t.Run("invalid_permission_"+invalidPerm, func(t *testing.T) {
			t.Parallel()
			mappingConfig := tenant.MappingConfig{
				Groups: []string{"test"},
				VMTenants: []tenant.VMTenantConfig{
					{Account: "0", Project: ""},
				},
				Permissions: []string{"read", invalidPerm},
			}
			validationErr := mappingConfig.Validate()
			require.Error(t, validationErr)
			assert.Contains(t, validationErr.Error(), "invalid permission")
		})
	}
}

func TestVMTenantConfig_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		vmTenant    tenant.VMTenantConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid with account only",
			vmTenant: tenant.VMTenantConfig{
				Account: "0",
				Project: "",
			},
			expectError: false,
		},
		{
			name: "valid with account and project",
			vmTenant: tenant.VMTenantConfig{
				Account: "1",
				Project: "monitoring",
			},
			expectError: false,
		},
		{
			name: "invalid without account",
			vmTenant: tenant.VMTenantConfig{
				Account: "",
				Project: "project1",
			},
			expectError: true,
			errorMsg:    "VM tenant account is required",
		},
		{
			name: "valid with numeric account",
			vmTenant: tenant.VMTenantConfig{
				Account: "12345",
				Project: "prod",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.vmTenant.Validate()
			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
