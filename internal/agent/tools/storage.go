package tools

import (
	"context"
	"io"
)

// FileStorage defines a generic storage system for session-specific files.
type FileStorage interface {
	Save(ctx context.Context, relativePath string, r io.Reader) (string, error)
	Get(ctx context.Context, relativePath string) (io.ReadCloser, error)
}
