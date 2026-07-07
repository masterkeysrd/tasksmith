package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	corefs "github.com/masterkeysrd/tasksmith/internal/core/fs"
	"github.com/masterkeysrd/tasksmith/internal/core/ripgrep"
)

const (
	// MaxGlobMatches is the maximum number of matches returned by the glob tool
	// to prevent resource exhaustion and huge payloads.
	MaxGlobMatches = 200
)

// Validate compiles the glob pattern to verify it is correct.
func (in GlobArgs) Validate() error {
	pattern := strings.TrimPrefix(in.Pattern, "./")
	if !strings.Contains(pattern, "/") {
		pattern = "**/" + pattern
	}
	_, err := corefs.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid pattern %q: %w", in.Pattern, err)
	}
	return nil
}

// Glob finds files matching a glob pattern using gitignore and predefined ignore rules.
func (h *ToolHandlers) Glob(ctx context.Context, in GlobArgs) (GlobOutput, error) {
	if err := in.Validate(); err != nil {
		return GlobOutput{}, err
	}
	baseDir := h.CWD
	if baseDir == "" {
		baseDir = "."
	}
	baseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return GlobOutput{}, fmt.Errorf("failed to resolve workspace directory: %w", err)
	}

	searchDir := baseDir
	if in.Path != "" {
		if filepath.IsAbs(in.Path) {
			searchDir = in.Path
		} else {
			searchDir = filepath.Join(baseDir, in.Path)
		}
	}
	searchDir, err = filepath.Abs(searchDir)
	if err != nil {
		return GlobOutput{}, fmt.Errorf("failed to resolve search directory: %w", err)
	}

	// Clean/normalize the pattern.
	pattern := strings.TrimPrefix(in.Pattern, "./")
	// If the pattern doesn't contain a directory separator, assume the agent
	// wants to find it anywhere in the tree (like bash find -name).
	if !strings.Contains(pattern, "/") {
		pattern = "**/" + pattern
	}
	g, err := corefs.Compile(pattern)
	if err != nil {
		return GlobOutput{}, fmt.Errorf("invalid pattern %q: %w", in.Pattern, err)
	}

	if ripgrep.Available() {
		rawMatches, err := ripgrep.Glob(ctx, baseDir, in.Path, pattern)
		if err == nil {
			ignorers := make(map[string]corefs.Ignorer)
			getIgnorer := func(dir string) corefs.Ignorer {
				if ig, ok := ignorers[dir]; ok {
					return ig
				}
				ig, err := corefs.NewIgnorer(dir)
				if err != nil {
					ig, _ = corefs.NewIgnorer("") // fallback: predefined rules only
				}
				ignorers[dir] = ig
				return ig
			}

			var matches []string
			var totalCount int
			for _, match := range rawMatches {
				var absPath string
				if strings.HasPrefix(match, "./") {
					absPath = filepath.Join(baseDir, match[2:])
				} else {
					absPath = filepath.Join(baseDir, match)
				}
				parentDir := filepath.Dir(absPath)
				ig := getIgnorer(parentDir)
				name := filepath.Base(absPath)
				if ig.ShouldIgnore(name, absPath, false) {
					continue
				}
				totalCount++
				if len(matches) < MaxGlobMatches {
					matches = append(matches, match)
				}
			}

			return GlobOutput{
				Matches:    matches,
				TotalCount: totalCount,
				Truncated:  totalCount > MaxGlobMatches,
			}, nil
		}
	}

	ignorers := make(map[string]corefs.Ignorer)
	getIgnorer := func(dir string) corefs.Ignorer {
		if ig, ok := ignorers[dir]; ok {
			return ig
		}
		ig, err := corefs.NewIgnorer(dir)
		if err != nil {
			ig, _ = corefs.NewIgnorer("") // fallback: predefined rules only
		}
		ignorers[dir] = ig
		return ig
	}

	var matches []string
	var totalCount int

	err = filepath.WalkDir(searchDir, func(path string, d os.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil
		}

		if path == searchDir {
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

		// Calculate relative path inside the search directory for pattern matching
		relSearch, err := filepath.Rel(searchDir, path)
		if err != nil {
			return nil
		}
		relSearchSlash := filepath.ToSlash(relSearch)

		// Check pattern match.
		if g.Match(relSearchSlash) {
			totalCount++
			if len(matches) < MaxGlobMatches {
				// Return paths relative to the workspace base directory.
				relBase, err := filepath.Rel(baseDir, path)
				if err != nil {
					return nil
				}
				relBaseSlash := filepath.ToSlash(relBase)
				matches = append(matches, "./"+relBaseSlash)
			}
		}

		return nil
	})

	if err != nil {
		return GlobOutput{}, fmt.Errorf("glob walk failed: %w", err)
	}

	return GlobOutput{
		Matches:    matches,
		TotalCount: totalCount,
		Truncated:  totalCount > MaxGlobMatches,
	}, nil
}

// TextContent implements tool.TextContentProvider so loom renders the result
// as a human-readable list of matching files instead of a raw JSON blob.
func (o GlobOutput) TextContent() string {
	if len(o.Matches) == 0 {
		return "No matches found."
	}
	var sb strings.Builder
	for _, match := range o.Matches {
		sb.WriteString(match)
		sb.WriteByte('\n')
	}

	if o.Truncated {
		fmt.Fprintf(&sb, "\n[SYSTEM NOTE: Showing %d of %d matches. Call glob again with a different pattern to narrow down results.]",
			len(o.Matches), o.TotalCount)
	} else {
		fmt.Fprintf(&sb, "\n[%d matches]", o.TotalCount)
	}

	return sb.String()
}
