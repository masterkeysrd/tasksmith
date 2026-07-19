package tools

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/diff"
	"github.com/masterkeysrd/tasksmith/internal/filetrack"
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

	if h.isProtectedPath(writeAbs) {
		return WriteOutput{
			Path:    path,
			Success: false,
		}, fmt.Errorf("cannot modify TaskSmith internal path: %q", path)
	}

	relPath, err := filepath.Rel(baseDir, writeAbs)
	if err != nil {
		relPath = writeAbs
	}
	relSlash := "./" + filepath.ToSlash(relPath)

	var existedBefore bool
	var oldContent string
	if info, statErr := os.Stat(writeAbs); statErr == nil && !info.IsDir() {
		existedBefore = true
		if oldBytes, readErr := os.ReadFile(writeAbs); readErr == nil {
			oldContent = string(oldBytes)
		}

		if h.FileTracker != nil {
			known, err := h.FileTracker.IsKnown(ctx, relSlash)
			if err != nil {
				return WriteOutput{
					Path:    path,
					Success: false,
				}, fmt.Errorf("failed to verify file status: %w", err)
			}
			if !known {
				return WriteOutput{
					Path:    path,
					Success: false,
				}, fmt.Errorf("the file %q has been modified externally since you last read or wrote it; you must view the file content before overwriting it", path)
			}
		}
	}

	// Create parent directories if they don't exist
	parentDir := filepath.Dir(writeAbs)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return WriteOutput{}, fmt.Errorf("failed to create directories for %q: %w", path, err)
	}

	if h.PreWriteHook != nil {
		enriched, err := h.PreWriteHook(writeAbs, in.Content)
		if err != nil {
			return WriteOutput{}, fmt.Errorf("pre-write hook failed: %w", err)
		}
		in.Content = enriched
	}

	// Write file content
	contentBytes := []byte(in.Content)
	if h.FileTracker != nil {
		hashVal := fmt.Sprintf("%x", sha256.Sum256(contentBytes))
		h.FileTracker.ExpectWrite(relSlash, hashVal)
	}
	if err := os.WriteFile(writeAbs, contentBytes, 0644); err != nil {
		return WriteOutput{
			Path:    path,
			Success: false,
		}, fmt.Errorf("failed to write file %q: %w", path, err)
	}

	var diagsStr string
	if h.LspManager != nil {
		h.LspManager.NotifyFileChanged(ctx, writeAbs, in.Content)
		diagsStr = GetFileDiagnosticsString(ctx, h.LspManager, h.CWD, writeAbs)
	}

	if h.FileTracker != nil {
		kind := filetrack.Created
		if existedBefore {
			kind = filetrack.Modified
		}
		var additions, deletions int
		var diffStr string
		if existedBefore {
			diffStr = diff.FormatUnified(relSlash, relSlash, oldContent, in.Content)
			for _, l := range strings.Split(diffStr, "\n") {
				if strings.HasPrefix(l, "--- ") || strings.HasPrefix(l, "+++ ") {
					continue
				}
				if strings.HasPrefix(l, "+") {
					additions++
				} else if strings.HasPrefix(l, "-") {
					deletions++
				}
			}
		} else {
			if in.Content != "" {
				additions = strings.Count(in.Content, "\n") + 1
			}
		}

		_ = h.FileTracker.Record(ctx, filetrack.Change{
			ToolName:  "write",
			Path:      relSlash,
			Kind:      kind,
			Additions: additions,
			Deletions: deletions,
		}, diffStr, oldContent)
	}

	return WriteOutput{
		Path:         relSlash,
		BytesWritten: len(contentBytes),
		Success:      true,
		Diagnostics:  diagsStr,
	}, nil
}

// TextContent implements tool.TextContentProvider so loom renders a clean success message.
func (o WriteOutput) TextContent() string {
	if !o.Success {
		return fmt.Sprintf("Failed to write file to %s", o.Path)
	}
	msg := fmt.Sprintf("Successfully wrote %d bytes to %s", o.BytesWritten, o.Path)
	if o.Diagnostics != "" {
		msg += "\n" + o.Diagnostics
	}
	return msg
}
