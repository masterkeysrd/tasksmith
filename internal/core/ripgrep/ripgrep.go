package ripgrep

import (
	"bytes"
	"context"
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
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
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
