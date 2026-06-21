package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Write writes content to a file. It creates any parent directories if they
// do not exist.
func (h *ToolHandlers) Write(ctx context.Context, in WriteArgs) (WriteOutput, error) {
	baseDir := h.CWD
	if baseDir == "" {
		baseDir = "."
	}
	baseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return WriteOutput{}, fmt.Errorf("failed to resolve workspace directory: %w", err)
	}

	path := cleanPath(in.Path)
	var writeAbs string
	if filepath.IsAbs(path) {
		writeAbs = path
	} else {
		writeAbs = filepath.Join(baseDir, path)
	}
	writeAbs, err = filepath.Abs(writeAbs)
	if err != nil {
		return WriteOutput{}, fmt.Errorf("failed to resolve target path: %w", err)
	}

	// Create parent directories if they don't exist
	parentDir := filepath.Dir(writeAbs)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return WriteOutput{}, fmt.Errorf("failed to create directories for %q: %w", path, err)
	}

	// Write file content
	contentBytes := []byte(in.Content)
	if err := os.WriteFile(writeAbs, contentBytes, 0644); err != nil {
		return WriteOutput{
			Path:    path,
			Success: false,
		}, fmt.Errorf("failed to write file %q: %w", path, err)
	}

	// Return clean relative path with "./" prefix
	relPath, err := filepath.Rel(baseDir, writeAbs)
	if err != nil {
		relPath = writeAbs
	}
	relSlash := "./" + filepath.ToSlash(relPath)

	return WriteOutput{
		Path:         relSlash,
		BytesWritten: len(contentBytes),
		Success:      true,
	}, nil
}

// TextContent implements tool.TextContentProvider so loom renders a clean success message.
func (o WriteOutput) TextContent() string {
	if !o.Success {
		return fmt.Sprintf("Failed to write file to %s", o.Path)
	}
	return fmt.Sprintf("Successfully wrote %d bytes to %s", o.BytesWritten, o.Path)
}
