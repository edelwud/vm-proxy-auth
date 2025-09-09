package metadata

// Default metadata configuration values.
const (
	DefaultNodeID      = "vm-proxy-auth-1"
	DefaultRole        = "gateway"
	DefaultEnvironment = "development"
	DefaultRegion      = "local"
)

// GetDefaults returns default metadata configuration.
func GetDefaults() Config {
	return Config{
		NodeID:      DefaultNodeID,
		Role:        DefaultRole,
		Environment: DefaultEnvironment,
		Region:      DefaultRegion,
	}
}
