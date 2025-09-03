package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/health"
	"github.com/edelwud/vm-proxy-auth/internal/services/proxy"
)

// ViperConfig represents the simplified configuration structure without nesting.
type ViperConfig struct {
	Server        ServerSettings        `mapstructure:"server"`
	Backends      []BackendSettings     `mapstructure:"backends"`
	LoadBalancing LoadBalancingSettings `mapstructure:"loadBalancing"`
	HealthCheck   HealthCheckSettings   `mapstructure:"healthCheck"`
	Queue         QueueSettings         `mapstructure:"queue"`
	Timeout       time.Duration         `mapstructure:"timeout"`
	MaxRetries    int                   `mapstructure:"maxRetries"`
	RetryBackoff  time.Duration         `mapstructure:"retryBackoff"`
	StateStorage  StateStorageSettings  `mapstructure:"stateStorage"`
	Auth          AuthSettings          `mapstructure:"auth"`
	TenantMapping []TenantMap           `mapstructure:"tenantMapping"`
	TenantFilter  TenantFilterSettings  `mapstructure:"tenantFilter"`
	Metrics       MetricsSettings       `mapstructure:"metrics"`
	Logging       LoggingSettings       `mapstructure:"logging"`
	Memberlist    MemberlistSettings    `mapstructure:"memberlist"`
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

// StateStorageSettings configures distributed state storage.
type StateStorageSettings struct {
	Type  string        `mapstructure:"type"` // local, redis, raft
	Redis RedisSettings `mapstructure:"redis"`
	Raft  RaftSettings  `mapstructure:"raft"`
}

// RedisSettings configures Redis state storage.
type RedisSettings struct {
	Address         string        `mapstructure:"address"`
	Password        string        `mapstructure:"password"`
	Database        int           `mapstructure:"database"`
	KeyPrefix       string        `mapstructure:"keyPrefix"`
	ConnectTimeout  time.Duration `mapstructure:"connectTimeout"`
	ReadTimeout     time.Duration `mapstructure:"readTimeout"`
	WriteTimeout    time.Duration `mapstructure:"writeTimeout"`
	PoolSize        int           `mapstructure:"poolSize"`
	MinIdleConns    int           `mapstructure:"minIdleConns"`
	MaxRetries      int           `mapstructure:"maxRetries"`
	MinRetryBackoff time.Duration `mapstructure:"minRetryBackoff"`
	MaxRetryBackoff time.Duration `mapstructure:"maxRetryBackoff"`
}

// RaftSettings configures Raft consensus state storage.
type RaftSettings struct {
	NodeID      string   `mapstructure:"nodeId"`
	BindAddress string   `mapstructure:"bindAddress"`
	Peers       []string `mapstructure:"peers"`
	DataDir     string   `mapstructure:"dataDir"`
}

// MemberlistSettings configures memberlist for cluster membership.
type MemberlistSettings struct {
	BindAddress      string            `mapstructure:"bindAddress"`
	BindPort         int               `mapstructure:"bindPort"`
	AdvertiseAddress string            `mapstructure:"advertiseAddress"`
	AdvertisePort    int               `mapstructure:"advertisePort"`
	JoinNodes        []string          `mapstructure:"joinNodes"`
	GossipInterval   time.Duration     `mapstructure:"gossipInterval"`
	GossipNodes      int               `mapstructure:"gossipNodes"`
	ProbeInterval    time.Duration     `mapstructure:"probeInterval"`
	ProbeTimeout     time.Duration     `mapstructure:"probeTimeout"`
	EncryptionKey    string            `mapstructure:"encryptionKey"`
	Metadata         map[string]string `mapstructure:"metadata"`
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
	v.AddConfigPath("/etc/vm-proxy-auth")
	v.AddConfigPath("$HOME/.vm-proxy-auth")

	if configPath != "" {
		v.SetConfigFile(configPath)
	}

	// Setup environment variable handling
	v.SetEnvPrefix("VM_PROXY_AUTH")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Bind specific environment variables
	_ = v.BindEnv("server.address", "VM_PROXY_AUTH_SERVER_ADDRESS")
	_ = v.BindEnv("backends", "VM_PROXY_AUTH_BACKENDS")
	_ = v.BindEnv("loadBalancing.strategy", "VM_PROXY_AUTH_LOADBALANCING_STRATEGY")
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
	v.SetDefault("server.timeouts.readTimeout", domain.DefaultReadTimeout.String())
	v.SetDefault("server.timeouts.writeTimeout", domain.DefaultWriteTimeout.String())
	v.SetDefault("server.timeouts.idleTimeout", domain.DefaultIdleTimeout.String())

	// Upstream defaults (flattened)
	v.SetDefault("timeout", domain.DefaultTimeout.String())
	v.SetDefault("maxRetries", domain.DefaultProxyMaxRetries)
	v.SetDefault("retryBackoff", (domain.DefaultRetryBackoffMs * time.Millisecond).String())

	// Load balancing defaults
	v.SetDefault("loadBalancing.strategy", string(domain.LoadBalancingStrategyRoundRobin))

	// Health check defaults
	v.SetDefault("healthCheck.checkInterval", "30s")
	v.SetDefault("healthCheck.timeout", "10s")
	v.SetDefault("healthCheck.healthyThreshold", domain.DefaultHealthyThreshold)
	v.SetDefault("healthCheck.unhealthyThreshold", domain.DefaultUnhealthyThreshold)
	v.SetDefault("healthCheck.healthEndpoint", "/health")

	// Queue defaults
	v.SetDefault("queue.enabled", true)
	v.SetDefault("queue.maxSize", domain.DefaultQueueMaxSize)
	v.SetDefault("queue.timeout", (domain.DefaultQueueTimeoutSeconds * time.Second).String())

	// StateStorage defaults
	v.SetDefault("stateStorage.type", string(domain.StateStorageTypeLocal))
	v.SetDefault("stateStorage.redis.database", domain.DefaultRedisDatabase)
	v.SetDefault("stateStorage.redis.keyPrefix", domain.DefaultRedisKeyPrefix)
	v.SetDefault("stateStorage.redis.connectTimeout", domain.DefaultRedisConnectTimeout.String())
	v.SetDefault("stateStorage.redis.readTimeout", domain.DefaultRedisReadTimeout.String())
	v.SetDefault("stateStorage.redis.writeTimeout", domain.DefaultRedisWriteTimeout.String())
	v.SetDefault("stateStorage.redis.poolSize", domain.DefaultRedisPoolSize)
	v.SetDefault("stateStorage.redis.minIdleConns", domain.DefaultRedisMinIdleConns)
	v.SetDefault("stateStorage.redis.maxRetries", domain.DefaultRedisMaxRetries)
	v.SetDefault("stateStorage.redis.minRetryBackoff", domain.DefaultRedisMinRetryBackoff.String())
	v.SetDefault("stateStorage.redis.maxRetryBackoff", domain.DefaultRedisMaxRetryBackoff.String())

	// Raft defaults
	v.SetDefault("stateStorage.raft.bindAddress", "127.0.0.1:9000")

	// Memberlist defaults
	v.SetDefault("memberlist.bindAddress", "0.0.0.0")
	v.SetDefault("memberlist.bindPort", domain.DefaultMemberlistBindPort)
	v.SetDefault("memberlist.advertisePort", domain.DefaultMemberlistBindPort)
	v.SetDefault("memberlist.gossipInterval", domain.DefaultMemberlistGossipInterval.String())
	v.SetDefault("memberlist.gossipNodes", domain.DefaultMemberlistGossipNodes)
	v.SetDefault("memberlist.probeInterval", domain.DefaultMemberlistProbeInterval.String())
	v.SetDefault("memberlist.probeTimeout", domain.DefaultMemberlistProbeTimeout.String())

	// Auth defaults
	v.SetDefault("auth.jwt.algorithm", string(domain.JWTAlgorithmRS256))
	v.SetDefault("auth.jwt.validation.validateAudience", false)
	v.SetDefault("auth.jwt.validation.validateIssuer", false)
	v.SetDefault("auth.jwt.claims.userGroupsClaim", "groups")
	v.SetDefault("auth.jwt.tokenTtl", domain.DefaultTokenTTL.String())
	v.SetDefault("auth.jwt.cacheTtl", domain.DefaultCacheTTL.String())

	// Tenant filter defaults
	v.SetDefault("tenantFilter.strategy", string(domain.TenantFilterStrategyOrConditions))
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
	// Validate backends
	if err := config.ValidateBackends(); err != nil {
		return err
	}

	// JWT configuration validation
	if config.Auth.JWT.JwksURL == "" && config.Auth.JWT.Secret == "" {
		return domain.ErrAuthConfigRequired
	}

	// JWT algorithm validation
	switch config.Auth.JWT.Algorithm {
	case string(domain.JWTAlgorithmRS256), string(domain.JWTAlgorithmHS256):
		// Valid algorithms
	default:
		return fmt.Errorf("unsupported JWT algorithm: %s (supported: RS256, HS256)", config.Auth.JWT.Algorithm)
	}

	// RS256 requires JWKS URL, HS256 requires secret
	if config.Auth.JWT.Algorithm == string(domain.JWTAlgorithmRS256) && config.Auth.JWT.JwksURL == "" {
		return errors.New("rS256 algorithm requires jwksUrl")
	}
	if config.Auth.JWT.Algorithm == string(domain.JWTAlgorithmHS256) && config.Auth.JWT.Secret == "" {
		return errors.New("hS256 algorithm requires secret")
	}

	// Tenant filter strategy validation
	strategyType := domain.TenantFilterStrategy(config.TenantFilter.Strategy)
	if !strategyType.IsValid() {
		return fmt.Errorf(
			"unsupported tenant filter strategy: %s (supported: %s, %s)",
			config.TenantFilter.Strategy,
			domain.TenantFilterStrategyOrConditions,
			domain.TenantFilterStrategyAndConditions,
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

// ValidateBackends validates the backend configuration.
func (c *ViperConfig) ValidateBackends() error {
	if len(c.Backends) == 0 {
		return errors.New("at least one backend is required")
	}

	// Validate strategy using domain constants
	strategy := domain.LoadBalancingStrategy(c.LoadBalancing.Strategy)
	if !strategy.IsValid() {
		return fmt.Errorf(
			"invalid load balancing strategy: %s. Valid strategies: %s, %s, %s",
			c.LoadBalancing.Strategy,
			domain.LoadBalancingStrategyRoundRobin,
			domain.LoadBalancingStrategyWeighted,
			domain.LoadBalancingStrategyLeastConnection,
		)
	}

	// Validate backends
	for i, backend := range c.Backends {
		if backend.URL == "" {
			return fmt.Errorf("backend %d: URL is required", i)
		}
		if backend.Weight < 0 {
			return fmt.Errorf("backend %d: weight must be non-negative, got %d", i, backend.Weight)
		}
	}

	// Validate timeouts
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive, got %v", c.Timeout)
	}

	if c.HealthCheck.CheckInterval < 0 {
		return fmt.Errorf("health check interval cannot be negative, got %v", c.HealthCheck.CheckInterval)
	}

	return nil
}

// ValidateStateStorage validates the state storage configuration.
func (c *ViperConfig) ValidateStateStorage() error {
	stateType := c.StateStorage.Type
	validTypes := []string{
		string(domain.StateStorageTypeLocal),
		string(domain.StateStorageTypeRedis),
		string(domain.StateStorageTypeRaft),
	}

	if !slices.Contains(validTypes, stateType) {
		return fmt.Errorf("invalid state storage type: %s. Valid types: %v", stateType, validTypes)
	}

	// Validate Redis configuration if Redis is selected
	if stateType == string(domain.StateStorageTypeRedis) {
		return c.validateRedisConfig()
	}

	// Validate Raft configuration if Raft is selected
	if stateType == string(domain.StateStorageTypeRaft) {
		if err := c.validateRaftConfig(); err != nil {
			return err
		}
	}

	return nil
}

// validateRedisConfig validates Redis-specific configuration.
func (c *ViperConfig) validateRedisConfig() error {
	redis := c.StateStorage.Redis
	if redis.Address == "" {
		return errors.New("redis address is required when using Redis state storage")
	}

	if redis.Database < 0 {
		return fmt.Errorf("redis database must be non-negative, got %d", redis.Database)
	}

	if redis.PoolSize <= 0 {
		return fmt.Errorf("redis pool size must be positive, got %d", redis.PoolSize)
	}

	if redis.MinIdleConns < 0 {
		return fmt.Errorf("redis min idle connections cannot be negative, got %d", redis.MinIdleConns)
	}

	if redis.MinIdleConns > redis.PoolSize {
		return fmt.Errorf("redis min idle connections (%d) cannot exceed pool size (%d)",
			redis.MinIdleConns, redis.PoolSize)
	}

	if redis.ConnectTimeout <= 0 {
		return fmt.Errorf("redis connect timeout must be positive, got %v", redis.ConnectTimeout)
	}

	if redis.ReadTimeout <= 0 {
		return fmt.Errorf("redis read timeout must be positive, got %v", redis.ReadTimeout)
	}

	if redis.WriteTimeout <= 0 {
		return fmt.Errorf("redis write timeout must be positive, got %v", redis.WriteTimeout)
	}

	return nil
}

// validateRaftConfig validates Raft-specific configuration.
func (c *ViperConfig) validateRaftConfig() error {
	raft := c.StateStorage.Raft
	if raft.NodeID == "" {
		return errors.New("raft node ID is required when using Raft state storage")
	}

	// Peers can be empty for single-node deployments (memberlist handles dynamic peers)

	if raft.BindAddress == "" {
		return errors.New("raft bind address is required when using Raft state storage")
	}

	if raft.DataDir == "" {
		return errors.New("raft data directory is required when using Raft state storage")
	}

	return nil
}

// ToStateStorageConfig converts the state storage settings to RedisStorageConfig.
func (c *ViperConfig) ToStateStorageConfig() (interface{}, string, error) {
	if err := c.ValidateStateStorage(); err != nil {
		return nil, "", fmt.Errorf("state storage validation failed: %w", err)
	}

	storageType := c.StateStorage.Type

	switch storageType {
	case string(domain.StateStorageTypeLocal):
		// Local storage doesn't need additional configuration
		return nil, string(domain.StateStorageTypeLocal), nil

	case string(domain.StateStorageTypeRedis):
		redis := c.StateStorage.Redis

		// Apply defaults if not set
		if redis.KeyPrefix == "" {
			redis.KeyPrefix = domain.DefaultRedisKeyPrefix
		}
		if redis.ConnectTimeout == 0 {
			redis.ConnectTimeout = domain.DefaultRedisConnectTimeout
		}
		if redis.ReadTimeout == 0 {
			redis.ReadTimeout = domain.DefaultRedisReadTimeout
		}
		if redis.WriteTimeout == 0 {
			redis.WriteTimeout = domain.DefaultRedisWriteTimeout
		}
		if redis.PoolSize == 0 {
			redis.PoolSize = domain.DefaultRedisPoolSize
		}
		if redis.MinIdleConns == 0 {
			redis.MinIdleConns = domain.DefaultRedisMinIdleConns
		}
		if redis.MaxRetries == 0 {
			redis.MaxRetries = domain.DefaultRedisMaxRetries
		}
		if redis.MinRetryBackoff == 0 {
			redis.MinRetryBackoff = domain.DefaultRedisMinRetryBackoff
		}
		if redis.MaxRetryBackoff == 0 {
			redis.MaxRetryBackoff = domain.DefaultRedisMaxRetryBackoff
		}

		return redis, string(domain.StateStorageTypeRedis), nil

	case string(domain.StateStorageTypeRaft):
		// Raft storage configuration
		return c.StateStorage.Raft, string(domain.StateStorageTypeRaft), nil

	default:
		return nil, "", fmt.Errorf("unsupported state storage type: %s", storageType)
	}
}

// ToProxyServiceConfig converts ViperConfig to proxy service configuration.
func (c *ViperConfig) ToProxyServiceConfig() (proxy.EnhancedServiceConfig, error) {
	// Create proxy configuration directly from config
	enhancedConfig := proxy.EnhancedServiceConfig{
		Backends:      make([]proxy.BackendConfig, len(c.Backends)),
		LoadBalancing: proxy.LoadBalancingConfig{Strategy: domain.LoadBalancingStrategy(c.LoadBalancing.Strategy)},
		HealthCheck: health.CheckerConfig{
			CheckInterval:      c.HealthCheck.CheckInterval,
			Timeout:            c.HealthCheck.Timeout,
			HealthyThreshold:   c.HealthCheck.HealthyThreshold,
			UnhealthyThreshold: c.HealthCheck.UnhealthyThreshold,
			HealthEndpoint:     c.HealthCheck.HealthEndpoint,
		},
		Queue:          proxy.QueueConfig{MaxSize: c.Queue.MaxSize, Timeout: c.Queue.Timeout},
		Timeout:        c.Timeout,
		MaxRetries:     c.MaxRetries,
		RetryBackoff:   c.RetryBackoff,
		EnableQueueing: c.Queue.Enabled,
	}

	// Convert backends with default weight handling
	for i, backend := range c.Backends {
		weight := backend.Weight
		if weight <= 0 {
			weight = domain.DefaultBackendWeight
		}
		enhancedConfig.Backends[i] = proxy.BackendConfig{
			URL:    backend.URL,
			Weight: weight,
		}
	}

	// Set defaults
	if enhancedConfig.MaxRetries <= 0 {
		enhancedConfig.MaxRetries = domain.DefaultProxyMaxRetries
	}
	if enhancedConfig.HealthCheck.HealthEndpoint == "" {
		enhancedConfig.HealthCheck.HealthEndpoint = "/health"
	}

	return enhancedConfig, nil
}
