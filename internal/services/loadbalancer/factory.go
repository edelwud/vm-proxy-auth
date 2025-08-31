package loadbalancer

import (
	"errors"
	"fmt"
	"slices"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// Factory creates load balancers based on strategy configuration.
type Factory struct {
	logger domain.Logger
}

// NewFactory creates a new load balancer factory.
func NewFactory(logger domain.Logger) *Factory {
	return &Factory{
		logger: logger.With(domain.Field{Key: "component", Value: "lb_factory"}),
	}
}

// CreateLoadBalancer creates a load balancer based on the specified strategy.
func (f *Factory) CreateLoadBalancer(
	strategy domain.LoadBalancingStrategy,
	backends []domain.Backend,
) (domain.LoadBalancer, error) {
	if len(backends) == 0 {
		return nil, errors.New("cannot create load balancer with empty backends list")
	}

	f.logger.Debug("Creating load balancer",
		domain.Field{Key: "strategy", Value: string(strategy)},
		domain.Field{Key: "backends_count", Value: len(backends)})

	switch strategy {
	case domain.LoadBalancingRoundRobin:
		f.logger.Debug("Creating Round Robin load balancer")
		return NewRoundRobinBalancer(backends, f.logger), nil

	case domain.LoadBalancingWeightedRoundRobin:
		f.logger.Debug("Creating Weighted Round Robin load balancer")
		return NewWeightedRoundRobinBalancer(backends, f.logger), nil

	case domain.LoadBalancingLeastConnections:
		f.logger.Debug("Creating Least Connections load balancer")
		return NewLeastConnectionsBalancer(backends, f.logger), nil

	default:
		f.logger.Error("Unsupported load balancing strategy",
			domain.Field{Key: "strategy", Value: string(strategy)})
		return nil, fmt.Errorf("unsupported load balancing strategy: %s", strategy)
	}
}

// GetSupportedStrategies returns all supported load balancing strategies.
func (f *Factory) GetSupportedStrategies() []domain.LoadBalancingStrategy {
	return []domain.LoadBalancingStrategy{
		domain.LoadBalancingRoundRobin,
		domain.LoadBalancingWeightedRoundRobin,
		domain.LoadBalancingLeastConnections,
	}
}

// ValidateStrategy checks if the given strategy is supported.
func (f *Factory) ValidateStrategy(strategy domain.LoadBalancingStrategy) error {
	supportedStrategies := f.GetSupportedStrategies()

	if slices.Contains(supportedStrategies, strategy) {
		return nil
	}

	return fmt.Errorf("unsupported load balancing strategy: %s. Supported strategies: %v",
		strategy, supportedStrategies)
}

// GetStrategyDescription returns a human-readable description of the strategy.
func (f *Factory) GetStrategyDescription(strategy domain.LoadBalancingStrategy) string {
	switch strategy {
	case domain.LoadBalancingRoundRobin:
		return "Round Robin: Distributes requests evenly across all healthy backends in sequence"

	case domain.LoadBalancingWeightedRoundRobin:
		return "Weighted Round Robin: Distributes requests based on backend weights using smooth algorithm"

	case domain.LoadBalancingLeastConnections:
		return "Least Connections: Routes requests to the backend with the fewest active connections"

	default:
		return fmt.Sprintf("Unknown strategy: %s", strategy)
	}
}
