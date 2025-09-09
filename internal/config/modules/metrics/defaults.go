package metrics

// Default metrics configuration values.
const (
	DefaultEnabled = true
	DefaultPath    = "/metrics"
)

// GetDefaults returns default metrics configuration.
func GetDefaults() Config {
	return Config{
		Enabled: DefaultEnabled,
		Path:    DefaultPath,
	}
}
