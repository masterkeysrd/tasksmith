package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	corefs "github.com/masterkeysrd/tasksmith/internal/core/fs"
	"github.com/masterkeysrd/tasksmith/internal/core/ripgrep"
)

const (
	// MaxGrepMatches is the maximum number of matches returned by the grep tool
	// to prevent resource exhaustion and huge payloads.
	MaxGrepMatches = 1000
)

// Grep searches for a regex pattern in files using ripgrep if available, falling back
// to a recursive directory scanner that respects ignore rules.
func (h *ToolHandlers) Grep(ctx context.Context, in GrepArgs) (GrepOutput, error) {
	baseDir := h.CWD
	if baseDir == "" {
		baseDir = "."
	}
	baseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return GrepOutput{}, fmt.Errorf("failed to resolve workspace directory: %w", err)
	}

	searchPath := in.Path
	if searchPath == "" {
		searchPath = "."
	}

	var searchAbs string
	if filepath.IsAbs(searchPath) {
		searchAbs = searchPath
	} else {
		searchAbs = filepath.Join(baseDir, searchPath)
	}
	searchAbs, err = filepath.Abs(searchAbs)
	if err != nil {
		return GrepOutput{}, fmt.Errorf("failed to resolve search path: %w", err)
	}

	// 1. Try Ripgrep backend first.
	if ripgrep.Available() {
		relSearch, err := filepath.Rel(baseDir, searchAbs)
		if err != nil {
			relSearch = searchPath
		}

		rawMatches, err := ripgrep.Search(ctx, baseDir, relSearch, in.Pattern)
		if err == nil {
			ignorers := make(map[string]corefs.Ignorer)
			getIgnorer := func(dir string) corefs.Ignorer {
				if ig, ok := ignorers[dir]; ok {
					return ig
				}
				ig, err := corefs.NewIgnorer(dir)
				if err != nil {
					ig, _ = corefs.NewIgnorer("")
				}
				ignorers[dir] = ig
				return ig
			}

			var matches []GrepOutputMatchesItem
			var totalCount int
			for _, match := range rawMatches {
				var absPath string
				if strings.HasPrefix(match.Path, "./") {
					absPath = filepath.Join(baseDir, match.Path[2:])
				} else {
					absPath = filepath.Join(baseDir, match.Path)
				}

				parentDir := filepath.Dir(absPath)
				ig := getIgnorer(parentDir)
				name := filepath.Base(absPath)
				if ig.ShouldIgnore(name, absPath, false) {
					continue
				}

				totalCount++
				if len(matches) < MaxGrepMatches {
					matches = append(matches, GrepOutputMatchesItem{
						Path:    match.Path,
						Line:    match.Line,
						Content: match.Content,
					})
				}
			}

			return GrepOutput{
				Matches:    matches,
				TotalCount: totalCount,
				Truncated:  totalCount > MaxGrepMatches,
			}, nil
		}
	}

	// 2. Go-based regex search fallback.
	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return GrepOutput{}, fmt.Errorf("invalid regex pattern: %w", err)
	}

	ignorers := make(map[string]corefs.Ignorer)
	getIgnorer := func(dir string) corefs.Ignorer {
		if ig, ok := ignorers[dir]; ok {
			return ig
		}
		ig, err := corefs.NewIgnorer(dir)
		if err != nil {
			ig, _ = corefs.NewIgnorer("")
		}
		ignorers[dir] = ig
		return ig
	}

	var matches []GrepOutputMatchesItem

	fi, err := os.Stat(searchAbs)
	if err != nil {
		return GrepOutput{}, fmt.Errorf("failed to access path: %w", err)
	}

	if !fi.IsDir() {
		name := fi.Name()
		ig := getIgnorer(filepath.Dir(searchAbs))
		if ig.ShouldIgnore(name, searchAbs, false) {
			return GrepOutput{}, nil
		}

		rel, err := filepath.Rel(baseDir, searchAbs)
		if err != nil {
			return GrepOutput{}, err
		}
		relSlash := "./" + filepath.ToSlash(rel)

		fileMatches, err := searchFile(ctx, searchAbs, relSlash, re)
		if err != nil {
			return GrepOutput{}, err
		}
		for _, m := range fileMatches {
			if len(matches) >= MaxGrepMatches {
				break
			}
			matches = append(matches, m)
		}

		return GrepOutput{
			Matches:    matches,
			TotalCount: len(matches),
			Truncated:  len(matches) >= MaxGrepMatches,
		}, nil
	}

	err = filepath.WalkDir(searchAbs, func(path string, d os.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil
		}

		if path == searchAbs {
			return nil
		}

		parentDir := filepath.Dir(path)
		ig := getIgnorer(parentDir)

		name := d.Name()
		if ig.ShouldIgnore(name, path, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(baseDir, path)
		if err != nil {
			return nil
		}
		relSlash := "./" + filepath.ToSlash(rel)

		fileMatches, err := searchFile(ctx, path, relSlash, re)
		if err != nil {
			return nil
		}

		for _, m := range fileMatches {
			if len(matches) >= MaxGrepMatches {
				return filepath.SkipAll
			}
			matches = append(matches, m)
		}

		return nil
	})

	if err != nil && err != filepath.SkipAll {
		return GrepOutput{}, fmt.Errorf("grep walk failed: %w", err)
	}

	return GrepOutput{
		Matches:    matches,
		TotalCount: len(matches),
		Truncated:  len(matches) >= MaxGrepMatches,
	}, nil
}

// searchFile scans a single file line-by-line for matches against the compiled regex.
func searchFile(ctx context.Context, path, relPath string, re *regexp.Regexp) ([]GrepOutputMatchesItem, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()

	var matches []GrepOutputMatchesItem
	scanner := bufio.NewScanner(f)

	const maxCapacity = 1024 * 1024 // 1MB
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	lineNum := 0
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		lineNum++
		text := scanner.Text()
		if re.MatchString(text) {
			matches = append(matches, GrepOutputMatchesItem{
				Path:    relPath,
				Line:    lineNum,
				Content: text,
			})
		}
	}

	return matches, scanner.Err()
}

// TextContent implements tool.TextContentProvider so loom renders the result
// as a human-readable list of search matches instead of a raw JSON blob.
func (o GrepOutput) TextContent() string {
	if len(o.Matches) == 0 {
		return "No matches found."
	}

	const maxRendered = 100
	const grepLineMaxLength = 500

	var sb strings.Builder
	currentFile := ""
	renderedCount := 0

	for _, m := range o.Matches {
		if renderedCount >= maxRendered {
			break
		}

		matchPath := m.Path
		matchLine := m.Line
		matchContent := m.Content
		matchChar := 0

		if matchPath != currentFile {
			if currentFile != "" {
				sb.WriteString("\n")
			}
			currentFile = matchPath
			fmt.Fprintf(&sb, "%s:\n", filepath.ToSlash(currentFile))
		}

		if matchLine > 0 {
			lineText := matchContent
			if len(lineText) > grepLineMaxLength {
				lineText = lineText[:grepLineMaxLength] + " [truncated]"
			}
			if matchChar > 0 {
				fmt.Fprintf(&sb, "  %d:%d: %s\n", matchLine, matchChar, lineText)
			} else {
				fmt.Fprintf(&sb, "  %d: %s\n", matchLine, lineText)
			}
		} else {
			fmt.Fprintf(&sb, "  %s\n", matchPath)
		}

		renderedCount++
	}

	res := sb.String()
	res = strings.TrimSuffix(res, "\n")

	totalCount := o.TotalCount
	if totalCount < len(o.Matches) {
		totalCount = len(o.Matches)
	}

	if totalCount > maxRendered {
		res += fmt.Sprintf("\n\n[SYSTEM NOTE: Showing %d of %d matches. Call grep again with a more specific pattern.]", maxRendered, totalCount)
	} else {
		res += fmt.Sprintf("\n\n[%d matches]", totalCount)
	}

	return res
}
