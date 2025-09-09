package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// LoadConfig loads configuration from file and environment variables.
func LoadConfig(configPath string) (*Config, error) {
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
	bindEnvironmentVariables(v)

	// Set defaults from modules
	setDefaults(v)

	// Read configuration file
	if err := v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFoundError) {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		// Config file not found is OK, we'll use defaults and env vars
	}

	// Unmarshal into struct
	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

// bindEnvironmentVariables binds all environment variables for each module.
func bindEnvironmentVariables(v *viper.Viper) {
	bindMetadataEnv(v)
	bindLoggingEnv(v)
	bindMetricsEnv(v)
	bindServerEnv(v)
	bindAuthEnv(v)
	bindProxyEnv(v)
	bindTenantEnv(v)
	bindStorageEnv(v)
	bindClusterEnv(v)
}

func bindMetadataEnv(v *viper.Viper) {
	_ = v.BindEnv("metadata.nodeId", "VM_PROXY_AUTH_METADATA_NODE_ID")
	_ = v.BindEnv("metadata.role", "VM_PROXY_AUTH_METADATA_ROLE")
	_ = v.BindEnv("metadata.environment", "VM_PROXY_AUTH_METADATA_ENVIRONMENT")
	_ = v.BindEnv("metadata.region", "VM_PROXY_AUTH_METADATA_REGION")
}

func bindLoggingEnv(v *viper.Viper) {
	_ = v.BindEnv("logging.level", "VM_PROXY_AUTH_LOGGING_LEVEL")
	_ = v.BindEnv("logging.format", "VM_PROXY_AUTH_LOGGING_FORMAT")
}

func bindMetricsEnv(v *viper.Viper) {
	_ = v.BindEnv("metrics.enabled", "VM_PROXY_AUTH_METRICS_ENABLED")
	_ = v.BindEnv("metrics.path", "VM_PROXY_AUTH_METRICS_PATH")
}

func bindServerEnv(v *viper.Viper) {
	_ = v.BindEnv("server.address", "VM_PROXY_AUTH_SERVER_ADDRESS")
	_ = v.BindEnv("server.timeouts.read", "VM_PROXY_AUTH_SERVER_TIMEOUTS_READ")
	_ = v.BindEnv("server.timeouts.write", "VM_PROXY_AUTH_SERVER_TIMEOUTS_WRITE")
	_ = v.BindEnv("server.timeouts.idle", "VM_PROXY_AUTH_SERVER_TIMEOUTS_IDLE")
}

func bindAuthEnv(v *viper.Viper) {
	_ = v.BindEnv("auth.jwt.algorithm", "VM_PROXY_AUTH_AUTH_JWT_ALGORITHM")
	_ = v.BindEnv("auth.jwt.secret", "VM_PROXY_AUTH_AUTH_JWT_SECRET")
	_ = v.BindEnv("auth.jwt.jwksUrl", "VM_PROXY_AUTH_AUTH_JWT_JWKS_URL")
	_ = v.BindEnv("auth.jwt.validation.audience", "VM_PROXY_AUTH_AUTH_JWT_VALIDATION_AUDIENCE")
	_ = v.BindEnv("auth.jwt.validation.issuer", "VM_PROXY_AUTH_AUTH_JWT_VALIDATION_ISSUER")
	_ = v.BindEnv("auth.jwt.claims.userGroups", "VM_PROXY_AUTH_AUTH_JWT_CLAIMS_USER_GROUPS")
	_ = v.BindEnv("auth.jwt.cache.tokenTTL", "VM_PROXY_AUTH_AUTH_JWT_CACHE_TOKEN_TTL")
	_ = v.BindEnv("auth.jwt.cache.jwksTTL", "VM_PROXY_AUTH_AUTH_JWT_CACHE_JWKS_TTL")
}

func bindProxyEnv(v *viper.Viper) {
	// Proxy routing
	_ = v.BindEnv("proxy.routing.strategy", "VM_PROXY_AUTH_PROXY_ROUTING_STRATEGY")
	_ = v.BindEnv("proxy.routing.healthCheck.interval", "VM_PROXY_AUTH_PROXY_ROUTING_HEALTH_CHECK_INTERVAL")
	_ = v.BindEnv("proxy.routing.healthCheck.timeout", "VM_PROXY_AUTH_PROXY_ROUTING_HEALTH_CHECK_TIMEOUT")
	_ = v.BindEnv("proxy.routing.healthCheck.endpoint", "VM_PROXY_AUTH_PROXY_ROUTING_HEALTH_CHECK_ENDPOINT")
	_ = v.BindEnv("proxy.routing.healthCheck.healthyThreshold",
		"VM_PROXY_AUTH_PROXY_ROUTING_HEALTH_CHECK_HEALTHY_THRESHOLD")
	_ = v.BindEnv("proxy.routing.healthCheck.unhealthyThreshold",
		"VM_PROXY_AUTH_PROXY_ROUTING_HEALTH_CHECK_UNHEALTHY_THRESHOLD")

	// Proxy reliability
	_ = v.BindEnv("proxy.reliability.timeout", "VM_PROXY_AUTH_PROXY_RELIABILITY_TIMEOUT")
	_ = v.BindEnv("proxy.reliability.retries", "VM_PROXY_AUTH_PROXY_RELIABILITY_RETRIES")
	_ = v.BindEnv("proxy.reliability.backoff", "VM_PROXY_AUTH_PROXY_RELIABILITY_BACKOFF")
	_ = v.BindEnv("proxy.reliability.queue.enabled", "VM_PROXY_AUTH_PROXY_RELIABILITY_QUEUE_ENABLED")
	_ = v.BindEnv("proxy.reliability.queue.maxSize", "VM_PROXY_AUTH_PROXY_RELIABILITY_QUEUE_MAX_SIZE")
	_ = v.BindEnv("proxy.reliability.queue.timeout", "VM_PROXY_AUTH_PROXY_RELIABILITY_QUEUE_TIMEOUT")

	// Proxy upstreams (dynamic configuration)
	_ = v.BindEnv("proxy.upstreams", "VM_PROXY_AUTH_PROXY_UPSTREAMS")
}

func bindTenantEnv(v *viper.Viper) {
	_ = v.BindEnv("tenants.filter.strategy", "VM_PROXY_AUTH_TENANTS_FILTER_STRATEGY")
	_ = v.BindEnv("tenants.filter.labels.account", "VM_PROXY_AUTH_TENANTS_FILTER_LABELS_ACCOUNT")
	_ = v.BindEnv("tenants.filter.labels.project", "VM_PROXY_AUTH_TENANTS_FILTER_LABELS_PROJECT")
	_ = v.BindEnv("tenants.filter.labels.useProjectId", "VM_PROXY_AUTH_TENANTS_FILTER_LABELS_USE_PROJECT_ID")
	_ = v.BindEnv("tenants.mappings", "VM_PROXY_AUTH_TENANTS_MAPPINGS")
}

func bindStorageEnv(v *viper.Viper) {
	_ = v.BindEnv("storage.type", "VM_PROXY_AUTH_STORAGE_TYPE")

	// Redis storage
	_ = v.BindEnv("storage.redis.address", "VM_PROXY_AUTH_STORAGE_REDIS_ADDRESS")
	_ = v.BindEnv("storage.redis.password", "VM_PROXY_AUTH_STORAGE_REDIS_PASSWORD")
	_ = v.BindEnv("storage.redis.database", "VM_PROXY_AUTH_STORAGE_REDIS_DATABASE")
	_ = v.BindEnv("storage.redis.keyPrefix", "VM_PROXY_AUTH_STORAGE_REDIS_KEY_PREFIX")
	_ = v.BindEnv("storage.redis.pool.size", "VM_PROXY_AUTH_STORAGE_REDIS_POOL_SIZE")
	_ = v.BindEnv("storage.redis.pool.minIdle", "VM_PROXY_AUTH_STORAGE_REDIS_POOL_MIN_IDLE")
	_ = v.BindEnv("storage.redis.timeouts.connect", "VM_PROXY_AUTH_STORAGE_REDIS_TIMEOUTS_CONNECT")
	_ = v.BindEnv("storage.redis.timeouts.read", "VM_PROXY_AUTH_STORAGE_REDIS_TIMEOUTS_READ")
	_ = v.BindEnv("storage.redis.timeouts.write", "VM_PROXY_AUTH_STORAGE_REDIS_TIMEOUTS_WRITE")
	_ = v.BindEnv("storage.redis.retry.maxAttempts", "VM_PROXY_AUTH_STORAGE_REDIS_RETRY_MAX_ATTEMPTS")
	_ = v.BindEnv("storage.redis.retry.backoff.min", "VM_PROXY_AUTH_STORAGE_REDIS_RETRY_BACKOFF_MIN")
	_ = v.BindEnv("storage.redis.retry.backoff.max", "VM_PROXY_AUTH_STORAGE_REDIS_RETRY_BACKOFF_MAX")

	// Raft storage
	_ = v.BindEnv("storage.raft.bindAddress", "VM_PROXY_AUTH_STORAGE_RAFT_BIND_ADDRESS")
	_ = v.BindEnv("storage.raft.dataDir", "VM_PROXY_AUTH_STORAGE_RAFT_DATA_DIR")
	_ = v.BindEnv("storage.raft.bootstrapExpected", "VM_PROXY_AUTH_STORAGE_RAFT_BOOTSTRAP_EXPECTED")
	_ = v.BindEnv("storage.raft.peers", "VM_PROXY_AUTH_STORAGE_RAFT_PEERS")
}

func bindClusterEnv(v *viper.Viper) {
	// Memberlist
	_ = v.BindEnv("cluster.memberlist.bindAddress", "VM_PROXY_AUTH_CLUSTER_MEMBERLIST_BIND_ADDRESS")
	_ = v.BindEnv("cluster.memberlist.advertiseAddress", "VM_PROXY_AUTH_CLUSTER_MEMBERLIST_ADVERTISE_ADDRESS")
	_ = v.BindEnv("cluster.memberlist.peers.join", "VM_PROXY_AUTH_CLUSTER_MEMBERLIST_PEERS_JOIN")
	_ = v.BindEnv("cluster.memberlist.peers.encryption", "VM_PROXY_AUTH_CLUSTER_MEMBERLIST_PEERS_ENCRYPTION")
	_ = v.BindEnv("cluster.memberlist.gossip.interval", "VM_PROXY_AUTH_CLUSTER_MEMBERLIST_GOSSIP_INTERVAL")
	_ = v.BindEnv("cluster.memberlist.gossip.nodes", "VM_PROXY_AUTH_CLUSTER_MEMBERLIST_GOSSIP_NODES")
	_ = v.BindEnv("cluster.memberlist.probe.interval", "VM_PROXY_AUTH_CLUSTER_MEMBERLIST_PROBE_INTERVAL")
	_ = v.BindEnv("cluster.memberlist.probe.timeout", "VM_PROXY_AUTH_CLUSTER_MEMBERLIST_PROBE_TIMEOUT")
	_ = v.BindEnv("cluster.memberlist.metadata", "VM_PROXY_AUTH_CLUSTER_MEMBERLIST_METADATA")

	// Discovery
	_ = v.BindEnv("cluster.discovery.enabled", "VM_PROXY_AUTH_CLUSTER_DISCOVERY_ENABLED")
	_ = v.BindEnv("cluster.discovery.interval", "VM_PROXY_AUTH_CLUSTER_DISCOVERY_INTERVAL")
	_ = v.BindEnv("cluster.discovery.providers", "VM_PROXY_AUTH_CLUSTER_DISCOVERY_PROVIDERS")

	// Static discovery
	_ = v.BindEnv("cluster.discovery.static.peers", "VM_PROXY_AUTH_CLUSTER_DISCOVERY_STATIC_PEERS")

	// mDNS discovery
	_ = v.BindEnv("cluster.discovery.mdns.enabled", "VM_PROXY_AUTH_CLUSTER_DISCOVERY_MDNS_ENABLED")
	_ = v.BindEnv("cluster.discovery.mdns.serviceName", "VM_PROXY_AUTH_CLUSTER_DISCOVERY_MDNS_SERVICE_NAME")
	_ = v.BindEnv("cluster.discovery.mdns.domain", "VM_PROXY_AUTH_CLUSTER_DISCOVERY_MDNS_DOMAIN")
	_ = v.BindEnv("cluster.discovery.mdns.hostname", "VM_PROXY_AUTH_CLUSTER_DISCOVERY_MDNS_HOSTNAME")
	_ = v.BindEnv("cluster.discovery.mdns.port", "VM_PROXY_AUTH_CLUSTER_DISCOVERY_MDNS_PORT")
	_ = v.BindEnv("cluster.discovery.mdns.txtRecords", "VM_PROXY_AUTH_CLUSTER_DISCOVERY_MDNS_TXT_RECORDS")
}

// setDefaults sets all default values from modules.
func setDefaults(v *viper.Viper) {
	defaults := GetDefaults()

	setMetadataDefaults(v, defaults)
	setLoggingDefaults(v, defaults)
	setMetricsDefaults(v, defaults)
	setServerDefaults(v, defaults)
	setProxyDefaults(v, defaults)
	setAuthDefaults(v, defaults)
	setTenantDefaults(v, defaults)
	setStorageDefaults(v, defaults)
	setClusterDefaults(v, defaults)
}

func setMetadataDefaults(v *viper.Viper, defaults *Config) {
	v.SetDefault("metadata.nodeId", defaults.Metadata.NodeID)
	v.SetDefault("metadata.role", defaults.Metadata.Role)
	v.SetDefault("metadata.environment", defaults.Metadata.Environment)
	v.SetDefault("metadata.region", defaults.Metadata.Region)
}

func setLoggingDefaults(v *viper.Viper, defaults *Config) {
	v.SetDefault("logging.level", defaults.Logging.Level)
	v.SetDefault("logging.format", defaults.Logging.Format)
}

func setMetricsDefaults(v *viper.Viper, defaults *Config) {
	v.SetDefault("metrics.enabled", defaults.Metrics.Enabled)
	v.SetDefault("metrics.path", defaults.Metrics.Path)
}

func setServerDefaults(v *viper.Viper, defaults *Config) {
	v.SetDefault("server.address", defaults.Server.Address)
	v.SetDefault("server.timeouts.read", defaults.Server.Timeouts.Read.String())
	v.SetDefault("server.timeouts.write", defaults.Server.Timeouts.Write.String())
	v.SetDefault("server.timeouts.idle", defaults.Server.Timeouts.Idle.String())
}

func setProxyDefaults(v *viper.Viper, defaults *Config) {
	v.SetDefault("proxy.routing.strategy", defaults.Proxy.Routing.Strategy)
	v.SetDefault("proxy.routing.healthCheck.interval", defaults.Proxy.Routing.HealthCheck.Interval.String())
	v.SetDefault("proxy.routing.healthCheck.timeout", defaults.Proxy.Routing.HealthCheck.Timeout.String())
	v.SetDefault("proxy.routing.healthCheck.endpoint", defaults.Proxy.Routing.HealthCheck.Endpoint)
	v.SetDefault("proxy.routing.healthCheck.healthyThreshold", defaults.Proxy.Routing.HealthCheck.HealthyThreshold)
	v.SetDefault("proxy.routing.healthCheck.unhealthyThreshold", defaults.Proxy.Routing.HealthCheck.UnhealthyThreshold)
	v.SetDefault("proxy.reliability.timeout", defaults.Proxy.Reliability.Timeout.String())
	v.SetDefault("proxy.reliability.retries", defaults.Proxy.Reliability.Retries)
	v.SetDefault("proxy.reliability.backoff", defaults.Proxy.Reliability.Backoff.String())
	v.SetDefault("proxy.reliability.queue.enabled", defaults.Proxy.Reliability.Queue.Enabled)
	v.SetDefault("proxy.reliability.queue.maxSize", defaults.Proxy.Reliability.Queue.MaxSize)
	v.SetDefault("proxy.reliability.queue.timeout", defaults.Proxy.Reliability.Queue.Timeout.String())
}

func setAuthDefaults(v *viper.Viper, defaults *Config) {
	v.SetDefault("auth.jwt.algorithm", defaults.Auth.JWT.Algorithm)
	v.SetDefault("auth.jwt.validation.audience", defaults.Auth.JWT.Validation.Audience)
	v.SetDefault("auth.jwt.validation.issuer", defaults.Auth.JWT.Validation.Issuer)
	v.SetDefault("auth.jwt.claims.userGroups", defaults.Auth.JWT.Claims.UserGroups)
	v.SetDefault("auth.jwt.cache.tokenTTL", defaults.Auth.JWT.Cache.TokenTTL.String())
	v.SetDefault("auth.jwt.cache.jwksTTL", defaults.Auth.JWT.Cache.JwksTTL.String())
}

func setTenantDefaults(v *viper.Viper, defaults *Config) {
	v.SetDefault("tenants.filter.strategy", defaults.Tenants.Filter.Strategy)
	v.SetDefault("tenants.filter.labels.account", defaults.Tenants.Filter.Labels.Account)
	v.SetDefault("tenants.filter.labels.project", defaults.Tenants.Filter.Labels.Project)
	v.SetDefault("tenants.filter.labels.useProjectId", defaults.Tenants.Filter.Labels.UseProjectID)
}

func setStorageDefaults(v *viper.Viper, defaults *Config) {
	v.SetDefault("storage.type", defaults.Storage.Type)
	v.SetDefault("storage.redis.database", defaults.Storage.Redis.Database)
	v.SetDefault("storage.redis.keyPrefix", defaults.Storage.Redis.KeyPrefix)
	v.SetDefault("storage.redis.pool.size", defaults.Storage.Redis.Pool.Size)
	v.SetDefault("storage.redis.pool.minIdle", defaults.Storage.Redis.Pool.MinIdle)
	v.SetDefault("storage.redis.timeouts.connect", defaults.Storage.Redis.Timeouts.Connect.String())
	v.SetDefault("storage.redis.timeouts.read", defaults.Storage.Redis.Timeouts.Read.String())
	v.SetDefault("storage.redis.timeouts.write", defaults.Storage.Redis.Timeouts.Write.String())
	v.SetDefault("storage.redis.retry.maxAttempts", defaults.Storage.Redis.Retry.MaxAttempts)
	v.SetDefault("storage.redis.retry.backoff.min", defaults.Storage.Redis.Retry.Backoff.Min.String())
	v.SetDefault("storage.redis.retry.backoff.max", defaults.Storage.Redis.Retry.Backoff.Max.String())
	v.SetDefault("storage.raft.bindAddress", defaults.Storage.Raft.BindAddress)
	v.SetDefault("storage.raft.dataDir", defaults.Storage.Raft.DataDir)
	v.SetDefault("storage.raft.bootstrapExpected", defaults.Storage.Raft.BootstrapExpected)
}

func setClusterDefaults(v *viper.Viper, defaults *Config) {
	v.SetDefault("cluster.memberlist.bindAddress", defaults.Cluster.Memberlist.BindAddress)
	v.SetDefault("cluster.memberlist.gossip.interval", defaults.Cluster.Memberlist.Gossip.Interval.String())
	v.SetDefault("cluster.memberlist.gossip.nodes", defaults.Cluster.Memberlist.Gossip.Nodes)
	v.SetDefault("cluster.memberlist.probe.interval", defaults.Cluster.Memberlist.Probe.Interval.String())
	v.SetDefault("cluster.memberlist.probe.timeout", defaults.Cluster.Memberlist.Probe.Timeout.String())
	v.SetDefault("cluster.discovery.interval", defaults.Cluster.Discovery.Interval.String())
	v.SetDefault("cluster.discovery.mdns.enabled", defaults.Cluster.Discovery.MDNS.Enabled)
	v.SetDefault("cluster.discovery.mdns.serviceName", defaults.Cluster.Discovery.MDNS.ServiceName)
	v.SetDefault("cluster.discovery.mdns.domain", defaults.Cluster.Discovery.MDNS.Domain)
	v.SetDefault("cluster.discovery.mdns.port", defaults.Cluster.Discovery.MDNS.Port)
}
