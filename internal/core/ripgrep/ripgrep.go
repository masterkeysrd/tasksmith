package ripgrep

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	rgAvailable bool
	lastChecked time.Time
	mu          sync.Mutex
)

// Match represents a search match returned by ripgrep.
type Match struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

// Available checks if the ripgrep binary (rg) is available on the system.
// It caches the result. If rg was not found, it will re-check at most once
// every 30 seconds to dynamically detect new installations.
func Available() bool {
	mu.Lock()
	defer mu.Unlock()

	now := time.Now()
	if rgAvailable {
		return true
	}

	if !lastChecked.IsZero() && now.Sub(lastChecked) < 30*time.Second {
		return false
	}

	lastChecked = now
	_, err := exec.LookPath("rg")
	if err == nil {
		rgAvailable = true
		return true
	}
	return false
}

// Glob searches for files matching the glob pattern using ripgrep.
// All returned paths are relative to cwd.
func Glob(ctx context.Context, cwd, searchPath, pattern string) ([]string, error) {
	if !Available() {
		return nil, fmt.Errorf("ripgrep is not available")
	}

	args := []string{"--files", "-L", "--null"}
	if pattern != "" {
		args = append(args, "--glob", pattern)
	}

	targetDir := "."
	if searchPath != "" {
		targetDir = searchPath
	}
	args = append(args, targetDir)

	cmd := exec.CommandContext(ctx, "rg", args...)
	cmd.Dir = cwd

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return nil, nil
		}
		return nil, fmt.Errorf("ripgrep failed: %w (stderr: %s)", err, stderr.String())
	}

	parts := bytes.Split(stdout.Bytes(), []byte{0})
	var matches []string
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		pathStr := strings.TrimSpace(string(part))
		if pathStr == "" {
			continue
		}

		absPath := pathStr
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(cwd, absPath)
		}

		relPath, err := filepath.Rel(cwd, absPath)
		if err != nil {
			continue
		}
		relSlash := filepath.ToSlash(relPath)
		matches = append(matches, "./"+relSlash)
	}

	// Sort matches to be deterministic and stable.
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i] < matches[j]
	})

	return matches, nil
}

// Search executes ripgrep to search for pattern inside searchPath.
// All matched file paths are returned relative to cwd.
func Search(ctx context.Context, cwd, searchPath, pattern string) ([]Match, error) {
	if !Available() {
		return nil, fmt.Errorf("ripgrep is not available")
	}

	args := []string{"--line-number", "--json", "--hidden", "-H", "-L", "-e", pattern}
	targetDir := "."
	if searchPath != "" {
		targetDir = searchPath
	}
	args = append(args, targetDir)

	cmd := exec.CommandContext(ctx, "rg", args...)
	cmd.Dir = cwd

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("ripgrep failed: %w (stderr: %s)", err, stderr.String())
	}

	var matches []Match
	scanner := bufio.NewScanner(&stdout)

	const maxCapacity = 1024 * 1024 // 1MB
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg struct {
			Type string `json:"type"`
			Data struct {
				Path struct {
					Text string `json:"text"`
				} `json:"path"`
				LineNumber int `json:"line_number"`
				Lines      struct {
					Text string `json:"text"`
				} `json:"lines"`
			} `json:"data"`
		}

		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		if msg.Type == "match" {
			absPath := msg.Data.Path.Text
			if !filepath.IsAbs(absPath) {
				absPath = filepath.Join(cwd, absPath)
			}
			relPath, err := filepath.Rel(cwd, absPath)
			if err != nil {
				continue
			}
			relSlash := filepath.ToSlash(relPath)

			matches = append(matches, Match{
				Path:    "./" + relSlash,
				Line:    msg.Data.LineNumber,
				Content: strings.TrimSuffix(msg.Data.Lines.Text, "\n"),
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan ripgrep output: %w", err)
	}

	return matches, nil
}
