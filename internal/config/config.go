package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Upstream   UpstreamConfig   `yaml:"upstream"`
	Auth       AuthConfig       `yaml:"auth"`
	TenantMaps []TenantMapping  `yaml:"tenant_mappings"`
	Metrics    MetricsConfig    `yaml:"metrics"`
	Logging    LoggingConfig    `yaml:"logging"`
}

type ServerConfig struct {
	Address      string        `yaml:"address" default:"0.0.0.0:8080"`
	ReadTimeout  time.Duration `yaml:"read_timeout" default:"30s"`
	WriteTimeout time.Duration `yaml:"write_timeout" default:"30s"`
	IdleTimeout  time.Duration `yaml:"idle_timeout" default:"60s"`
}

type UpstreamConfig struct {
	URL           string        `yaml:"url"`
	Timeout       time.Duration `yaml:"timeout" default:"30s"`
	MaxRetries    int           `yaml:"max_retries" default:"3"`
	RetryDelay    time.Duration `yaml:"retry_delay" default:"1s"`
	TenantHeader  string        `yaml:"tenant_header" default:"X-Prometheus-Tenant"`
	TenantLabel   string        `yaml:"tenant_label" default:"tenant_id"`
}

type AuthConfig struct {
	Type              string        `yaml:"type" default:"jwt"`
	JWKSURL           string        `yaml:"jwks_url"`
	JWTSecret         string        `yaml:"jwt_secret"`
	JWTAlgorithm      string        `yaml:"jwt_algorithm" default:"RS256"`
	ValidateAudience  bool          `yaml:"validate_audience" default:"false"`
	ValidateIssuer    bool          `yaml:"validate_issuer" default:"false"`
	RequiredIssuer    string        `yaml:"required_issuer"`
	RequiredAudience  []string      `yaml:"required_audience"`
	TokenTTL          time.Duration `yaml:"token_ttl" default:"1h"`
	CacheTTL          time.Duration `yaml:"cache_ttl" default:"5m"`
	UserGroupsClaim   string        `yaml:"user_groups_claim" default:"groups"`
}

type TenantMapping struct {
	Groups   []string `yaml:"groups"`
	Tenants  []string `yaml:"tenants"`
	ReadOnly bool     `yaml:"read_only" default:"false"`
}

type MetricsConfig struct {
	Enabled bool   `yaml:"enabled" default:"true"`
	Path    string `yaml:"path" default:"/metrics"`
}

type LoggingConfig struct {
	Level  string `yaml:"level" default:"info"`
	Format string `yaml:"format" default:"json"`
}

func Load(path string) (*Config, error) {
	config := &Config{}
	
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	if err := loadFromEnv(config); err != nil {
		return nil, fmt.Errorf("failed to load environment variables: %w", err)
	}

	if err := validate(config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	setDefaults(config)
	
	return config, nil
}

func loadFromEnv(config *Config) error {
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
	
	return nil
}

func validate(config *Config) error {
	if config.Upstream.URL == "" {
		return fmt.Errorf("upstream.url is required")
	}
	
	if config.Auth.Type == "jwt" && config.Auth.JWKSURL == "" && config.Auth.JWTSecret == "" {
		return fmt.Errorf("either auth.jwks_url or auth.jwt_secret must be provided for JWT authentication")
	}
	
	return nil
}

func setDefaults(config *Config) {
	if config.Server.Address == "" {
		config.Server.Address = "0.0.0.0:8080"
	}
	
	if config.Server.ReadTimeout == 0 {
		config.Server.ReadTimeout = 30 * time.Second
	}
	
	if config.Server.WriteTimeout == 0 {
		config.Server.WriteTimeout = 30 * time.Second
	}
	
	if config.Server.IdleTimeout == 0 {
		config.Server.IdleTimeout = 60 * time.Second
	}
	
	if config.Upstream.Timeout == 0 {
		config.Upstream.Timeout = 30 * time.Second
	}
	
	if config.Upstream.MaxRetries == 0 {
		config.Upstream.MaxRetries = 3
	}
	
	if config.Upstream.RetryDelay == 0 {
		config.Upstream.RetryDelay = time.Second
	}
	
	if config.Upstream.TenantHeader == "" {
		config.Upstream.TenantHeader = "X-Prometheus-Tenant"
	}
	
	if config.Upstream.TenantLabel == "" {
		config.Upstream.TenantLabel = "tenant_id"
	}
	
	if config.Auth.JWTAlgorithm == "" {
		config.Auth.JWTAlgorithm = "RS256"
	}
	
	if config.Auth.TokenTTL == 0 {
		config.Auth.TokenTTL = time.Hour
	}
	
	if config.Auth.CacheTTL == 0 {
		config.Auth.CacheTTL = 5 * time.Minute
	}
	
	if config.Auth.UserGroupsClaim == "" {
		config.Auth.UserGroupsClaim = "groups"
	}
	
	if config.Metrics.Path == "" {
		config.Metrics.Path = "/metrics"
	}
	
	if config.Logging.Level == "" {
		config.Logging.Level = "info"
	}
	
	if config.Logging.Format == "" {
		config.Logging.Format = "json"
	}
}