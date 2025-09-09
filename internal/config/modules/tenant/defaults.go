package tenant

// Default tenant configuration values.
const (
	DefaultFilterStrategy = "orConditions"
	DefaultAccountLabel   = "vm_account_id"
	DefaultProjectLabel   = "vm_project_id"
	DefaultUseProjectID   = true
)

// GetDefaults returns default tenant configuration.
func GetDefaults() Config {
	return Config{
		Filter: FilterConfig{
			Strategy: DefaultFilterStrategy,
			Labels: LabelsConfig{
				Account:      DefaultAccountLabel,
				Project:      DefaultProjectLabel,
				UseProjectID: DefaultUseProjectID,
			},
		},
		Mappings: []MappingConfig{}, // Empty by default, must be configured
	}
}
