package testutils

// MockPeerJoiner implements the PeerJoiner interface for testing.
type MockPeerJoiner struct {
	JoinedPeers []string
}

// Join joins the specified peers.
func (m *MockPeerJoiner) Join(peers []string) error {
	m.JoinedPeers = append(m.JoinedPeers, peers...)
	return nil
}
