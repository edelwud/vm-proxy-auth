package testutils

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
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

// CopyFile copies a file from src to dst with comprehensive error handling and security validation.
func CopyFile(src, dst string) error {
	// Validate paths to prevent directory traversal attacks
	if err := validateFilePath(src); err != nil {
		return fmt.Errorf("invalid source path: %w", err)
	}
	if err := validateFilePath(dst); err != nil {
		return fmt.Errorf("invalid destination path: %w", err)
	}

	// Clean and validate paths
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

	srcFile, err := os.Open(src) // #nosec G304 - path validated above
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", src, err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst) // #nosec G304 - path validated above
	if err != nil {
		return fmt.Errorf("failed to create destination file %s: %w", dst, err)
	}
	defer dstFile.Close()

	if _, copyErr := io.Copy(dstFile, srcFile); copyErr != nil {
		return fmt.Errorf("failed to copy from %s to %s: %w", src, dst, copyErr)
	}

	return nil
}

// validateFilePath validates file paths to prevent directory traversal attacks.
func validateFilePath(path string) error {
	if path == "" {
		return errors.New("empty path")
	}

	// Prevent directory traversal
	if strings.Contains(path, "..") {
		return errors.New("path traversal detected")
	}

	// Clean the path and check if it's absolute or relative to current directory
	cleanPath := filepath.Clean(path)
	if filepath.IsAbs(cleanPath) {
		// For absolute paths, ensure they're within allowed directories
		// This is a basic check - in production you'd have more specific rules
		if strings.HasPrefix(cleanPath, "/tmp/") || strings.HasPrefix(cleanPath, "/var/tmp/") {
			return nil // Allow temp directories for tests
		}

		// Allow OS temp directory and its subdirectories (for testing)
		tempDir := os.TempDir()
		if strings.HasPrefix(cleanPath, tempDir) {
			return nil
		}

		// Allow paths within the current working directory tree
		wd, err := os.Getwd()
		if err == nil && strings.HasPrefix(cleanPath, wd) {
			return nil
		}
	}

	// For relative paths, ensure they don't escape current directory
	if !strings.HasPrefix(cleanPath, "/") && !strings.Contains(cleanPath, "..") {
		return nil
	}

	return errors.New("potentially unsafe file path")
}
