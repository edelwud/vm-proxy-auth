package discovery

import (
	"fmt"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

const (
	k8sDefaultWatchTimeout = 10 * time.Minute
)

// NewServiceDiscovery creates a new service discovery instance based on type.
func NewServiceDiscovery(
	discoveryType string,
	kubeConfig config.KubernetesDiscoveryConfig,
	dnsConfig config.DNSDiscoveryConfig,
	logger domain.Logger,
) (domain.ServiceDiscovery, error) {
	switch discoveryType {
	case "kubernetes":
		return newKubernetesDiscovery(kubeConfig, logger)
	case "dns":
		return newDNSDiscovery(dnsConfig, logger), nil
	default:
		return nil, fmt.Errorf("unsupported service discovery type: %s", discoveryType)
	}
}

// newKubernetesDiscovery creates a new Kubernetes service discovery instance.
func newKubernetesDiscovery(
	cfg config.KubernetesDiscoveryConfig,
	logger domain.Logger,
) (domain.ServiceDiscovery, error) {
	// Convert config type
	kubeConfig := KubernetesDiscoveryConfig{
		Namespace:            cfg.Namespace,
		PeerLabelSelector:    cfg.PeerLabelSelector,
		BackendLabelSelector: cfg.BackendLabelSelector,
		RaftPortName:         cfg.RaftPortName,
		HTTPPortName:         cfg.HTTPPortName,
		WatchTimeout:         cfg.WatchTimeout,
	}

	return NewKubernetesDiscovery(kubeConfig, logger)
}

// newDNSDiscovery creates a new DNS service discovery instance.
func newDNSDiscovery(
	config config.DNSDiscoveryConfig,
	logger domain.Logger,
) domain.ServiceDiscovery {
	return NewDNSDiscovery(config, logger)
}
