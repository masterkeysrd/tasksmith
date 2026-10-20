package xdg

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
)

// WorkspacesHome returns the root directory for all workspaces (<DataHome>/workspaces).
func WorkspacesHome() (string, error) {
	return SubDataDir("workspaces")
}

// WorkspaceDir returns a deterministic directory path for a given workspace key.
// The directory is located inside WorkspacesHome() and named after the 16-hex-character
// prefix (first 8 bytes) of the SHA-256 digest of key.
func WorkspaceDir(key string) (string, error) {
	hash := sha256.Sum256([]byte(key))
	workspacesDir, err := WorkspacesHome()
	if err != nil {
		return "", fmt.Errorf("failed to get workspaces home: %w", err)
	}

	return filepath.Join(workspacesDir, fmt.Sprintf("%x", hash[:8])), nil
}

// LogsDir returns the directory path for application logs (<StateHome>/logs),
// following Neovim's stdpath("log") convention under XDG_STATE_HOME.
func LogsDir() (string, error) {
	return SubStateDir("logs")
}

// LogFilePath returns the full path for a given log filename within LogsDir().
func LogFilePath(filename string) (string, error) {
	dir, err := LogsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filename), nil
}

// SocketPath returns the full path for an ephemeral socket or lockfile (such as
// "brain.sock" or "brain.lock") inside the application's runtime directory.
func SocketPath(name string) (string, error) {
	return SubRuntimeDir(name)
}
