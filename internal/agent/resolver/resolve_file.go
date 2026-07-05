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
	"github.com/masterkeysrd/warp"
)

const (
	MaxTotalChars = 32000
	MaxLineChars  = 500
)

// ResolvePath resolves a partial or relative filepath to an absolute filesystem path
// without reading any content. It handles line range extraction, spacing normalization,
// and fuzzy matching. This is the cheap first phase of two-phase resolution.
func (r *Resolver) ResolvePath(ctx context.Context, inputPath string, resType ResourceType, agentName string) (string, error) {
	if resType == TypeSymbol {
		_, _, _, _, _, ok := parseCoordinates(inputPath)
		return r.ResolveSymbol(ctx, inputPath, ok)
	}

	if resType == TypeSkill {
		return r.resolveSkillPath(ctx, inputPath, agentName)
	}

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

// LoadResource loads content and metadata for a known, verified absolute path/coordinates.
// This is the expensive second phase of two-phase resolution.
func (r *Resolver) LoadResource(ctx context.Context, value string, resType ResourceType, agentName string) (ResolvedResource, error) {
	switch resType {
	case TypeFile:
		return r.loadResourceFile(ctx, value)
	case TypeSymbol:
		return r.loadResourceSymbol(ctx, value)
	case TypeSkill:
		return r.loadResourceSkill(ctx, value, agentName)
	default:
		return nil, fmt.Errorf("unsupported resource type for LoadResource: %s", resType)
	}
}

// loadResourceFile loads content and metadata for a known, verified absolute path.
// This reads file content, retrieves LSP diagnostics, records the file read in the tracker,
// and handles binary file caching.
func (r *Resolver) loadResourceFile(ctx context.Context, value string) (ResolvedResource, error) {
	absPath, startLine, endLine := parseLineRange(value)

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
		content, totalLines, actualEndLine, truncated, err = r.readTextFile(absPath, startLine, endLine)
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

// ResolveReferences resolves a user message text by extracting references, resolving paths,
// deduplicating, and loading content in a single pipeline. This is the primary entry point
// for Phase 4 two-phase resolution.
func (r *Resolver) ResolveReferences(ctx context.Context, text string, trackedRefs []Reference, agentName string) ([]ResolvedResource, error) {
	// 1. Extract manual references not already tracked
	manualRefs := ExtractReferences(text, trackedRefs)

	// 2. Resolve paths for both tracked and manual refs to ensure all paths are absolute (cheap)
	allRefs := append(trackedRefs, manualRefs...)
	for i := range allRefs {
		if allRefs[i].Type == TypeFile {
			cleanVal, start, end := parseLineRange(allRefs[i].Value)
			allRefs[i].Value = cleanVal
			if allRefs[i].StartLine == 0 {
				allRefs[i].StartLine = start
			}
			if allRefs[i].EndLine == 0 {
				allRefs[i].EndLine = end
			}
		}

		fullPath, err := r.ResolvePath(ctx, allRefs[i].Value, allRefs[i].Type, agentName)
		if err != nil {
			continue // skip unresolvable
		}
		allRefs[i].Value = fullPath
	}

	// Track which files are loaded as a whole file to optimize token usage
	wholeFiles := make(map[string]bool)
	for _, ref := range allRefs {
		if ref.Type == TypeFile && ref.StartLine == 1 && ref.EndLine == 0 {
			wholeFiles[ref.Value] = true
		}
	}

	// 3. Dedup ALL refs by Type + path + line range — BEFORE content loading,
	// applying token optimization to drop specific ranges if the whole file is loaded.
	seen := make(map[string]bool)
	var unique []Reference
	for _, ref := range allRefs {
		if ref.Type == TypeFile && wholeFiles[ref.Value] && (ref.StartLine != 1 || ref.EndLine != 0) {
			continue
		}

		key := fmt.Sprintf("%s:%s:%d:%d", ref.Type, ref.Value, ref.StartLine, ref.EndLine)
		if !seen[key] {
			seen[key] = true
			unique = append(unique, ref)
		}
	}

	// Load content ONLY for the unique set
	var resources []ResolvedResource
	for _, ref := range unique {
		val := ref.Value
		if ref.Type == TypeFile {
			if ref.EndLine > 0 {
				val = fmt.Sprintf("%s#L%d-L%d", val, ref.StartLine, ref.EndLine)
			} else {
				val = fmt.Sprintf("%s#L%d", val, ref.StartLine)
			}
		}
		res, err := r.LoadResource(ctx, val, ref.Type, agentName)
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

// findAgentSkill looks up a skill in the agent's assigned skills list.
func (r *Resolver) findAgentSkill(ctx context.Context, skillName string, agentName string) (*warp.Skill, *warp.ResolvedAgent, error) {
	if r.Workspace == nil {
		return nil, nil, fmt.Errorf("workspace not available")
	}
	if agentName == "" {
		return nil, nil, fmt.Errorf("agent name required")
	}

	resolvedAgent, err := r.Workspace.ResolveAgent(ctx, agentName)
	if err != nil {
		return nil, nil, err
	}

	for _, skill := range resolvedAgent.Skills {
		if skill.Metadata.Name == skillName ||
			filepath.Base(skill.Directory) == skillName ||
			strings.TrimSuffix(skill.Metadata.Name, filepath.Ext(skill.Metadata.Name)) == skillName {
			return &skill, resolvedAgent, nil
		}
	}
	return nil, nil, fmt.Errorf("skill %q not found for agent %q", skillName, agentName)
}

// resolveSkillPath resolves a skill name to its absolute file path using the workspace agent resolver.
// It searches the active agent's skills list by name, basename, or name without extension.
func (r *Resolver) resolveSkillPath(ctx context.Context, skillName string, agentName string) (string, error) {
	skill, _, err := r.findAgentSkill(ctx, skillName, agentName)
	if err != nil {
		return "", err
	}
	path := skill.Directory
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.Cwd, path)
	}
	return filepath.Clean(path), nil
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
