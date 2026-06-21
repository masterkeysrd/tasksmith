package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/diff"
)

// MultiEdit applies multiple edits to a file, allowing partial success.
func (h *ToolHandlers) MultiEdit(ctx context.Context, in MultiEditArgs) (MultiEditOutput, error) {
	baseDir := h.CWD
	if baseDir == "" {
		baseDir = "."
	}
	baseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return MultiEditOutput{}, fmt.Errorf("failed to resolve workspace directory: %w", err)
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
		return MultiEditOutput{}, fmt.Errorf("failed to resolve target path: %w", err)
	}

	// Read current file content
	data, err := os.ReadFile(editAbs)
	if err != nil {
		return MultiEditOutput{
			Path:    path,
			Success: false,
		}, fmt.Errorf("failed to read file %q: %w", path, err)
	}
	content := string(data)

	contentNorm := strings.ReplaceAll(content, "\r\n", "\n")
	originalContent := contentNorm
	modified := false

	results := make([]MultiEditOutputResultsItem, len(in.Edits))

	for i, edit := range in.Edits {
		targetNorm := strings.ReplaceAll(edit.Target, "\r\n", "\n")
		count := strings.Count(contentNorm, targetNorm)

		if count == 0 {
			results[i] = MultiEditOutputResultsItem{
				Success: false,
				Message: "target block not found in file",
			}
			continue
		}

		if count > 1 && !edit.ReplaceAll {
			results[i] = MultiEditOutputResultsItem{
				Success: false,
				Message: fmt.Sprintf("target block matches %d occurrences (must be unique or replace_all must be true)", count),
			}
			continue
		}

		replacementNorm := strings.ReplaceAll(edit.Replacement, "\r\n", "\n")
		if edit.ReplaceAll {
			contentNorm = strings.ReplaceAll(contentNorm, targetNorm, replacementNorm)
		} else {
			contentNorm = strings.Replace(contentNorm, targetNorm, replacementNorm, 1)
		}

		results[i] = MultiEditOutputResultsItem{
			Success: true,
		}
		modified = true
	}

	var diffStr string
	var additions, deletions int

	relPath, err := filepath.Rel(baseDir, editAbs)
	if err != nil {
		relPath = editAbs
	}
	relSlash := "./" + filepath.ToSlash(relPath)

	if modified {
		// Write the partially or fully modified content back
		if err := os.WriteFile(editAbs, []byte(contentNorm), 0644); err != nil {
			return MultiEditOutput{
				Path:    relSlash,
				Success: false,
			}, fmt.Errorf("failed to write edited file: %w", err)
		}

		diffStr = diff.FormatUnified(relSlash, relSlash, originalContent, contentNorm)

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
	}

	diffVal, fullDiffVal := truncateDiff(diffStr)

	return MultiEditOutput{
		Path:      relSlash,
		Success:   modified,
		Diff:      diffVal,
		FullDiff:  fullDiffVal,
		Additions: additions,
		Deletions: deletions,
		Results:   results,
	}, nil
}

// OverrideForHistory implements the session history overrider to save the full diff.
func (o MultiEditOutput) OverrideForHistory() any {
	if o.FullDiff != "" {
		o.Diff = o.FullDiff
		o.FullDiff = ""
	}
	return o
}

// TextContent implements tool.TextContentProvider so loom renders the diff.
func (o MultiEditOutput) TextContent() string {
	var failedMsgs []string
	for i, r := range o.Results {
		if !r.Success {
			failedMsgs = append(failedMsgs, fmt.Sprintf("Edit #%d failed: %s", i+1, r.Message))
		}
	}

	var sb strings.Builder
	if o.Success && o.Diff != "" {
		sb.WriteString(o.Diff)
		if len(failedMsgs) > 0 {
			sb.WriteString("\n\n--- Warnings / Failed Edits ---\n")
			sb.WriteString(strings.Join(failedMsgs, "\n"))
		}
	} else {
		if len(failedMsgs) > 0 {
			return strings.Join(failedMsgs, "\n")
		}
		return fmt.Sprintf("Failed to edit file %s", o.Path)
	}
	return sb.String()
}
