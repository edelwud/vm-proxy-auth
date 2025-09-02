package discovery_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config"
	"github.com/edelwud/vm-proxy-auth/internal/services/discovery"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func TestNewServiceDiscovery_DNS(t *testing.T) {
	logger := testutils.NewMockLogger()

	kubeConfig := config.KubernetesDiscoveryConfig{
		Namespace: "default",
	}
	dnsConfig := config.DNSDiscoveryConfig{
		Domain: "test.local",
	}

	discovery, err := discovery.NewServiceDiscovery("dns", kubeConfig, dnsConfig, config.MDNSDiscoveryConfig{}, logger)
	require.NoError(t, err)
	require.NotNil(t, discovery)
}

func TestNewServiceDiscovery_UnsupportedType(t *testing.T) {
	logger := testutils.NewMockLogger()

	kubeConfig := config.KubernetesDiscoveryConfig{}
	dnsConfig := config.DNSDiscoveryConfig{}

	discovery, err := discovery.NewServiceDiscovery(
		"unsupported",
		kubeConfig,
		dnsConfig,
		config.MDNSDiscoveryConfig{},
		logger,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported service discovery type")
	require.Nil(t, discovery)
}

func TestNewDNSDiscovery_Simple(t *testing.T) {
	logger := testutils.NewMockLogger()

	config := config.DNSDiscoveryConfig{
		Domain: "test.local",
	}

	dnsDiscovery := discovery.NewDNSDiscovery(config, logger)

	require.NotNil(t, dnsDiscovery)
	require.NotNil(t, dnsDiscovery.Events())
}
