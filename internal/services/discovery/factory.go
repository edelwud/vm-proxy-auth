package discovery

import (
	"errors"
	"fmt"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/discovery/providers"
)

// Factory creates discovery providers based on configuration.
type Factory struct {
	logger domain.Logger
}

// NewFactory creates a new discovery factory.
func NewFactory(logger domain.Logger) *Factory {
	return &Factory{
		logger: logger.With(domain.Field{Key: "component", Value: "discovery-factory"}),
	}
}

// CreateProviders creates discovery providers based on configuration.
func (f *Factory) CreateProviders(cfg config.DiscoverySettings) ([]domain.DiscoveryProvider, error) {
	var discoveryProviders []domain.DiscoveryProvider

	for _, providerType := range cfg.Providers {
		provider, err := f.createProvider(providerType, cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create %s provider: %w", providerType, err)
		}

		if provider != nil {
			discoveryProviders = append(discoveryProviders, provider)
			f.logger.Info("Created discovery provider",
				domain.Field{Key: "type", Value: providerType})
		}
	}

	if len(discoveryProviders) == 0 {
		f.logger.Info("No discovery providers configured")
	}

	return discoveryProviders, nil
}

// createProvider creates a single discovery provider.
func (f *Factory) createProvider(providerType string, cfg config.DiscoverySettings) (domain.DiscoveryProvider, error) {
	switch providerType {
	case "static":
		return f.createStaticProvider(cfg)
	case "mdns":
		return f.createMDNSProvider(cfg)
	default:
		return nil, fmt.Errorf("unknown discovery provider type: %s", providerType)
	}
}

// createStaticProvider creates a static discovery provider.
func (f *Factory) createStaticProvider(cfg config.DiscoverySettings) (domain.DiscoveryProvider, error) {
	if len(cfg.Static.Peers) == 0 {
		f.logger.Debug("No static peers configured, skipping static provider")
		return nil, errors.New("no static peers configured")
	}

	return providers.NewStaticProvider(cfg.Static.Peers, f.logger), nil
}

// createMDNSProvider creates an mDNS discovery provider.
func (f *Factory) createMDNSProvider(cfg config.DiscoverySettings) (domain.DiscoveryProvider, error) {
	return providers.NewMDNSProvider(
		cfg.MDNS.ServiceName,
		cfg.MDNS.Domain,
		cfg.MDNS.Hostname,
		cfg.MDNS.Port,
		cfg.MDNS.TXTRecords,
		f.logger,
	), nil
}
