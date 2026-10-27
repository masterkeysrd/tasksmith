package ripgrep

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrNotAvailable is returned when the ripgrep binary cannot be found or executed.
var ErrNotAvailable = errors.New("ripgrep: rg binary not available")

// ExecFunc executes a binary by name with args in dir and returns its stdout,
// exit code, and any execution error.
type ExecFunc func(ctx context.Context, name string, args []string, dir string) (stdout []byte, exitCode int, err error)

// Runner executes ripgrep commands through an injected ExecFunc (such as a
// sandbox Proc capability) and caches binary availability per Runner instance.
type Runner struct {
	exec        ExecFunc
	mu          sync.Mutex
	available   bool
	lastChecked time.Time
}

// New creates a new ripgrep Runner backed by execFn.
func New(execFn ExecFunc) *Runner {
	return &Runner{exec: execFn}
}

// IsAvailable reports whether the ripgrep (rg) binary is available via Runner's ExecFunc.
// If rg was not found previously, it re-checks at most once every 30 seconds.
func (r *Runner) IsAvailable(ctx context.Context) bool {
	if r == nil || r.exec == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.available {
		return true
	}
	now := time.Now()
	if !r.lastChecked.IsZero() && now.Sub(r.lastChecked) < 30*time.Second {
		return false
	}
	r.lastChecked = now
	_, exitCode, err := r.exec(ctx, "rg", []string{"--version"}, ".")
	r.available = (err == nil && exitCode == 0)
	return r.available
}

// GlobOptions configures file matching options for [Glob].
type GlobOptions struct {
	IncludeHidden bool
	Ignore        []string
}

// Glob searches for files matching pattern under searchPath using ripgrep.
func (r *Runner) Glob(ctx context.Context, searchPath, pattern string, opts ...GlobOptions) ([]string, error) {
	if !r.IsAvailable(ctx) {
		return nil, ErrNotAvailable
	}

	var opt GlobOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	args := []string{"--files", "--null", "--glob", "!.git", "--glob", "!.bare"}
	if opt.IncludeHidden {
		args = append(args, "--hidden")
	}

	for _, ig := range opt.Ignore {
		ig = strings.TrimPrefix(strings.TrimSpace(ig), "./")
		if ig == "" {
			continue
		}
		args = append(args, "--glob", "!"+ig)
		if !strings.HasSuffix(ig, "/**") && !strings.HasSuffix(ig, "/*") {
			args = append(args, "--glob", "!"+strings.TrimSuffix(ig, "/")+"/**")
		}
	}

	if pattern != "" {
		args = append(args, "--glob", pattern)
		if !strings.HasSuffix(pattern, "/*") && !strings.HasSuffix(pattern, "**") {
			args = append(args, "--glob", strings.TrimSuffix(pattern, "/")+"/**")
		}
	}

	targetDir := "."
	if searchPath != "" {
		targetDir = searchPath
	}
	args = append(args, targetDir)

	out, exitCode, err := r.exec(ctx, "rg", args, ".")
	if err != nil {
		return nil, fmt.Errorf("ripgrep glob: %w", err)
	}
	if exitCode == 1 {
		return nil, nil
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("ripgrep glob exited with code %d", exitCode)
	}

	entries := bytes.Split(out, []byte{0})
	var matches []string
	for _, entry := range entries {
		raw := strings.TrimSpace(string(entry))
		if raw == "" {
			continue
		}
		slash := filepath.ToSlash(filepath.Clean(raw))
		slash = strings.TrimPrefix(slash, "./")
		if slash != "" && slash != "." {
			matches = append(matches, slash)
		}
	}

	sort.Strings(matches)
	return matches, nil
}

// Match represents a text match returned by [Search].
type Match struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Content string `json:"content"`
}

// SearchOptions configures pattern search options for [Search].
type SearchOptions struct {
	Literal         bool
	Include         string
	CaseInsensitive bool
	IncludeHidden   bool
}

type rgMsg struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"submatches"`
	} `json:"data"`
}

// Search searches for pattern in text files under searchPath using ripgrep.
func (r *Runner) Search(ctx context.Context, searchPath, pattern string, opts ...SearchOptions) ([]Match, error) {
	if !r.IsAvailable(ctx) {
		return nil, ErrNotAvailable
	}

	var opt SearchOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	args := []string{"--line-number", "--json", "-H", "--glob", "!.git", "--glob", "!.bare"}
	if opt.IncludeHidden {
		args = append(args, "--hidden")
	}
	if opt.Literal {
		args = append(args, "--fixed-strings")
	}
	if opt.CaseInsensitive {
		args = append(args, "-i")
	}
	if opt.Include != "" {
		inc := strings.TrimPrefix(strings.TrimSpace(opt.Include), "./")
		args = append(args, "--glob", inc)
	}

	args = append(args, "-e", pattern)

	targetDir := "."
	if searchPath != "" {
		targetDir = searchPath
	}
	args = append(args, targetDir)

	out, exitCode, err := r.exec(ctx, "rg", args, ".")
	if err != nil {
		return nil, fmt.Errorf("ripgrep search: %w", err)
	}
	if exitCode == 1 {
		return nil, nil
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("ripgrep search exited with code %d", exitCode)
	}

	scanner := bufio.NewScanner(bytes.NewReader(out))
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var matches []Match
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg rgMsg
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if msg.Type != "match" {
			continue
		}
		p := msg.Data.Path.Text
		relSlash := filepath.ToSlash(filepath.Clean(p))
		relSlash = strings.TrimPrefix(relSlash, "./")

		col := 1
		if len(msg.Data.Submatches) > 0 {
			col = msg.Data.Submatches[0].Start + 1
		}
		text := strings.TrimRight(msg.Data.Lines.Text, "\r\n")

		matches = append(matches, Match{
			Path:    relSlash,
			Line:    msg.Data.LineNumber,
			Column:  col,
			Content: text,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ripgrep scan: %w", err)
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Path != matches[j].Path {
			return matches[i].Path < matches[j].Path
		}
		if matches[i].Line != matches[j].Line {
			return matches[i].Line < matches[j].Line
		}
		return matches[i].Column < matches[j].Column
	})

	return matches, nil
}
