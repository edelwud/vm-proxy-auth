package testutils

import (
	"errors"
)

// MockRaftManager provides a mock implementation of RaftManager for testing.
type MockRaftManager struct {
	IsLeaderVal bool
	AddVoterErr error
	RemoveErr   error
	Peers       []string
	LeaderID    string
	LeaderAddr  string
}

func NewMockRaftManager() *MockRaftManager {
	return &MockRaftManager{
		IsLeaderVal: true,
		Peers:       []string{},
		LeaderID:    "test-leader",
		LeaderAddr:  "127.0.0.1:9000",
	}
}

func (m *MockRaftManager) AddVoter(_, _ string) error {
	if !m.IsLeaderVal {
		return errors.New("not leader")
	}
	return m.AddVoterErr
}

func (m *MockRaftManager) RemoveServer(_ string) error {
	if !m.IsLeaderVal {
		return errors.New("not leader")
	}
	return m.RemoveErr
}

func (m *MockRaftManager) GetLeader() (string, string) {
	return m.LeaderID, m.LeaderAddr
}

func (m *MockRaftManager) GetLeaderID() string {
	return m.LeaderID
}

func (m *MockRaftManager) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"state":  "Leader",
		"term":   1,
		"peers":  m.Peers,
		"leader": m.LeaderID,
	}
}

func (m *MockRaftManager) GetPeers() ([]string, error) {
	return m.Peers, nil
}

func (m *MockRaftManager) IsLeader() bool {
	return m.IsLeaderVal
}

func (m *MockRaftManager) TryDelayedBootstrap(_ []string) error {
	// Mock implementation - do nothing
	return nil
}

func (m *MockRaftManager) ForceRecoverCluster(_, _ string) error {
	// Mock implementation - do nothing
	return nil
}
