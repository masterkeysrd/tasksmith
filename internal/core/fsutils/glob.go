package fsutils

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Glob represents a compiled glob pattern.
type Glob struct {
	re *regexp.Regexp
}

// Compile compiles pattern into a [Glob].
// It supports standard wildcard syntax including '*', '?', '[...]', '[!...]',
// and '**' multi-directory wildcards.
func Compile(pattern string) (*Glob, error) {
	if strings.HasSuffix(pattern, "\\") {
		return nil, fmt.Errorf("glob: invalid pattern %q: %w", pattern, path.ErrBadPattern)
	}

	norm := strings.ReplaceAll(pattern, "\\", "/")
	norm = strings.TrimPrefix(norm, "./")

	segments := strings.SplitSeq(norm, "/")
	for seg := range segments {
		if seg == "**" {
			continue
		}
		if _, err := path.Match(seg, "x"); err != nil {
			return nil, fmt.Errorf("glob: invalid pattern %q: %w", pattern, err)
		}
	}

	var sb strings.Builder
	sb.WriteString("^")
	i := 0
	for i < len(norm) {
		if strings.HasPrefix(norm[i:], "**/") {
			sb.WriteString("(?:.*/)?")
			i += 3
		} else if strings.HasPrefix(norm[i:], "**") {
			sb.WriteString(".*")
			i += 2
		} else if norm[i] == '*' {
			sb.WriteString("[^/]*")
			i++
		} else if norm[i] == '?' {
			sb.WriteString("[^/]")
			i++
		} else if norm[i] == '[' {
			j := i + 1
			if j < len(norm) && (norm[j] == '!' || norm[j] == '^') {
				j++
			}
			if j < len(norm) && norm[j] == ']' {
				j++
			}
			for j < len(norm) && norm[j] != ']' {
				j++
			}
			if j >= len(norm) {
				return nil, fmt.Errorf("glob: invalid pattern %q: %w", pattern, path.ErrBadPattern)
			}
			class := norm[i : j+1]
			if strings.HasPrefix(class, "[!") {
				class = "[^" + class[2:]
			}
			sb.WriteString(class)
			i = j + 1
		} else {
			sb.WriteString(regexp.QuoteMeta(string(norm[i])))
			i++
		}
	}
	sb.WriteString("$")

	re, err := regexp.Compile(sb.String())
	if err != nil {
		return nil, fmt.Errorf("glob: invalid pattern %q: %w", pattern, err)
	}

	return &Glob{re: re}, nil
}

// Match reports whether text matches the compiled glob pattern.
func (g *Glob) Match(text string) bool {
	if g == nil || g.re == nil {
		return false
	}
	norm := strings.ReplaceAll(text, "\\", "/")
	norm = strings.TrimPrefix(norm, "./")
	return g.re.MatchString(norm)
}
