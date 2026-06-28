package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/diff"
	"github.com/masterkeysrd/tasksmith/internal/session/filetrack"
)

// Edit edits a file by replacing a target block of text with a replacement block.
func (h *ToolHandlers) Edit(ctx context.Context, in EditArgs) (EditOutput, error) {
	baseDir := h.CWD
	if baseDir == "" {
		baseDir = "."
	}
	baseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return EditOutput{}, fmt.Errorf("failed to resolve workspace directory: %w", err)
	}

	path := cleanPath(in.Path)
	var editAbs string
	if filepath.IsAbs(path) {
		editAbs = path
	} else {
		editAbs = filepath.Join(baseDir, path)
	}
	editAbs, err = filepath.Abs(editAbs)
	if err != nil {
		return EditOutput{}, fmt.Errorf("failed to resolve target path: %w", err)
	}

	if h.isProtectedPath(editAbs) {
		return EditOutput{
			Path:    path,
			Success: false,
			Message: fmt.Sprintf("cannot modify TaskSmith internal path: %q", path),
		}, fmt.Errorf("cannot modify TaskSmith internal path: %q", path)
	}

	relPath, err := filepath.Rel(baseDir, editAbs)
	if err != nil {
		relPath = editAbs
	}
	relSlash := "./" + filepath.ToSlash(relPath)

	if h.FileTracker != nil {
		known, err := h.FileTracker.IsKnown(ctx, relSlash)
		if err != nil {
			return EditOutput{
				Path:    path,
				Success: false,
				Message: fmt.Sprintf("failed to verify file status: %v", err),
			}, fmt.Errorf("failed to verify file status: %w", err)
		}
		if !known {
			return EditOutput{
				Path:    path,
				Success: false,
				Message: fmt.Sprintf("The file %q has been modified externally since you last read or wrote it. You must use the view tool to read the updated file content before editing.", path),
			}, fmt.Errorf("the file %q has been modified externally since you last read or wrote it; you must view the file content before editing", path)
		}
	}

	// Read current file content
	data, err := os.ReadFile(editAbs)
	if err != nil {
		return EditOutput{
			Path:    path,
			Success: false,
		}, fmt.Errorf("failed to read file %q: %w", path, err)
	}
	content := string(data)

	contentNorm := strings.ReplaceAll(content, "\r\n", "\n")
	newContent, count, err := SmartReplace(contentNorm, in.Target, in.Replacement, in.ReplaceAll)
	if count == 0 {
		return EditOutput{
			Path:    path,
			Success: false,
			Message: "edit failed: target block not found in file",
		}, nil
	}
	if err != nil {
		return EditOutput{
			Path:    path,
			Success: false,
			Message: fmt.Sprintf("edit failed: %v", err),
		}, nil
	}

	// Write new content back
	if err := os.WriteFile(editAbs, []byte(newContent), 0644); err != nil {
		return EditOutput{
			Path:    path,
			Success: false,
			Message: fmt.Sprintf("failed to write edited file: %v", err),
		}, nil
	}

	var diagsStr string
	if h.LspManager != nil {
		h.LspManager.NotifyFileChanged(ctx, editAbs, newContent)
		diagsStr = GetFileDiagnosticsString(ctx, h.LspManager, h.CWD, editAbs)
	}

	// Generate unified diff
	diffStr := diff.FormatUnified(relSlash, relSlash, contentNorm, newContent)

	// Compute additions and deletions
	var additions, deletions int
	diffLines := strings.Split(diffStr, "\n")
	for _, l := range diffLines {
		if strings.HasPrefix(l, "--- ") || strings.HasPrefix(l, "+++ ") {
			continue
		}
		if strings.HasPrefix(l, "+") {
			additions++
		} else if strings.HasPrefix(l, "-") {
			deletions++
		}
	}

	if h.FileTracker != nil {
		_ = h.FileTracker.Record(ctx, filetrack.Change{
			ToolName:  "edit",
			Path:      relSlash,
			Kind:      filetrack.Modified,
			Additions: additions,
			Deletions: deletions,
		}, diffStr, content)
	}

	diffVal, fullDiffVal := truncateDiff(diffStr)

	return EditOutput{
		Path:        relSlash,
		Success:     true,
		Diff:        diffVal,
		FullDiff:    fullDiffVal,
		Additions:   additions,
		Deletions:   deletions,
		Diagnostics: diagsStr,
	}, nil
}

// OverrideForHistory implements the session history overrider to save the full diff.
func (o EditOutput) OverrideForHistory() any {
	if o.FullDiff != "" {
		o.Diff = o.FullDiff
		o.FullDiff = ""
	}
	return o
}

// TextContent implements tool.TextContentProvider so loom renders the diff.
func (o EditOutput) TextContent() string {
	if !o.Success {
		if o.Message != "" {
			return o.Message
		}
		return fmt.Sprintf("Failed to edit file %s", o.Path)
	}
	msg := o.Diff
	if o.Diff == "" {
		msg = fmt.Sprintf("No changes made to %s", o.Path)
	}
	if o.Diagnostics != "" {
		msg += "\n" + o.Diagnostics
	}
	return msg
}
