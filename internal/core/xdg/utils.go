package xdg

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
)

// WorkspaceDir returns a unique directory path for a given workspace key.
// The directory is located within the workspaces home directory and is
// named after the first 16 characters of the SHA-256 hash of the key.
func WorkspaceDir(key string) (string, error) {
	hash := sha256.Sum256([]byte(key))
	workspacesDir, err := WorkspacesHome()
	if err != nil {
		return "", fmt.Errorf("failed to get workspaces home: %w", err)
	}

	return filepath.Join(workspacesDir, fmt.Sprintf("%x", hash[:8])), nil
}

// WorkspacesHome returns the root directory for all workspaces.
func WorkspacesHome() (string, error) {
	return SubDataDir("workspaces")
}

// LogsDir returns the directory path for application logs.
func LogsDir() (string, error) {
	return SubDataDir("logs")
}

// LogFilePath returns the full path for a given log filename within the logs directory.
func LogFilePath(filename string) (string, error) {
	dir, err := LogsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filename), nil
}
