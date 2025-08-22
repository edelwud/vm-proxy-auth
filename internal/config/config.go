package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

var ()

type Config struct {
	Server     ServerConfig    `yaml:"server"`
	Upstream   UpstreamConfig  `yaml:"upstream"`
	Auth       AuthConfig      `yaml:"auth"`
	TenantMaps []TenantMapping `yaml:"tenant_mappings"`
	Metrics    MetricsConfig   `yaml:"metrics"`
	Logging    LoggingConfig   `yaml:"logging"`
}

type ServerConfig struct {
	Address      string        `yaml:"address"       default:"0.0.0.0:8080"`
	ReadTimeout  time.Duration `yaml:"read_timeout"  default:"30s"`
	WriteTimeout time.Duration `yaml:"write_timeout" default:"30s"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"  default:"60s"`
}

type UpstreamConfig struct {
	URL          string        `yaml:"url"`
	Timeout      time.Duration `yaml:"timeout"        default:"30s"`
	MaxRetries   int           `yaml:"max_retries"    default:"3"`
	RetryDelay   time.Duration `yaml:"retry_delay"    default:"1s"`
	TenantHeader string        `yaml:"tenant_header"  default:"X-Prometheus-Tenant"`
	// VictoriaMetrics multi-tenancy labels
	TenantLabel  string `yaml:"tenant_label"   default:"vm_account_id"`
	ProjectLabel string `yaml:"project_label"  default:"vm_project_id"`
	UseProjectID bool   `yaml:"use_project_id" default:"false"`
	// Tenant filtering strategy configuration
	TenantFilter TenantFilterConfig `yaml:"tenant_filter"`
}

type TenantFilterConfig struct {
	Strategy string `yaml:"strategy" default:"regex"`
}

type AuthConfig struct {
	Type             string        `yaml:"type"              default:"jwt"`
	JWKSURL          string        `yaml:"jwks_url"`
	JWTSecret        string        `yaml:"jwt_secret"`
	JWTAlgorithm     string        `yaml:"jwt_algorithm"     default:"RS256"`
	ValidateAudience bool          `yaml:"validate_audience" default:"false"`
	ValidateIssuer   bool          `yaml:"validate_issuer"   default:"false"`
	RequiredIssuer   string        `yaml:"required_issuer"`
	RequiredAudience []string      `yaml:"required_audience"`
	TokenTTL         time.Duration `yaml:"token_ttl"         default:"1h"`
	CacheTTL         time.Duration `yaml:"cache_ttl"         default:"5m"`
	UserGroupsClaim  string        `yaml:"user_groups_claim" default:"groups"`
}

type TenantMapping struct {
	Groups   []string `yaml:"groups"`
	Tenants  []string `yaml:"tenants"`
	ReadOnly bool     `yaml:"read_only"            default:"false"`
	// VictoriaMetrics specific
	VMTenants []VMTenantMapping `yaml:"vm_tenants,omitempty"`
}

type VMTenantMapping struct {
	AccountID string `yaml:"account_id"`
	ProjectID string `yaml:"project_id,omitempty"` // Optional
}

type MetricsConfig struct {
	Enabled bool   `yaml:"enabled" default:"true"`
	Path    string `yaml:"path"    default:"/metrics"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"  default:"info"`
	Format string `yaml:"format" default:"json"`
}

func Load(path string) (*Config, error) {
	config := &Config{}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		if yamlErr := yaml.Unmarshal(data, config); yamlErr != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", yamlErr)
		}
	}

	loadFromEnv(config)

	if err := validate(config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	setDefaults(config)

	return config, nil
}

func loadFromEnv(config *Config) {
	if addr := os.Getenv("SERVER_ADDRESS"); addr != "" {
		config.Server.Address = addr
	}

	if url := os.Getenv("UPSTREAM_URL"); url != "" {
		config.Upstream.URL = url
	}

	if jwksURL := os.Getenv("AUTH_JWKS_URL"); jwksURL != "" {
		config.Auth.JWKSURL = jwksURL
	}

	if secret := os.Getenv("AUTH_JWT_SECRET"); secret != "" {
		config.Auth.JWTSecret = secret
	}

	if issuer := os.Getenv("AUTH_REQUIRED_ISSUER"); issuer != "" {
		config.Auth.RequiredIssuer = issuer
	}

	if claim := os.Getenv("AUTH_USER_GROUPS_CLAIM"); claim != "" {
		config.Auth.UserGroupsClaim = claim
	}

	if level := os.Getenv("LOG_LEVEL"); level != "" {
		config.Logging.Level = level
	}
}

func validate(config *Config) error {
	if config.Upstream.URL == "" {
		return domain.ErrUpstreamURLRequired
	}

	if config.Auth.Type == "jwt" && config.Auth.JWKSURL == "" && config.Auth.JWTSecret == "" {
		return domain.ErrAuthConfigRequired
	}

	return nil
}

func setDefaults(config *Config) {
	setServerDefaults(&config.Server)
	setUpstreamDefaults(&config.Upstream)
	setAuthDefaults(&config.Auth)
	setMetricsDefaults(&config.Metrics)
	setLoggingDefaults(&config.Logging)
}

func setServerDefaults(server *ServerConfig) {
	if server.Address == "" {
		server.Address = "0.0.0.0:8080"
	}
	if server.ReadTimeout == 0 {
		server.ReadTimeout = domain.DefaultReadTimeout
	}
	if server.WriteTimeout == 0 {
		server.WriteTimeout = domain.DefaultWriteTimeout
	}
	if server.IdleTimeout == 0 {
		server.IdleTimeout = domain.DefaultIdleTimeout
	}
}

func setUpstreamDefaults(upstream *UpstreamConfig) {
	if upstream.Timeout == 0 {
		upstream.Timeout = domain.DefaultTimeout
	}
	if upstream.MaxRetries == 0 {
		upstream.MaxRetries = domain.DefaultMaxRetries
	}
	if upstream.RetryDelay == 0 {
		upstream.RetryDelay = domain.DefaultRetryDelay
	}
	if upstream.TenantHeader == "" {
		upstream.TenantHeader = "X-Prometheus-Tenant"
	}
	if upstream.TenantLabel == "" {
		upstream.TenantLabel = "vm_account_id"
	}
	if upstream.ProjectLabel == "" {
		upstream.ProjectLabel = "vm_project_id"
	}
	if upstream.TenantFilter.Strategy == "" {
		upstream.TenantFilter.Strategy = "or_conditions" // Default to secure OR strategy
	}
}

func setAuthDefaults(auth *AuthConfig) {
	if auth.JWTAlgorithm == "" {
		auth.JWTAlgorithm = "RS256"
	}
	if auth.TokenTTL == 0 {
		auth.TokenTTL = domain.DefaultTokenTTL
	}
	if auth.CacheTTL == 0 {
		auth.CacheTTL = domain.DefaultCacheTTL
	}
	if auth.UserGroupsClaim == "" {
		auth.UserGroupsClaim = "groups"
	}
}

func setMetricsDefaults(metrics *MetricsConfig) {
	if metrics.Path == "" {
		metrics.Path = "/metrics"
	}
}

func setLoggingDefaults(logging *LoggingConfig) {
	if logging.Level == "" {
		logging.Level = "info"
	}
	if logging.Format == "" {
		logging.Format = "json"
	}
}
