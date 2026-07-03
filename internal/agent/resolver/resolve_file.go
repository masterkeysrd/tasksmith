package resolver

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	corefs "github.com/masterkeysrd/tasksmith/internal/core/fs"
	"github.com/masterkeysrd/tasksmith/internal/core/lsp"
)

const (
	MaxTotalChars = 16000
	MaxLineChars  = 500
)

// ResolvePath resolves a partial or relative filepath to an absolute filesystem path
// without reading any content. It handles line range extraction, spacing normalization,
// and fuzzy matching. This is the cheap first phase of two-phase resolution.
func (r *Resolver) ResolvePath(ctx context.Context, inputPath string) (string, error) {
	cleanedInput := strings.Trim(inputPath, "\"'` ")

	// Extract optional line range anchors (e.g. #L10-L20)
	targetPath, _, _ := parseLineRange(cleanedInput)

	resolvedPath := targetPath
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(r.Cwd, resolvedPath)
	}

	// 1. If it doesn't exist directly, search recursively using fuzzy matching and spacing normalization
	if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
		// A. Try spacing normalization in the target directory first (matching view tool behavior)
		dir := filepath.Dir(resolvedPath)
		base := filepath.Base(resolvedPath)
		normalizedBaseLower := strings.ToLower(normalizeSpacing(base))

		found := false
		if files, readErr := os.ReadDir(dir); readErr == nil {
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				if strings.ToLower(normalizeSpacing(f.Name())) == normalizedBaseLower {
					resolvedPath = filepath.Join(dir, f.Name())
					found = true
					break
				}
			}
		}

		// B. Fall back to recursive fuzzy find
		if !found {
			bestMatch, err := r.fuzzyFindFile(targetPath)
			if err != nil || bestMatch == "" {
				return "", fmt.Errorf("file not found: %s (no such file or directory)", targetPath)
			}
			resolvedPath = bestMatch
		}
	}

	absPath, err := filepath.Abs(resolvedPath)
	if err != nil {
		absPath = resolvedPath
	}

	return absPath, nil
}

// loadResourceWithPath loads content and metadata for a known absolute path with
// optional line range. This is the internal helper used by ResolveFile.
func (r *Resolver) loadResourceWithPath(ctx context.Context, absPath string, startLine, endLine int) (ResolvedResource, error) {
	mimeType := corefs.DetectMIMEType(absPath)
	isBinary := corefs.IsBinaryMIME(mimeType)

	var content string
	var totalLines, actualEndLine int
	var truncated bool
	var cachedPath string

	if !isBinary {
		var err error
		content, totalLines, actualEndLine, truncated, err = r.readTextFile(absPath, startLine, endLine)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}

		if r.Lsp != nil {
			r.Lsp.NotifyFileOpened(ctx, absPath)
		}
	} else if r.Storage != nil {
		file, err := os.Open(absPath)
		if err == nil {
			defer file.Close()
			filename := filepath.Base(absPath)

			toolCallID, _ := ctx.Value("tool_call_id").(string)
			var storagePath string
			if toolCallID != "" {
				storagePath = fmt.Sprintf("%s_%s", toolCallID, filename)
			} else {
				storagePath = fmt.Sprintf("attach_%s", filename)
			}

			if cached, errSave := r.Storage.Save(ctx, storagePath, file); errSave == nil {
				cachedPath = cached
			}
		}
	}

	if r.FileTracker != nil {
		if rel, err := filepath.Rel(r.Cwd, absPath); err == nil {
			_ = r.FileTracker.RecordRead(ctx, "./"+filepath.ToSlash(rel))
		}
	}

	var diags []lsp.Diagnostic
	if r.Lsp != nil && !isBinary {
		if client, err := r.Lsp.GetClient(ctx, r.Cwd); err == nil && client != nil {
			if fileDiags, err := client.GetDiagnostics(ctx, absPath); err == nil {
				diags = fileDiags
			}
		}
	}

	return &ResolvedFile{
		FilePath:    absPath,
		Content:     content,
		StartLine:   startLine,
		EndLine:     actualEndLine,
		TotalLines:  totalLines,
		Truncated:   truncated,
		MimeType:    mimeType,
		IsBinary:    isBinary,
		CachedPath:  cachedPath,
		Diagnostics: diags,
	}, nil
}

// LoadResource loads content and metadata for a known, verified absolute path.
// This is the expensive second phase of two-phase resolution. It reads file content,
// retrieves LSP diagnostics, records the file read in the tracker, and handles
// binary file caching.
func (r *Resolver) LoadResource(ctx context.Context, absPath string) (ResolvedResource, error) {
	// Verify file exists before attempting to read
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("file not found: %s", absPath)
	}

	// MIME type check to detect binary vs text files
	mimeType := corefs.DetectMIMEType(absPath)
	isBinary := corefs.IsBinaryMIME(mimeType)

	var content string
	var totalLines, actualEndLine int
	var truncated bool
	var cachedPath string

	if !isBinary {
		var err error
		content, totalLines, actualEndLine, truncated, err = r.readTextFile(absPath, 1, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}

		// Notify LSP server of file open event (only for text files, matching view tool)
		if r.Lsp != nil {
			r.Lsp.NotifyFileOpened(ctx, absPath)
		}
	} else if r.Storage != nil {
		file, err := os.Open(absPath)
		if err == nil {
			defer file.Close()
			filename := filepath.Base(absPath)

			// Generate storage path: use context-based tool_call_id if present
			toolCallID, _ := ctx.Value("tool_call_id").(string)
			var storagePath string
			if toolCallID != "" {
				storagePath = fmt.Sprintf("%s_%s", toolCallID, filename)
			} else {
				storagePath = fmt.Sprintf("attach_%s", filename)
			}

			if cached, errSave := r.Storage.Save(ctx, storagePath, file); errSave == nil {
				cachedPath = cached
			}
		}
	}

	// Record file read in the file tracker (updates autocomplete and active files list)
	if r.FileTracker != nil {
		if rel, err := filepath.Rel(r.Cwd, absPath); err == nil {
			_ = r.FileTracker.RecordRead(ctx, "./"+filepath.ToSlash(rel))
		}
	}

	// Retrieve raw LSP diagnostics (errors/warnings) for this file
	var diags []lsp.Diagnostic
	if r.Lsp != nil && !isBinary {
		if client, err := r.Lsp.GetClient(ctx, r.Cwd); err == nil && client != nil {
			if fileDiags, err := client.GetDiagnostics(ctx, absPath); err == nil {
				diags = fileDiags
			}
		}
	}

	return &ResolvedFile{
		FilePath:    absPath,
		Content:     content,
		StartLine:   1,
		EndLine:     actualEndLine,
		TotalLines:  totalLines,
		Truncated:   truncated,
		MimeType:    mimeType,
		IsBinary:    isBinary,
		CachedPath:  cachedPath,
		Diagnostics: diags,
	}, nil
}

// ResolveReferences resolves a user message text by extracting references, resolving paths,
// deduplicating, and loading content in a single pipeline. This is the primary entry point
// for Phase 4 two-phase resolution.
func (r *Resolver) ResolveReferences(ctx context.Context, text string, trackedRefs []Reference) ([]ResolvedResource, error) {
	// 1. Extract manual references not already tracked
	manualRefs := ExtractReferences(text, trackedRefs)

	// 2. Resolve paths only for manual refs (cheap)
	for i, ref := range manualRefs {
		fullPath, err := r.ResolvePath(ctx, ref.Value)
		if err != nil {
			continue // skip unresolvable
		}
		manualRefs[i].Value = fullPath
	}

	// 3. Dedup ALL refs by (Type + full path) — BEFORE content loading
	seen := make(map[string]bool)
	var unique []Reference
	for _, ref := range append(trackedRefs, manualRefs...) {
		key := string(ref.Type) + ":" + ref.Value
		if !seen[key] {
			seen[key] = true
			unique = append(unique, ref)
		}
	}

	// 4. Load content ONLY for the unique set
	var resources []ResolvedResource
	for _, ref := range unique {
		res, err := r.LoadResource(ctx, ref.Value)
		if err != nil {
			continue
		}
		resources = append(resources, res)
	}

	return resources, nil
}

// ResolveFile resolves a partial or relative filepath, reads its raw data safely within limits,
// and returns a ResolvedFile type. Supports line range hashes (e.g., "view.go#L10-L20").
//
// Deprecated: Use ResolvePath followed by LoadResource for two-phase resolution with deduplication.
func (r *Resolver) ResolveFile(ctx context.Context, inputPath string) (ResolvedResource, error) {
	cleanedInput := strings.Trim(inputPath, "\"'` ")
	targetPath, startLine, endLine := parseLineRange(cleanedInput)

	resolvedPath := targetPath
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(r.Cwd, resolvedPath)
	}

	if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
		dir := filepath.Dir(resolvedPath)
		base := filepath.Base(resolvedPath)
		normalizedBaseLower := strings.ToLower(normalizeSpacing(base))

		found := false
		if files, readErr := os.ReadDir(dir); readErr == nil {
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				if strings.ToLower(normalizeSpacing(f.Name())) == normalizedBaseLower {
					resolvedPath = filepath.Join(dir, f.Name())
					found = true
					break
				}
			}
		}

		if !found {
			bestMatch, err := r.fuzzyFindFile(targetPath)
			if err != nil || bestMatch == "" {
				return nil, fmt.Errorf("file not found: %s (no such file or directory)", targetPath)
			}
			resolvedPath = bestMatch
		}
	}

	absPath, err := filepath.Abs(resolvedPath)
	if err != nil {
		absPath = resolvedPath
	}

	return r.loadResourceWithPath(ctx, absPath, startLine, endLine)
}

// parseLineRange extracts line number bounds from a path string containing a "#L<start>-L<end>" anchor.
func parseLineRange(inputPath string) (cleanPath string, startLine, endLine int) {
	parts := strings.Split(inputPath, "#")
	if len(parts) != 2 {
		return inputPath, 1, 0
	}

	cleanPath = parts[0]
	hash := parts[1]

	if strings.HasPrefix(hash, "L") {
		hash = hash[1:]
		if before, after, ok := strings.Cut(hash, "-L"); ok {
			startStr := before
			endStr := after
			var s, e int
			fmt.Sscanf(startStr, "%d", &s)
			fmt.Sscanf(endStr, "%d", &e)
			return cleanPath, s, e
		} else {
			var s int
			fmt.Sscanf(hash, "%d", &s)
			return cleanPath, s, 0
		}
	}

	return inputPath, 1, 0
}

// readTextFile safely reads a file line-by-line from disk, enforcing size limit boundaries and line bounds.
func (r *Resolver) readTextFile(path string, startLine, endLine int) (content string, totalLines int, lastLineRead int, truncated bool, err error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, 0, false, err
	}
	defer file.Close()

	var lines []string
	reader := bufio.NewReader(file)
	currentLine := 0
	totalChars := 0

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			currentLine++
			if !truncated && currentLine >= startLine && (endLine == 0 || currentLine <= endLine) {
				trimmedLine := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
				charCount := len(trimmedLine)

				// Truncate minified or extremely long individual lines
				var contentToAppend string
				if charCount > MaxLineChars {
					contentToAppend = trimmedLine[:MaxLineChars] + fmt.Sprintf(" ... [Line %d truncated: %d characters of minified/dense data]", currentLine, charCount)
				} else {
					contentToAppend = trimmedLine
				}

				lineLength := len(contentToAppend)
				if len(lines) > 0 {
					lineLength += 1 // account for "\n" separator
				}

				if totalChars+lineLength > MaxTotalChars {
					truncated = true
				} else {
					lines = append(lines, contentToAppend)
					totalChars += lineLength
					lastLineRead = currentLine
				}
			}
		}
		if err != nil {
			break
		}
	}

	return strings.Join(lines, "\n"), currentLine, lastLineRead, truncated, nil
}

// fuzzyFindFile recursively walks the workspace root to match a partial path or filename,
// ignoring directories in .gitignore or default ignore lists.
func (r *Resolver) fuzzyFindFile(query string) (string, error) {
	ign, _ := corefs.NewIgnorer(r.Cwd)

	var matches []string
	queryLower := strings.ToLower(query)

	err := filepath.WalkDir(r.Cwd, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip files with read errors
		}

		name := d.Name()
		if ign != nil && ign.ShouldIgnore(name, path, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		pathLower := strings.ToLower(path)
		if strings.HasSuffix(pathLower, queryLower) || strings.Contains(strings.ToLower(name), queryLower) {
			matches = append(matches, path)
		}

		// Limit matches to avoid running out of memory
		if len(matches) > 100 {
			return filepath.SkipAll
		}
		return nil
	})

	if err != nil && err != filepath.SkipAll {
		return "", err
	}

	if len(matches) == 0 {
		return "", nil
	}

	// Choose the best match (favor exact filename match, then shorter relative paths)
	best := matches[0]
	for _, m := range matches {
		if strings.ToLower(filepath.Base(m)) == queryLower {
			return m, nil
		}
		if len(m) < len(best) {
			best = m
		}
	}
	return best, nil
}

func normalizeSpacing(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r == ' ' || r == '\u202f' || r == '\u00a0' || (r >= '\u2000' && r <= '\u200a') {
			sb.WriteRune(' ')
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
