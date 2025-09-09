package logging

// Default logging configuration values.
const (
	DefaultLevel  = "info"
	DefaultFormat = "json"
)

// GetDefaults returns default logging configuration.
func GetDefaults() Config {
	return Config{
		Level:  DefaultLevel,
		Format: DefaultFormat,
	}
}
