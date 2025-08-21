package testutils

import "github.com/edelwud/vm-proxy-auth/internal/domain"

// MockLogger implements domain.Logger for testing purposes.
// Use blank identifier for unused parameters to satisfy revive linter.
type MockLogger struct{}

// Debug implements domain.Logger interface.
func (m *MockLogger) Debug(_ string, _ ...domain.Field) {}

// Info implements domain.Logger interface.
func (m *MockLogger) Info(_ string, _ ...domain.Field) {}

// Warn implements domain.Logger interface.
func (m *MockLogger) Warn(_ string, _ ...domain.Field) {}

// Error implements domain.Logger interface.
func (m *MockLogger) Error(_ string, _ ...domain.Field) {}

// With implements domain.Logger interface.
func (m *MockLogger) With(_ ...domain.Field) domain.Logger { return m }

