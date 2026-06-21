package session

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutil"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

// localFileStorage implements tools.FileStorage using the local workspace sessions directory.
type localFileStorage struct {
	workspacePath string
	sessionID     string
}

// NewLocalFileStorage creates a new FileStorage instance configured for the session.
func NewLocalFileStorage(workspacePath, sessionID string) tools.FileStorage {
	return &localFileStorage{
		workspacePath: workspacePath,
		sessionID:     sessionID,
	}
}

func (l *localFileStorage) Save(ctx context.Context, relativePath string, r io.Reader) (string, error) {
	wsDir, err := xdg.WorkspaceDir(l.workspacePath)
	if err != nil {
		return "", err
	}

	destPath := filepath.Join(wsDir, "sessions", l.sessionID, relativePath)
	destDir := filepath.Dir(destPath)

	if err := fsutil.EnsureDir(destDir); err != nil {
		return "", err
	}

	out, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	if _, err := io.Copy(out, r); err != nil {
		return "", err
	}

	return destPath, nil
}

func (l *localFileStorage) Get(ctx context.Context, relativePath string) (io.ReadCloser, error) {
	wsDir, err := xdg.WorkspaceDir(l.workspacePath)
	if err != nil {
		return nil, err
	}

	destPath := filepath.Join(wsDir, "sessions", l.sessionID, relativePath)
	return os.Open(destPath)
}
