package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// ViperConfig represents the new configuration structure with camelCase naming.
type ViperConfig struct {
	Server        ServerSettings       `mapstructure:"server"`
	Upstream      UpstreamSettings     `mapstructure:"upstream"`
	Auth          AuthSettings         `mapstructure:"auth"`
	TenantMapping []TenantMap          `mapstructure:"tenantMapping"`
	TenantFilter  TenantFilterSettings `mapstructure:"tenantFilter"`
	Metrics       MetricsSettings      `mapstructure:"metrics"`
	Logging       LoggingSettings      `mapstructure:"logging"`
}

type ServerSettings struct {
	Address  string         `mapstructure:"address"`
	Timeouts ServerTimeouts `mapstructure:"timeouts"`
}

type ServerTimeouts struct {
	ReadTimeout  time.Duration `mapstructure:"readTimeout"`
	WriteTimeout time.Duration `mapstructure:"writeTimeout"`
	IdleTimeout  time.Duration `mapstructure:"idleTimeout"`
}

type UpstreamSettings struct {
	URL     string        `mapstructure:"url"`
	Timeout time.Duration `mapstructure:"timeout"`
	Retry   RetrySettings `mapstructure:"retry"`
}

type RetrySettings struct {
	MaxRetries int           `mapstructure:"maxRetries"`
	RetryDelay time.Duration `mapstructure:"retryDelay"`
}

type AuthSettings struct {
	JWT JWTSettings `mapstructure:"jwt"`
}

type JWTSettings struct {
	Algorithm  string                `mapstructure:"algorithm"`
	JwksURL    string                `mapstructure:"jwksUrl"`
	Secret     string                `mapstructure:"secret"`
	Validation JWTValidationSettings `mapstructure:"validation"`
	Claims     JWTClaimsSettings     `mapstructure:"claims"`
	TokenTTL   time.Duration         `mapstructure:"tokenTtl"`
	CacheTTL   time.Duration         `mapstructure:"cacheTtl"`
}

type JWTValidationSettings struct {
	ValidateAudience bool     `mapstructure:"validateAudience"`
	ValidateIssuer   bool     `mapstructure:"validateIssuer"`
	RequiredIssuer   string   `mapstructure:"requiredIssuer"`
	RequiredAudience []string `mapstructure:"requiredAudience"`
}

type JWTClaimsSettings struct {
	UserGroupsClaim string `mapstructure:"userGroupsClaim"`
}

type TenantMap struct {
	Groups    []string       `mapstructure:"groups"`
	VMTenants []VMTenantInfo `mapstructure:"vmTenants"`
	ReadOnly  bool           `mapstructure:"readOnly"`
}

type VMTenantInfo struct {
	AccountID string `mapstructure:"accountId"`
	ProjectID string `mapstructure:"projectId"`
}

type TenantFilterSettings struct {
	Strategy string             `mapstructure:"strategy"`
	Labels   TenantFilterLabels `mapstructure:"labels"`
}

type TenantFilterLabels struct {
	AccountLabel string `mapstructure:"accountLabel"`
	ProjectLabel string `mapstructure:"projectLabel"`
	UseProjectID bool   `mapstructure:"useProjectId"`
}

type MetricsSettings struct {
	Enabled bool   `mapstructure:"enabled"`
	Path    string `mapstructure:"path"`
}

type LoggingSettings struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// LoadViperConfig loads configuration using Viper with proper defaults and validation.
func LoadViperConfig(configPath string) (*ViperConfig, error) {
	v := viper.New()

	// Set configuration file path and format
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./examples")
	v.AddConfigPath("/etc/vm-proxy-auth")
	v.AddConfigPath("$HOME/.vm-proxy-auth")

	if configPath != "" {
		v.SetConfigFile(configPath)
	}

	// Setup environment variable handling
	v.SetEnvPrefix("VM_PROXY_AUTH")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Bind specific environment variables for nested structures
	_ = v.BindEnv("server.address", "VM_PROXY_AUTH_SERVER_ADDRESS")
	_ = v.BindEnv("upstream.url", "VM_PROXY_AUTH_UPSTREAM_URL")
	_ = v.BindEnv("auth.jwt.algorithm", "VM_PROXY_AUTH_AUTH_JWT_ALGORITHM")
	_ = v.BindEnv("auth.jwt.secret", "VM_PROXY_AUTH_AUTH_JWT_SECRET")
	_ = v.BindEnv("auth.jwt.jwksUrl", "VM_PROXY_AUTH_AUTH_JWT_JWKSURL")
	_ = v.BindEnv("tenantFilter.strategy", "VM_PROXY_AUTH_TENANTFILTER_STRATEGY")
	_ = v.BindEnv("logging.level", "VM_PROXY_AUTH_LOGGING_LEVEL")

	// Set defaults
	setViperDefaults(v)

	// Read configuration file
	if err := v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFoundError) {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		// Config file not found is OK, we'll use defaults and env vars
	}

	// Unmarshal into struct
	var config ViperConfig
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if err := validateViperConfig(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

// setViperDefaults sets all default values.
func setViperDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.address", "0.0.0.0:8080")
	v.SetDefault("server.timeouts.readTimeout", "30s")
	v.SetDefault("server.timeouts.writeTimeout", "30s")
	v.SetDefault("server.timeouts.idleTimeout", "60s")

	// Upstream defaults
	v.SetDefault("upstream.timeout", "30s")
	const defaultMaxRetries = 3
	v.SetDefault("upstream.retry.maxRetries", defaultMaxRetries)
	v.SetDefault("upstream.retry.retryDelay", "1s")

	// Auth defaults
	v.SetDefault("auth.jwt.algorithm", "RS256")
	v.SetDefault("auth.jwt.validation.validateAudience", false)
	v.SetDefault("auth.jwt.validation.validateIssuer", false)
	v.SetDefault("auth.jwt.claims.userGroupsClaim", "groups")
	v.SetDefault("auth.jwt.tokenTtl", "1h")
	v.SetDefault("auth.jwt.cacheTtl", "5m")

	// Tenant filter defaults
	v.SetDefault("tenantFilter.strategy", "orConditions")
	v.SetDefault("tenantFilter.labels.accountLabel", "vm_account_id")
	v.SetDefault("tenantFilter.labels.projectLabel", "vm_project_id")
	v.SetDefault("tenantFilter.labels.useProjectId", true)

	// Metrics defaults
	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.path", "/metrics")

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
}

// validateViperConfig validates the loaded configuration.
func validateViperConfig(config *ViperConfig) error {
	// Required fields validation
	if config.Upstream.URL == "" {
		return domain.ErrUpstreamURLRequired
	}

	// JWT configuration validation
	if config.Auth.JWT.JwksURL == "" && config.Auth.JWT.Secret == "" {
		return domain.ErrAuthConfigRequired
	}

	// JWT algorithm validation
	const rs256Algorithm = "RS256"
	const hs256Algorithm = "HS256"
	switch config.Auth.JWT.Algorithm {
	case rs256Algorithm, hs256Algorithm:
		// Valid algorithms
	default:
		return fmt.Errorf("unsupported JWT algorithm: %s (supported: RS256, HS256)", config.Auth.JWT.Algorithm)
	}

	// RS256 requires JWKS URL, HS256 requires secret
	if config.Auth.JWT.Algorithm == rs256Algorithm && config.Auth.JWT.JwksURL == "" {
		return errors.New("RS256 algorithm requires jwksUrl")
	}
	if config.Auth.JWT.Algorithm == hs256Algorithm && config.Auth.JWT.Secret == "" {
		return errors.New("HS256 algorithm requires secret")
	}

	// Tenant filter strategy validation
	switch config.TenantFilter.Strategy {
	case "orConditions", "regex":
		// Valid strategies
	default:
		return fmt.Errorf(
			"unsupported tenant filter strategy: %s (supported: orConditions, regex)",
			config.TenantFilter.Strategy,
		)
	}

	// Logging level validation
	switch strings.ToLower(config.Logging.Level) {
	case "debug", "info", "warn", "error", "fatal":
		// Valid levels
	default:
		return fmt.Errorf(
			"unsupported logging level: %s (supported: debug, info, warn, error, fatal)",
			config.Logging.Level,
		)
	}

	// Logging format validation
	switch strings.ToLower(config.Logging.Format) {
	case "json", "text", "logfmt", "pretty", "console":
		// Valid formats
	default:
		return fmt.Errorf(
			"unsupported logging format: %s (supported: json, text, logfmt, pretty, console)",
			config.Logging.Format,
		)
	}

	return nil
}

// ToLegacyConfig converts ViperConfig to legacy Config for backward compatibility during migration.
// This will be implemented in the next commit when we integrate the new config system.
