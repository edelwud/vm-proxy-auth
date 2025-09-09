package tenant

import (
	"errors"
	"fmt"
	"slices"
)

// Config represents tenant management configuration.
type Config struct {
	Filter   FilterConfig    `mapstructure:"filter"`
	Mappings []MappingConfig `mapstructure:"mappings"`
}

// FilterConfig represents tenant filtering configuration.
type FilterConfig struct {
	Strategy string       `mapstructure:"strategy"`
	Labels   LabelsConfig `mapstructure:"labels"`
}

// LabelsConfig represents label configuration for tenant filtering.
type LabelsConfig struct {
	Account      string `mapstructure:"account"`
	Project      string `mapstructure:"project"`
	UseProjectID bool   `mapstructure:"useProjectId"`
}

// MappingConfig represents tenant mapping configuration.
type MappingConfig struct {
	Groups      []string         `mapstructure:"groups"`
	VMTenants   []VMTenantConfig `mapstructure:"vmTenants"`
	Permissions []string         `mapstructure:"permissions"`
}

// VMTenantConfig represents VictoriaMetrics tenant configuration.
type VMTenantConfig struct {
	Account string `mapstructure:"account"`
	Project string `mapstructure:"project"`
}

// Validate validates tenant configuration.
func (c *Config) Validate() error {
	// Validate filter configuration
	if err := c.Filter.Validate(); err != nil {
		return fmt.Errorf("filter validation failed: %w", err)
	}

	// Validate mappings
	if len(c.Mappings) == 0 {
		return errors.New("at least one tenant mapping is required")
	}

	for i, mapping := range c.Mappings {
		if err := mapping.Validate(); err != nil {
			return fmt.Errorf("mapping %d validation failed: %w", i, err)
		}
	}

	return nil
}

// Validate validates filter configuration.
func (f *FilterConfig) Validate() error {
	validStrategies := []string{"orConditions", "andConditions"}
	if !slices.Contains(validStrategies, f.Strategy) {
		return fmt.Errorf("invalid filter strategy: %s (valid: %v)", f.Strategy, validStrategies)
	}

	// Validate labels configuration
	return f.Labels.Validate()
}

// Validate validates labels configuration.
func (l *LabelsConfig) Validate() error {
	if l.Account == "" {
		return errors.New("account label is required")
	}

	if l.Project == "" {
		return errors.New("project label is required")
	}

	return nil
}

// Validate validates mapping configuration.
func (m *MappingConfig) Validate() error {
	if len(m.Groups) == 0 {
		return errors.New("at least one group is required in mapping")
	}

	if len(m.VMTenants) == 0 {
		return errors.New("at least one VM tenant is required in mapping")
	}

	// Validate each VM tenant
	for i, tenant := range m.VMTenants {
		if err := tenant.Validate(); err != nil {
			return fmt.Errorf("VM tenant %d validation failed: %w", i, err)
		}
	}

	// Validate permissions if provided
	if len(m.Permissions) > 0 {
		validPermissions := []string{"read", "write"}
		for _, perm := range m.Permissions {
			if !slices.Contains(validPermissions, perm) {
				return fmt.Errorf("invalid permission: %s (valid: %v)", perm, validPermissions)
			}
		}
	}

	return nil
}

// Validate validates VM tenant configuration.
func (v *VMTenantConfig) Validate() error {
	if v.Account == "" {
		return errors.New("VM tenant account is required")
	}

	// Project can be empty for some use cases
	return nil
}
