package testutils

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
)

// GetFreePort returns a free TCP port for testing with proper error handling.
// Avoids global variables by using the OS to ensure unique port allocation.
func GetFreePort() (int, error) {
	lc := &net.ListenConfig{}
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("failed to allocate port: %w", err)
	}
	defer listener.Close()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("failed to convert to TCP address")
	}
	return tcpAddr.Port, nil
}

// GetFreePorts returns multiple free TCP ports for testing.
func GetFreePorts(count int) ([]int, error) {
	ports := make([]int, count)
	for i := range count {
		port, err := GetFreePort()
		if err != nil {
			return nil, fmt.Errorf("failed to allocate port %d: %w", i, err)
		}
		ports[i] = port
	}
	return ports, nil
}

// CopyFile copies a file from src to dst with comprehensive error handling.
func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", src, err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file %s: %w", dst, err)
	}
	defer dstFile.Close()

	if _, copyErr := io.Copy(dstFile, srcFile); copyErr != nil {
		return fmt.Errorf("failed to copy from %s to %s: %w", src, dst, copyErr)
	}

	return nil
}
