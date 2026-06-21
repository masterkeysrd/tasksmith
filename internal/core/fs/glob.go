package fs

import (
	"fmt"
	"regexp"
	"strings"
)

// Glob represents a compiled glob pattern.
type Glob struct {
	re *regexp.Regexp
}

// Compile compiles a glob pattern into a Glob struct.
// It supports standard globbing operators:
// - '*' matches any sequence of non-separator characters.
// - '?' matches any single non-separator character.
// - '**' matches any sequence of characters including separators.
func Compile(pattern string) (*Glob, error) {
	// Standardize to forward slashes for matching.
	pattern = strings.ReplaceAll(pattern, "\\", "/")

	// Replace glob patterns with unique placeholders before escaping
	p := pattern
	p = strings.ReplaceAll(p, "**/", "___DOUBLE_STAR_SLASH___")
	p = strings.ReplaceAll(p, "**", "___DOUBLE_STAR___")
	p = strings.ReplaceAll(p, "*", "___STAR___")
	p = strings.ReplaceAll(p, "?", "___QUESTION___")

	// Escape other standard regex characters
	rePattern := regexp.QuoteMeta(p)

	// Replace placeholders with regex equivalents
	rePattern = strings.ReplaceAll(rePattern, "___DOUBLE_STAR_SLASH___", `(?:.*/)?`)
	rePattern = strings.ReplaceAll(rePattern, "___DOUBLE_STAR___", `.*`)
	rePattern = strings.ReplaceAll(rePattern, "___STAR___", `[^/]*`)
	rePattern = strings.ReplaceAll(rePattern, "___QUESTION___", `[^/]`)

	re, err := regexp.Compile("^" + rePattern + "$")
	if err != nil {
		return nil, fmt.Errorf("glob: failed to compile pattern: %w", err)
	}

	return &Glob{re: re}, nil
}

// Match returns true if the given text matches the glob pattern.
func (g *Glob) Match(text string) bool {
	// Standardize input path separators.
	text = strings.ReplaceAll(text, "\\", "/")
	return g.re.MatchString(text)
}
