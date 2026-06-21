package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/masterkeysrd/tasksmith/internal/core/fs"
)

// Remove removes a file or directory.
func (h *ToolHandlers) Remove(ctx context.Context, in RemoveArgs) (RemoveOutput, error) {
	baseDir := h.CWD
	if baseDir == "" {
		baseDir = "."
	}
	baseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return RemoveOutput{}, fmt.Errorf("failed to resolve workspace directory: %w", err)
	}

	path := cleanPath(in.Path)
	var removeAbs string
	if filepath.IsAbs(path) {
		removeAbs = path
	} else {
		removeAbs = filepath.Join(baseDir, path)
	}
	removeAbs, err = filepath.Abs(removeAbs)
	if err != nil {
		return RemoveOutput{}, fmt.Errorf("failed to resolve target path: %w", err)
	}

	// For safety, verify if the path exists and check if it is a directory
	info, err := os.Lstat(removeAbs)
	if os.IsNotExist(err) {
		return RemoveOutput{
			Path:    path,
			Success: false,
		}, fmt.Errorf("path %q does not exist", path)
	} else if err != nil {
		return RemoveOutput{
			Path:    path,
			Success: false,
		}, fmt.Errorf("failed to access path %q: %w", path, err)
	}

	if info.IsDir() && !in.Recursive {
		return RemoveOutput{
			Path:    path,
			Success: false,
		}, fmt.Errorf("path %q is a directory; use recursive=true to remove", path)
	}

	var fileContent string
	var isBinary bool
	var mimeType string
	if !info.IsDir() {
		mimeType = fs.DetectMIMEType(removeAbs)
		isBinary = fs.IsBinaryMIME(mimeType)
		if !isBinary {
			if data, err := os.ReadFile(removeAbs); err == nil {
				fileContent = string(data)
			}
		}
	}

	// Remove file or directory recursively
	if err := os.RemoveAll(removeAbs); err != nil {
		return RemoveOutput{
			Path:    path,
			Success: false,
		}, fmt.Errorf("failed to remove path %q: %w", path, err)
	}

	// Return clean relative path with "./" prefix
	relPath, err := filepath.Rel(baseDir, removeAbs)
	if err != nil {
		relPath = removeAbs
	}
	relSlash := "./" + filepath.ToSlash(relPath)

	return RemoveOutput{
		Path:     relSlash,
		Success:  true,
		Content:  fileContent,
		IsBinary: isBinary,
		MimeType: mimeType,
	}, nil
}

// TextContent implements tool.TextContentProvider so loom renders a clean success message.
func (o RemoveOutput) TextContent() string {
	if !o.Success {
		return fmt.Sprintf("Failed to remove %s", o.Path)
	}
	return fmt.Sprintf("Successfully removed %s", o.Path)
}
