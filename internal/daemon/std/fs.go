package std

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/diff"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/ripgrep"
	"github.com/masterkeysrd/tasksmith/internal/core/strutils"
)

// View reads a text file from the sandbox filesystem, records its SHA-256
// content hash in the FileTracker (emitting a SourceExternal change if a
// previously tracked file was modified outside the agent), and returns the
// requested line range with 1-based line numbers.
func (s *Std) View(ctx context.Context, req ViewRequest) (*ViewResult, error) {
	if strings.TrimSpace(req.Path) == "" {
		return nil, fmt.Errorf("%w: path is required", ErrInvalidArgument)
	}
	normPath := s.normalizePath(req.Path)
	if normPath == "." {
		return nil, fmt.Errorf("%w: path must refer to a file", ErrInvalidArgument)
	}

	data, err := s.sandbox.FS().ReadFile(ctx, normPath)
	if err != nil {
		return nil, err
	}
	if isBinaryContent(data) {
		return nil, fmt.Errorf("%w: %s", ErrBinaryFile, normPath)
	}

	hash := sha256Hex(data)

	if prevHash, ok := s.tracker.LastHash(normPath); ok && prevHash != "" && prevHash != hash {
		if prevContent, hasPriorChange := s.tracker.LastContent(normPath); hasPriorChange {
			add, del := diff.Stats(string(prevContent), string(data))
			change := FileChange{
				Path:          normPath,
				Source:        SourceExternal,
				Tool:          "external",
				Kind:          KindModified,
				BeforeHash:    prevHash,
				AfterHash:     hash,
				BeforeContent: prevContent,
				AfterContent:  data,
				Additions:     add,
				Deletions:     del,
				Timestamp:     time.Now().UTC(),
			}
			if err := s.tracker.RecordChange(ctx, &change); err != nil {
				return nil, err
			}
		}
	}

	if err := s.tracker.RecordRead(ctx, normPath, hash); err != nil {
		return nil, err
	}

	lines := splitTextLines(string(data))
	totalLines := len(lines)

	offset := req.Offset
	if offset <= 0 {
		offset = 1
	}
	limit := req.Limit
	if limit <= 0 {
		limit = DefaultViewLimit
	} else if limit > MaxViewLimit {
		limit = MaxViewLimit
	}

	res := &ViewResult{
		Path:       normPath,
		TotalLines: totalLines,
		Hash:       hash,
	}

	if totalLines == 0 || offset > totalLines {
		return res, nil
	}

	startIdx := offset - 1
	endIdx := startIdx + limit
	if endIdx < totalLines {
		res.Truncated = true
	} else {
		endIdx = totalLines
	}

	var selected []string
	var contentBytes int
	for i := startIdx; i < endIdx; i++ {
		line, lineTrunc := strutils.TruncateLine(lines[i], MaxViewLineBytes, " ... [line truncated]")
		if lineTrunc {
			res.Truncated = true
		}
		addedBytes := len(line)
		if len(selected) > 0 {
			addedBytes++
		}
		if len(selected) > 0 && contentBytes+addedBytes > MaxViewBytes {
			res.Truncated = true
			break
		}
		selected = append(selected, line)
		contentBytes += addedBytes
	}

	res.StartLine = startIdx + 1
	res.EndLine = startIdx + len(selected)
	res.Content = strings.Join(selected, "\n")

	width := len(strconv.Itoa(res.EndLine))
	var numbered strings.Builder
	for i, line := range selected {
		if i > 0 {
			numbered.WriteByte('\n')
		}
		fmt.Fprintf(&numbered, "%*d\t%s", width, startIdx+i+1, line)
	}
	res.Numbered = numbered.String()

	return res, nil
}

// List enumerates directory entries within the sandbox filesystem.
func (s *Std) List(ctx context.Context, req ListRequest) (*ListResult, error) {
	if err := validateGlobPatterns(req.Ignore); err != nil {
		return nil, err
	}

	basePath := s.normalizePath(req.Path)
	info, err := s.sandbox.FS().Stat(ctx, basePath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a directory", ErrInvalidArgument, basePath)
	}

	res := &ListResult{
		Path:    basePath,
		Entries: []ListEntry{},
	}

	limit := req.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	} else if limit > MaxListLimit {
		limit = MaxListLimit
	}
	var totalBytes int

	var walk func(currDir string, depth int) error
	walk = func(currDir string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		dirEntries, err := s.sandbox.FS().ReadDir(ctx, currDir)
		if err != nil {
			return err
		}

		for _, de := range dirEntries {
			name := de.Name()
			if !req.IncludeHidden && strings.HasPrefix(name, ".") {
				continue
			}

			entryPath := name
			if currDir != "." {
				entryPath = path.Join(currDir, name)
			}
			relToBase := entryPath
			if basePath != "." {
				relToBase = strings.TrimPrefix(entryPath, basePath+"/")
			}

			if matchesAnyIgnore(req.Ignore, name, relToBase, entryPath) {
				continue
			}

			entryBytes := len(entryPath) + len(name) + 48
			if len(res.Entries) >= limit || (len(res.Entries) > 0 && totalBytes+entryBytes > MaxListBytes) {
				res.Truncated = true
				return nil
			}

			fi, err := de.Info()
			if err != nil {
				return err
			}

			res.Entries = append(res.Entries, ListEntry{
				Path:    entryPath,
				Name:    name,
				IsDir:   de.IsDir(),
				Size:    fi.Size(),
				Mode:    fi.Mode(),
				ModTime: fi.ModTime().UTC(),
			})
			totalBytes += entryBytes

			if de.IsDir() && req.Recursive {
				if req.MaxDepth <= 0 || depth < req.MaxDepth {
					if err := walk(entryPath, depth+1); err != nil {
						return err
					}
					if res.Truncated {
						return nil
					}
				}
			}
		}
		return nil
	}

	if err := walk(basePath, 1); err != nil {
		return nil, err
	}

	return res, nil
}

// Glob walks the sandbox filesystem starting from req.Path and returns all file
// paths matching req.Pattern (including ** multi-directory wildcards).
// It skips .git and .bare directories by default.
func (s *Std) Glob(ctx context.Context, req GlobRequest) (*GlobResult, error) {
	rawPattern := strings.TrimSpace(req.Pattern)
	if rawPattern == "" {
		return nil, fmt.Errorf("%w: glob pattern is required", ErrInvalidArgument)
	}
	if err := validateGlobPattern(rawPattern); err != nil {
		return nil, err
	}
	if err := validateGlobPatterns(req.Ignore); err != nil {
		return nil, err
	}
	normPattern := strings.TrimPrefix(rawPattern, "./")
	if !strings.Contains(normPattern, "/") {
		normPattern = "**/" + normPattern
	}
	g, err := fsutils.Compile(normPattern)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}
	var dirGlob *fsutils.Glob
	if !strings.HasSuffix(normPattern, "/*") && !strings.HasSuffix(normPattern, "**") {
		dirGlob, _ = fsutils.Compile(strings.TrimSuffix(normPattern, "/") + "/**")
	}

	basePath := s.normalizePath(req.Path)
	info, err := s.sandbox.FS().Stat(ctx, basePath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a directory", ErrInvalidArgument, basePath)
	}

	res := &GlobResult{
		Pattern: rawPattern,
		Path:    basePath,
		Matches: []string{},
	}

	limit := req.Limit
	if limit <= 0 {
		limit = DefaultGlobLimit
	} else if limit > MaxGlobLimit {
		limit = MaxGlobLimit
	}
	var totalBytes int

	includeHidden := req.IncludeHidden || patternTargetsHidden(rawPattern)

	if s.rg != nil && s.rg.IsAvailable(ctx) && !filepath.IsAbs(basePath) {
		rawMatches, err := s.rg.Glob(ctx, basePath, normPattern, ripgrep.GlobOptions{
			IncludeHidden: includeHidden,
			Ignore:        req.Ignore,
		})
		if err == nil {
			for _, entryPath := range rawMatches {
				relToBase := entryPath
				if basePath != "." {
					relToBase = strings.TrimPrefix(entryPath, basePath+"/")
				}

				if matchesAnyIgnore(req.Ignore, path.Base(entryPath), relToBase, entryPath) {
					continue
				}

				if !includeHidden && hasDotSegment(relToBase) && !patternTargetsHidden(rawPattern) {
					continue
				}

				matched := g.Match(relToBase) || (dirGlob != nil && dirGlob.Match(relToBase)) || (basePath != "." && (g.Match(entryPath) || (dirGlob != nil && dirGlob.Match(entryPath))))
				if matched {
					entryBytes := len(entryPath) + 4
					if len(res.Matches) >= limit || (len(res.Matches) > 0 && totalBytes+entryBytes > MaxGlobBytes) {
						res.Truncated = true
						break
					}
					res.Matches = append(res.Matches, entryPath)
					totalBytes += entryBytes
				}
			}
			sort.Strings(res.Matches)
			return res, nil
		}
	}

	var walk func(currDir string) error
	walk = func(currDir string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		dirEntries, err := s.sandbox.FS().ReadDir(ctx, currDir)
		if err != nil {
			return err
		}

		for _, de := range dirEntries {
			name := de.Name()
			if name == ".git" || name == ".bare" {
				continue
			}
			if !includeHidden && strings.HasPrefix(name, ".") {
				continue
			}

			entryPath := name
			if currDir != "." {
				entryPath = path.Join(currDir, name)
			}
			relToBase := entryPath
			if basePath != "." {
				relToBase = strings.TrimPrefix(entryPath, basePath+"/")
			}

			if matchesAnyIgnore(req.Ignore, name, relToBase, entryPath) {
				continue
			}

			if de.IsDir() {
				if err := walk(entryPath); err != nil {
					return err
				}
				if res.Truncated {
					return nil
				}
				continue
			}

			matched := g.Match(relToBase) || (dirGlob != nil && dirGlob.Match(relToBase))
			if !matched && basePath != "." {
				matched = g.Match(entryPath) || (dirGlob != nil && dirGlob.Match(entryPath))
			}
			if matched {
				entryBytes := len(entryPath) + 4
				if len(res.Matches) >= limit || (len(res.Matches) > 0 && totalBytes+entryBytes > MaxGlobBytes) {
					res.Truncated = true
					return nil
				}
				res.Matches = append(res.Matches, entryPath)
				totalBytes += entryBytes
			}
		}
		return nil
	}

	if err := walk(basePath); err != nil {
		return nil, err
	}

	sort.Strings(res.Matches)
	return res, nil
}

// Grep searches text files under req.Path for lines matching req.Pattern.
func (s *Std) Grep(ctx context.Context, req GrepRequest) (*GrepResult, error) {
	if req.Pattern == "" {
		return nil, fmt.Errorf("%w: grep pattern is required", ErrInvalidArgument)
	}
	if req.Include != "" {
		if err := validateGlobPattern(req.Include); err != nil {
			return nil, err
		}
	}

	expr := req.Pattern
	if req.CaseInsensitive && !strings.HasPrefix(expr, "(?i)") {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid regex pattern %q: %v", ErrInvalidArgument, req.Pattern, err)
	}

	basePath := s.normalizePath(req.Path)
	info, err := s.sandbox.FS().Stat(ctx, basePath)
	if err != nil {
		return nil, err
	}

	res := &GrepResult{
		Pattern: req.Pattern,
		Matches: []GrepMatch{},
	}

	limit := req.Limit
	if limit <= 0 {
		limit = DefaultGrepLimit
	} else if limit > MaxGrepLimit {
		limit = MaxGrepLimit
	}
	var totalBytes int

	if s.rg != nil && s.rg.IsAvailable(ctx) && !filepath.IsAbs(basePath) && req.ContextLines == 0 {
		rgMatches, err := s.rg.Search(ctx, basePath, req.Pattern, ripgrep.SearchOptions{
			Include:         req.Include,
			CaseInsensitive: req.CaseInsensitive,
			IncludeHidden:   req.IncludeHidden,
		})
		if err == nil {
			for _, m := range rgMatches {
				text, _ := strutils.TruncateLine(m.Content, MaxGrepLineBytes, "...")
				matchBytes := len(m.Path) + len(text) + 32
				if len(res.Matches) >= limit || (len(res.Matches) > 0 && totalBytes+matchBytes > MaxGrepBytes) {
					res.Truncated = true
					break
				}
				res.Matches = append(res.Matches, GrepMatch{
					Path:   m.Path,
					Line:   m.Line,
					Column: m.Column,
					Text:   text,
				})
				totalBytes += matchBytes
			}
			return res, nil
		}
	}

	searchFile := func(filePath, relToBase string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if req.Include != "" {
			if !matchesIncludeGlob(req.Include, path.Base(filePath), relToBase, filePath) {
				return nil
			}
		}

		data, err := s.sandbox.FS().ReadFile(ctx, filePath)
		if err != nil {
			return err
		}
		if isBinaryContent(data) {
			return nil
		}

		lines := splitTextLines(string(data))
		for i, line := range lines {
			loc := re.FindStringIndex(line)
			if loc == nil {
				continue
			}

			text, _ := strutils.TruncateLine(line, MaxGrepLineBytes, "...")
			match := GrepMatch{
				Path:   filePath,
				Line:   i + 1,
				Column: loc[0] + 1,
				Text:   text,
			}
			matchBytes := len(filePath) + len(text) + 32

			if req.ContextLines > 0 {
				startCtx := max(i-req.ContextLines, 0)
				if startCtx < i {
					before := make([]string, i-startCtx)
					for j, ctxLine := range lines[startCtx:i] {
						before[j], _ = strutils.TruncateLine(ctxLine, MaxGrepLineBytes, "...")
						matchBytes += len(before[j]) + 4
					}
					match.Before = before
				}

				endCtx := min(i+1+req.ContextLines, len(lines))
				if i+1 < endCtx {
					after := make([]string, endCtx-(i+1))
					for j, ctxLine := range lines[i+1 : endCtx] {
						after[j], _ = strutils.TruncateLine(ctxLine, MaxGrepLineBytes, "...")
						matchBytes += len(after[j]) + 4
					}
					match.After = after
				}
			}

			if len(res.Matches) >= limit || (len(res.Matches) > 0 && totalBytes+matchBytes > MaxGrepBytes) {
				res.Truncated = true
				return nil
			}

			res.Matches = append(res.Matches, match)
			totalBytes += matchBytes
		}
		return nil
	}

	if !info.IsDir() {
		if err := searchFile(basePath, path.Base(basePath)); err != nil {
			return nil, err
		}
		return res, nil
	}

	var walk func(currDir string) error
	walk = func(currDir string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		dirEntries, err := s.sandbox.FS().ReadDir(ctx, currDir)
		if err != nil {
			return err
		}

		for _, de := range dirEntries {
			name := de.Name()
			if name == ".git" || name == ".bare" {
				continue
			}
			if !req.IncludeHidden && strings.HasPrefix(name, ".") {
				continue
			}

			entryPath := name
			if currDir != "." {
				entryPath = path.Join(currDir, name)
			}
			relToBase := entryPath
			if basePath != "." {
				relToBase = strings.TrimPrefix(entryPath, basePath+"/")
			}

			if de.IsDir() {
				if err := walk(entryPath); err != nil {
					return err
				}
				if res.Truncated {
					return nil
				}
				continue
			}

			if err := searchFile(entryPath, relToBase); err != nil {
				return err
			}
			if res.Truncated {
				return nil
			}
		}
		return nil
	}

	if err := walk(basePath); err != nil {
		return nil, err
	}

	return res, nil
}

// Write creates a new file or overwrites an existing file within the sandbox.
// When overwriting an existing file and !req.Force, Write enforces
// Read-Before-Overwrite (ErrFileNotRead) and Compare-And-Swap (ErrFileModified).
func (s *Std) Write(ctx context.Context, req WriteRequest) (*WriteResult, error) {
	if strings.TrimSpace(req.Path) == "" {
		return nil, fmt.Errorf("%w: path is required", ErrInvalidArgument)
	}
	normPath := s.normalizePath(req.Path)
	if normPath == "." {
		return nil, fmt.Errorf("%w: path must refer to a file", ErrInvalidArgument)
	}

	var (
		diskBytes []byte
		diskHash  string
		exists    bool
		fileMode  fs.FileMode = 0o644
	)

	existingInfo, statErr := s.sandbox.FS().Stat(ctx, normPath)
	if statErr == nil {
		if existingInfo.IsDir() {
			return nil, fmt.Errorf("%w: %s is a directory", ErrInvalidArgument, normPath)
		}
		fileMode = existingInfo.Mode().Perm()
		if fileMode == 0 {
			fileMode = 0o644
		}
	}

	readBytes, err := s.sandbox.FS().ReadFile(ctx, normPath)
	switch {
	case err == nil:
		exists = true
		diskBytes = readBytes
		diskHash = sha256Hex(diskBytes)
		if !req.Force {
			if err := s.verifyCAS(normPath, diskHash, req.ExpectedHash); err != nil {
				return nil, err
			}
		}
	case errors.Is(err, os.ErrNotExist), errors.Is(err, fs.ErrNotExist):
		exists = false
		if !req.Force && req.ExpectedHash != "" {
			return nil, fmt.Errorf("%w: %s (expected %s, file does not exist)", ErrFileModified, normPath, req.ExpectedHash)
		}
	default:
		return nil, err
	}

	if req.Mode != 0 {
		fileMode = req.Mode
	}

	afterBytes := []byte(req.Content)
	afterHash := sha256Hex(afterBytes)

	beforeStr := string(diskBytes)
	unifiedDiff := diff.FormatUnified("a/"+normPath, "b/"+normPath, beforeStr, req.Content)
	add, del := diff.Stats(beforeStr, req.Content)

	if !req.DryRun {
		parent := path.Dir(normPath)
		if parent != "." && parent != "" {
			if err := s.sandbox.FS().MkdirAll(ctx, parent, 0o755); err != nil {
				return nil, err
			}
		}
		if err := s.sandbox.FS().WriteFile(ctx, normPath, afterBytes, fileMode); err != nil {
			return nil, err
		}

		kind := KindCreated
		if exists {
			kind = KindModified
		}

		change := FileChange{
			Path:          normPath,
			Source:        SourceAgent,
			Tool:          "write",
			Kind:          kind,
			BeforeHash:    diskHash,
			AfterHash:     afterHash,
			BeforeContent: diskBytes,
			AfterContent:  afterBytes,
			Additions:     add,
			Deletions:     del,
			IsBinary:      isBinaryContent(afterBytes),
			Timestamp:     time.Now().UTC(),
		}
		if err := s.tracker.RecordChange(ctx, &change); err != nil {
			return nil, err
		}
		s.logger.Debug("wrote file",
			log.String("path", normPath),
			log.String("kind", string(kind)),
			log.Int("bytes", len(afterBytes)),
		)
	}

	return &WriteResult{
		Path:         normPath,
		Created:      !exists,
		BytesWritten: len(afterBytes),
		Additions:    add,
		Deletions:    del,
		Diff:         unifiedDiff,
		BeforeHash:   diskHash,
		AfterHash:    afterHash,
		DryRun:       req.DryRun,
	}, nil
}

// Edit performs an exact literal string replacement on an existing text file in
// the sandbox, enforcing Read-Before-Overwrite and Compare-And-Swap unless
// req.Force is true.
func (s *Std) Edit(ctx context.Context, req EditRequest) (*EditResult, error) {
	if strings.TrimSpace(req.Path) == "" {
		return nil, fmt.Errorf("%w: path is required", ErrInvalidArgument)
	}
	if req.OldText == "" {
		return nil, fmt.Errorf("%w: old_text must not be empty", ErrInvalidArgument)
	}
	normPath := s.normalizePath(req.Path)
	if normPath == "." {
		return nil, fmt.Errorf("%w: path must refer to a file", ErrInvalidArgument)
	}

	diskBytes, err := s.sandbox.FS().ReadFile(ctx, normPath)
	if err != nil {
		return nil, err
	}
	if isBinaryContent(diskBytes) {
		return nil, fmt.Errorf("%w: %s", ErrBinaryFile, normPath)
	}

	diskHash := sha256Hex(diskBytes)
	if !req.Force {
		if err := s.verifyCAS(normPath, diskHash, req.ExpectedHash); err != nil {
			return nil, err
		}
	}

	beforeStr := string(diskBytes)
	count := strings.Count(beforeStr, req.OldText)
	if count == 0 {
		return nil, fmt.Errorf("%w: %s", ErrTargetNotFound, normPath)
	}
	if count > 1 && !req.ReplaceAll {
		return nil, fmt.Errorf("%w: %s (%d matches)", ErrAmbiguousEdit, normPath, count)
	}

	var (
		afterStr     string
		replacements int
	)
	if req.ReplaceAll {
		afterStr = strings.ReplaceAll(beforeStr, req.OldText, req.NewText)
		replacements = count
	} else {
		afterStr = strings.Replace(beforeStr, req.OldText, req.NewText, 1)
		replacements = 1
	}

	afterBytes := []byte(afterStr)
	afterHash := sha256Hex(afterBytes)
	unifiedDiff := diff.FormatUnified("a/"+normPath, "b/"+normPath, beforeStr, afterStr)
	add, del := diff.Stats(beforeStr, afterStr)

	if !req.DryRun {
		fileMode := fs.FileMode(0o644)
		if info, statErr := s.sandbox.FS().Stat(ctx, normPath); statErr == nil {
			if perm := info.Mode().Perm(); perm != 0 {
				fileMode = perm
			}
		}

		if err := s.sandbox.FS().WriteFile(ctx, normPath, afterBytes, fileMode); err != nil {
			return nil, err
		}

		change := FileChange{
			Path:          normPath,
			Source:        SourceAgent,
			Tool:          "edit",
			Kind:          KindModified,
			BeforeHash:    diskHash,
			AfterHash:     afterHash,
			BeforeContent: diskBytes,
			AfterContent:  afterBytes,
			Additions:     add,
			Deletions:     del,
			Timestamp:     time.Now().UTC(),
		}
		if err := s.tracker.RecordChange(ctx, &change); err != nil {
			return nil, err
		}
		s.logger.Debug("edited file",
			log.String("path", normPath),
			log.Int("replacements", replacements),
		)
	}

	return &EditResult{
		Path:         normPath,
		Replacements: replacements,
		Additions:    add,
		Deletions:    del,
		Diff:         unifiedDiff,
		BeforeHash:   diskHash,
		AfterHash:    afterHash,
		DryRun:       req.DryRun,
	}, nil
}

// Remove deletes an existing file from the sandbox filesystem, enforcing
// Read-Before-Overwrite and Compare-And-Swap unless req.Force is true.
func (s *Std) Remove(ctx context.Context, req RemoveRequest) (*RemoveResult, error) {
	if strings.TrimSpace(req.Path) == "" {
		return nil, fmt.Errorf("%w: path is required", ErrInvalidArgument)
	}
	normPath := s.normalizePath(req.Path)
	if normPath == "." {
		return nil, fmt.Errorf("%w: path must refer to a file", ErrInvalidArgument)
	}

	diskBytes, err := s.sandbox.FS().ReadFile(ctx, normPath)
	if err != nil {
		return nil, err
	}

	diskHash := sha256Hex(diskBytes)
	if !req.Force {
		if err := s.verifyCAS(normPath, diskHash, req.ExpectedHash); err != nil {
			return nil, err
		}
	}

	isBin := isBinaryContent(diskBytes)
	var (
		unifiedDiff string
		del         int
	)
	if !isBin {
		beforeStr := string(diskBytes)
		unifiedDiff = diff.FormatUnified("a/"+normPath, "b/"+normPath, beforeStr, "")
		_, del = diff.Stats(beforeStr, "")
	}

	if !req.DryRun {
		if err := s.sandbox.FS().Remove(ctx, normPath); err != nil {
			return nil, err
		}

		change := FileChange{
			Path:          normPath,
			Source:        SourceAgent,
			Tool:          "remove",
			Kind:          KindDeleted,
			BeforeHash:    diskHash,
			AfterHash:     "",
			BeforeContent: diskBytes,
			AfterContent:  nil,
			Additions:     0,
			Deletions:     del,
			IsBinary:      isBin,
			Timestamp:     time.Now().UTC(),
		}
		if err := s.tracker.RecordChange(ctx, &change); err != nil {
			return nil, err
		}
		s.logger.Debug("removed file", log.String("path", normPath))
	}

	return &RemoveResult{
		Path:       normPath,
		Deletions:  del,
		Diff:       unifiedDiff,
		BeforeHash: diskHash,
		DryRun:     req.DryRun,
	}, nil
}

// SyncExternalChanges inspects all paths currently tracked in the FileTracker
// against the sandbox filesystem. Any tracked file whose on-disk SHA-256 hash
// differs from its last recorded hash—or that has been deleted on disk—is
// recorded as a SourceExternal change.
func (s *Std) SyncExternalChanges(ctx context.Context) ([]FileChange, error) {
	paths := s.tracker.TrackedPaths()
	var changes []FileChange

	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return changes, err
		}

		lastHash, ok := s.tracker.LastHash(p)
		if !ok || lastHash == "" {
			continue
		}
		prevContent, hasPrevContent := s.tracker.LastContent(p)

		diskBytes, err := s.sandbox.FS().ReadFile(ctx, p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrNotExist) {
				isBin := isBinaryContent(prevContent)
				var del int
				if !isBin {
					_, del = diff.Stats(string(prevContent), "")
				}
				change := FileChange{
					Path:          p,
					Source:        SourceExternal,
					Tool:          "external",
					Kind:          KindDeleted,
					BeforeHash:    lastHash,
					AfterHash:     "",
					BeforeContent: prevContent,
					Additions:     0,
					Deletions:     del,
					IsBinary:      isBin,
					Timestamp:     time.Now().UTC(),
				}
				if recErr := s.tracker.RecordChange(ctx, &change); recErr != nil {
					return changes, recErr
				}
				changes = append(changes, change)
				continue
			}
			return changes, err
		}

		diskHash := sha256Hex(diskBytes)
		if diskHash != lastHash {
			isBin := isBinaryContent(prevContent) || isBinaryContent(diskBytes)
			var add, del int
			if !isBin {
				add, del = diff.Stats(string(prevContent), string(diskBytes))
			}
			kind := KindModified
			beforeHash := lastHash
			if !hasPrevContent {
				kind = KindCreated
				beforeHash = ""
			}
			change := FileChange{
				Path:          p,
				Source:        SourceExternal,
				Tool:          "external",
				Kind:          kind,
				BeforeHash:    beforeHash,
				AfterHash:     diskHash,
				BeforeContent: prevContent,
				AfterContent:  diskBytes,
				Additions:     add,
				Deletions:     del,
				IsBinary:      isBin,
				Timestamp:     time.Now().UTC(),
			}
			if recErr := s.tracker.RecordChange(ctx, &change); recErr != nil {
				return changes, recErr
			}
			changes = append(changes, change)
		}
	}

	return changes, nil
}

func (s *Std) verifyCAS(normPath, diskHash, expectedHash string) error {
	if expectedHash != "" {
		if diskHash != expectedHash {
			return fmt.Errorf("%w: %s (expected %s, found %s)", ErrFileModified, normPath, expectedHash, diskHash)
		}
		return nil
	}
	lastHash, ok := s.tracker.LastHash(normPath)
	if !ok || lastHash == "" {
		return fmt.Errorf("%w: %s", ErrFileNotRead, normPath)
	}
	if lastHash != diskHash {
		return fmt.Errorf("%w: %s (expected %s, found %s)", ErrFileModified, normPath, lastHash, diskHash)
	}
	return nil
}

func validateGlobPatterns(patterns []string) error {
	for _, p := range patterns {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if err := validateGlobPattern(p); err != nil {
			return err
		}
	}
	return nil
}

func validateGlobPattern(pattern string) error {
	if _, err := fsutils.Compile(pattern); err != nil {
		return fmt.Errorf("%w: invalid glob pattern %q: %v", ErrInvalidArgument, pattern, err)
	}
	return nil
}

func patternTargetsHidden(pattern string) bool {
	norm := strings.TrimPrefix(strings.TrimSpace(pattern), "./")
	for seg := range strings.SplitSeq(norm, "/") {
		if strings.HasPrefix(seg, ".") && seg != "." && seg != ".." {
			return true
		}
	}
	return false
}

func hasDotSegment(p string) bool {
	for part := range strings.SplitSeq(p, "/") {
		if strings.HasPrefix(part, ".") && part != "." && part != ".." {
			return true
		}
	}
	return false
}

func matchesAnyIgnore(patterns []string, name, relToBase, fullPath string) bool {
	for _, pat := range patterns {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if ok, _ := matchGlob(pat, name); ok {
			return true
		}
		if ok, _ := matchGlob(pat, relToBase); ok {
			return true
		}
		if ok, _ := matchGlob(pat, fullPath); ok {
			return true
		}
	}
	return false
}

func matchesIncludeGlob(pattern, name, relToBase, fullPath string) bool {
	pattern = strings.TrimSpace(pattern)
	if !strings.Contains(pattern, "/") {
		if ok, _ := matchGlob(pattern, name); ok {
			return true
		}
	}
	if ok, _ := matchGlob(pattern, relToBase); ok {
		return true
	}
	if ok, _ := matchGlob(pattern, fullPath); ok {
		return true
	}
	return false
}

// matchGlob matches a slash-separated target path against a glob pattern that
// supports *, ?, [...], and ** (zero or more directory segments).
func matchGlob(pattern, target string) (bool, error) {
	g, err := fsutils.Compile(pattern)
	if err != nil {
		return false, fmt.Errorf("%w: invalid glob pattern %q: %v", ErrInvalidArgument, pattern, err)
	}
	return g.Match(target), nil
}
