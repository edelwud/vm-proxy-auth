package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// Default configuration values.
const (
	defaultRetryBackoffMilliseconds = 100
	defaultQueueMaxSize             = 1000
	defaultQueueTimeoutSeconds      = 5
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
	// Legacy single upstream configuration (for backward compatibility)
	URL     string        `mapstructure:"url"`
	Timeout time.Duration `mapstructure:"timeout"`
	Retry   RetrySettings `mapstructure:"retry"`

	// New multiple upstreams configuration
	Multiple MultipleUpstreamSettings `mapstructure:"multiple"`
}

type RetrySettings struct {
	MaxRetries int           `mapstructure:"maxRetries"`
	RetryDelay time.Duration `mapstructure:"retryDelay"`
}

// MultipleUpstreamSettings configures multiple upstream backends.
type MultipleUpstreamSettings struct {
	Enabled       bool                  `mapstructure:"enabled"`
	Backends      []BackendSettings     `mapstructure:"backends"`
	LoadBalancing LoadBalancingSettings `mapstructure:"loadBalancing"`
	HealthCheck   HealthCheckSettings   `mapstructure:"healthCheck"`
	Queue         QueueSettings         `mapstructure:"queue"`
	Timeout       time.Duration         `mapstructure:"timeout"`
	MaxRetries    int                   `mapstructure:"maxRetries"`
	RetryBackoff  time.Duration         `mapstructure:"retryBackoff"`
}

// BackendSettings represents a single backend configuration.
type BackendSettings struct {
	URL    string `mapstructure:"url"`
	Weight int    `mapstructure:"weight"`
}

// LoadBalancingSettings configures load balancing behavior.
type LoadBalancingSettings struct {
	Strategy string `mapstructure:"strategy"`
}

// HealthCheckSettings configures health check behavior.
type HealthCheckSettings struct {
	CheckInterval      time.Duration `mapstructure:"checkInterval"`
	Timeout            time.Duration `mapstructure:"timeout"`
	HealthyThreshold   int           `mapstructure:"healthyThreshold"`
	UnhealthyThreshold int           `mapstructure:"unhealthyThreshold"`
	HealthEndpoint     string        `mapstructure:"healthEndpoint"`
}

// QueueSettings configures request queuing behavior.
type QueueSettings struct {
	Enabled bool          `mapstructure:"enabled"`
	MaxSize int           `mapstructure:"maxSize"`
	Timeout time.Duration `mapstructure:"timeout"`
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
	const defaultHealthyThreshold = 2
	const defaultUnhealthyThreshold = 3
	const defaultQueueMaxSize = 1000
	v.SetDefault("upstream.retry.maxRetries", defaultMaxRetries)
	v.SetDefault("upstream.retry.retryDelay", "1s")

	// Multiple upstreams defaults
	v.SetDefault("upstream.multiple.enabled", false)
	v.SetDefault("upstream.multiple.loadBalancing.strategy", "round-robin")
	v.SetDefault("upstream.multiple.healthCheck.checkInterval", "30s")
	v.SetDefault("upstream.multiple.healthCheck.timeout", "10s")
	v.SetDefault("upstream.multiple.healthCheck.healthyThreshold", defaultHealthyThreshold)
	v.SetDefault("upstream.multiple.healthCheck.unhealthyThreshold", defaultUnhealthyThreshold)
	v.SetDefault("upstream.multiple.healthCheck.healthEndpoint", "/health")
	v.SetDefault("upstream.multiple.queue.enabled", true)
	v.SetDefault("upstream.multiple.queue.maxSize", defaultQueueMaxSize)
	v.SetDefault("upstream.multiple.queue.timeout", "5s")
	v.SetDefault("upstream.multiple.maxRetries", defaultMaxRetries)
	v.SetDefault("upstream.multiple.retryBackoff", "100ms")

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
	// Check upstream configuration: either single URL or multiple upstreams
	if config.Upstream.URL == "" && !config.IsMultipleUpstreamsEnabled() {
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

// IsMultipleUpstreamsEnabled returns true if multiple upstream configuration is enabled.
func (c *ViperConfig) IsMultipleUpstreamsEnabled() bool {
	return c.Upstream.Multiple.Enabled && len(c.Upstream.Multiple.Backends) > 0
}

// ValidateMultipleUpstreams validates the multiple upstream configuration.
func (c *ViperConfig) ValidateMultipleUpstreams() error {
	if !c.Upstream.Multiple.Enabled {
		return nil // Skip validation if not enabled
	}

	if len(c.Upstream.Multiple.Backends) == 0 {
		return errors.New("multiple upstreams enabled but no backends configured")
	}

	// Validate strategy
	strategy := c.Upstream.Multiple.LoadBalancing.Strategy
	validStrategies := []string{"round-robin", "weighted-round-robin", "least-connections"}
	isValidStrategy := slices.Contains(validStrategies, strategy)
	if !isValidStrategy {
		return fmt.Errorf("invalid load balancing strategy: %s. Valid strategies: %v", strategy, validStrategies)
	}

	// Validate backends
	for i, backend := range c.Upstream.Multiple.Backends {
		if backend.URL == "" {
			return fmt.Errorf("backend %d: URL is required", i)
		}
		if backend.Weight < 0 {
			return fmt.Errorf("backend %d: weight must be non-negative, got %d", i, backend.Weight)
		}
	}

	// Validate timeouts
	if c.Upstream.Multiple.Timeout <= 0 {
		return fmt.Errorf("multiple upstreams timeout must be positive, got %v", c.Upstream.Multiple.Timeout)
	}

	if c.Upstream.Multiple.HealthCheck.CheckInterval < 0 {
		return fmt.Errorf(
			"health check interval cannot be negative, got %v",
			c.Upstream.Multiple.HealthCheck.CheckInterval,
		)
	}

	return nil
}

// ToEnhancedServiceConfig converts the multiple upstream settings to EnhancedServiceConfig.
func (c *ViperConfig) ToEnhancedServiceConfig() (*EnhancedServiceConfig, error) {
	if !c.IsMultipleUpstreamsEnabled() {
		return nil, errors.New("multiple upstreams not enabled")
	}

	if err := c.ValidateMultipleUpstreams(); err != nil {
		return nil, fmt.Errorf("invalid multiple upstream configuration: %w", err)
	}

	config := &EnhancedServiceConfig{
		Backends: make([]BackendConfig, len(c.Upstream.Multiple.Backends)),
		LoadBalancing: LoadBalancingConfig{
			Strategy: domain.LoadBalancingStrategy(c.Upstream.Multiple.LoadBalancing.Strategy),
		},
		Timeout:        c.Upstream.Multiple.Timeout,
		MaxRetries:     c.Upstream.Multiple.MaxRetries,
		RetryBackoff:   c.Upstream.Multiple.RetryBackoff,
		EnableQueueing: c.Upstream.Multiple.Queue.Enabled,
	}

	// Convert backends
	for i, backend := range c.Upstream.Multiple.Backends {
		weight := backend.Weight
		if weight <= 0 {
			weight = 1 // Default weight
		}
		config.Backends[i] = BackendConfig{
			URL:    backend.URL,
			Weight: weight,
		}
	}

	// Convert health check settings
	config.HealthCheck = HealthCheckConfig{
		CheckInterval:      c.Upstream.Multiple.HealthCheck.CheckInterval,
		Timeout:            c.Upstream.Multiple.HealthCheck.Timeout,
		HealthyThreshold:   c.Upstream.Multiple.HealthCheck.HealthyThreshold,
		UnhealthyThreshold: c.Upstream.Multiple.HealthCheck.UnhealthyThreshold,
		HealthEndpoint:     c.Upstream.Multiple.HealthCheck.HealthEndpoint,
	}

	// Convert queue settings
	if c.Upstream.Multiple.Queue.Enabled {
		config.Queue = QueueConfig{
			MaxSize: c.Upstream.Multiple.Queue.MaxSize,
			Timeout: c.Upstream.Multiple.Queue.Timeout,
		}
	}

	// Set defaults
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}
	if config.RetryBackoff <= 0 {
		config.RetryBackoff = defaultRetryBackoffMilliseconds * time.Millisecond
	}
	if config.HealthCheck.HealthEndpoint == "" {
		config.HealthCheck.HealthEndpoint = "/health"
	}
	if config.Queue.MaxSize <= 0 && config.EnableQueueing {
		config.Queue.MaxSize = defaultQueueMaxSize
	}
	if config.Queue.Timeout <= 0 && config.EnableQueueing {
		config.Queue.Timeout = defaultQueueTimeoutSeconds * time.Second
	}

	return config, nil
}

// EnhancedServiceConfig represents the enhanced service configuration.
// This mirrors the struct in the proxy package to avoid circular dependencies.
type EnhancedServiceConfig struct {
	Backends       []BackendConfig     `yaml:"backends"`
	LoadBalancing  LoadBalancingConfig `yaml:"load_balancing"`
	HealthCheck    HealthCheckConfig   `yaml:"health_check"`
	Queue          QueueConfig         `yaml:"queue"`
	Timeout        time.Duration       `yaml:"timeout"`
	MaxRetries     int                 `yaml:"max_retries"`
	RetryBackoff   time.Duration       `yaml:"retry_backoff"`
	EnableQueueing bool                `yaml:"enable_queueing"`
}

// BackendConfig represents configuration for a single backend.
type BackendConfig struct {
	URL    string `yaml:"url"`
	Weight int    `yaml:"weight"`
}

// LoadBalancingConfig holds load balancing configuration.
type LoadBalancingConfig struct {
	Strategy domain.LoadBalancingStrategy `yaml:"strategy"`
}

// HealthCheckConfig holds health check configuration.
type HealthCheckConfig struct {
	CheckInterval      time.Duration `yaml:"check_interval"`
	Timeout            time.Duration `yaml:"timeout"`
	HealthyThreshold   int           `yaml:"healthy_threshold"`
	UnhealthyThreshold int           `yaml:"unhealthy_threshold"`
	HealthEndpoint     string        `yaml:"health_endpoint"`
}

// QueueConfig holds request queue configuration.
type QueueConfig struct {
	MaxSize int           `yaml:"max_size"`
	Timeout time.Duration `yaml:"timeout"`
}

// ToLegacyConfig converts ViperConfig to legacy Config for backward compatibility during migration.
// This will be implemented in the next commit when we integrate the new config system.
