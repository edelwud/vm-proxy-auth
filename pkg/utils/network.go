// Package netutils provides common utility functions for network operations.
package netutils

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

// GetExternalIP returns the external IP address of this machine.
func GetExternalIP() (string, error) {
	const networkTimeout = 5 * time.Second
	dialer := &net.Dialer{Timeout: networkTimeout}
	conn, err := dialer.DialContext(context.Background(), "udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()

	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return "", errors.New("failed to get UDP address")
	}
	return localAddr.IP.String(), nil
}

// ConvertBindToAdvertiseAddress converts bind address (0.0.0.0:port) to routable advertise address.
func ConvertBindToAdvertiseAddress(bindAddr string) (string, error) {
	host, port, err := net.SplitHostPort(bindAddr)
	if err != nil {
		return "", fmt.Errorf("invalid bind address: %w", err)
	}

	// If binding to all interfaces (0.0.0.0), get external IP
	if host == "0.0.0.0" || host == "" {
		externalIP, ipErr := GetExternalIP()
		if ipErr != nil {
			return "", fmt.Errorf("failed to get external IP: %w", ipErr)
		}
		return fmt.Sprintf("%s:%s", externalIP, port), nil
	}

	// If specific IP provided, use as-is
	return bindAddr, nil
}
