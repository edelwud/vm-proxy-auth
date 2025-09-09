package config

import (
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/auth"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/cluster"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/logging"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/metadata"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/metrics"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/proxy"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/server"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/storage"
	"github.com/edelwud/vm-proxy-auth/internal/config/modules/tenant"
)

// GetDefaults returns complete default configuration.
func GetDefaults() *Config {
	return &Config{
		Metadata: metadata.GetDefaults(),
		Logging:  logging.GetDefaults(),
		Metrics:  metrics.GetDefaults(),
		Server:   server.GetDefaults(),
		Proxy:    proxy.GetDefaults(),
		Auth:     auth.GetDefaults(),
		Tenants:  tenant.GetDefaults(),
		Storage:  storage.GetDefaults(),
		Cluster:  cluster.GetDefaults(),
	}
}
