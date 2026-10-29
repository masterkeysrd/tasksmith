package std

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/ripgrep"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
)

const (
	// DefaultViewLimit is the default maximum number of lines returned by View.
	DefaultViewLimit = 500

	// MaxViewLimit is the maximum number of lines that View can return in a single call.
	MaxViewLimit = 2000

	// MaxViewBytes is the maximum total byte size of content returned by View (32 KiB).
	MaxViewBytes = 32 * 1024

	// MaxViewLineBytes is the maximum byte length of a single line in View before line truncation.
	MaxViewLineBytes = 2048

	// DefaultListLimit is the default maximum number of entries returned by List.
	DefaultListLimit = 200

	// MaxListLimit is the maximum number of entries that List can return in a single call.
	MaxListLimit = 500

	// MaxListBytes is the maximum total byte budget for List entries (32 KiB).
	MaxListBytes = 32 * 1024

	// DefaultGlobLimit is the default maximum number of matches returned by Glob.
	DefaultGlobLimit = 200

	// MaxGlobLimit is the maximum number of matches that Glob can return in a single call.
	MaxGlobLimit = 500

	// MaxGlobBytes is the maximum total byte budget for Glob match paths (32 KiB).
	MaxGlobBytes = 32 * 1024

	// DefaultGrepLimit is the default maximum number of matches returned by Grep.
	DefaultGrepLimit = 100

	// MaxGrepLimit is the maximum number of matches that Grep can return in a single call.
	MaxGrepLimit = 500

	// MaxGrepLineBytes is the maximum byte length of a matched or context line in Grep.
	MaxGrepLineBytes = 500

	// MaxGrepBytes is the maximum total byte budget for Grep matches (32 KiB).
	MaxGrepBytes = 32 * 1024

	// DefaultMaxFetchBytes is the default response body cap for Fetch (10 MiB).
	DefaultMaxFetchBytes int64 = 10 * 1024 * 1024

	// DefaultMaxDownloadBytes is the default response body cap for Download (100 MiB).
	DefaultMaxDownloadBytes int64 = 100 * 1024 * 1024

	// binarySniffLen is the number of leading bytes inspected for NUL bytes to
	// classify a file as binary.
	binarySniffLen = 8000
)

// Config holds the required dependencies for constructing a [Std] instance.
type Config struct {
	Sandbox sandbox.Sandbox
	Tracker FileTracker
	Logger  log.Interface
}

// Validate ensures that all required dependencies (Sandbox, Tracker, and Logger)
// are non-nil. No silent fallbacks are permitted.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("%w: config cannot be nil", ErrInvalidConfig)
	}
	if c.Sandbox == nil {
		return fmt.Errorf("%w: sandbox is required", ErrInvalidConfig)
	}
	if c.Tracker == nil {
		return fmt.Errorf("%w: tracker is required", ErrInvalidConfig)
	}
	if c.Logger == nil {
		return fmt.Errorf("%w: logger is required", ErrInvalidConfig)
	}
	return nil
}

// Std implements TaskSmith's Standard Operations Library over an isolated
// [sandbox.Sandbox] and a [FileTracker].
type Std struct {
	sandbox sandbox.Sandbox
	tracker FileTracker
	logger  log.Interface
	rg      *ripgrep.Runner
}

// New validates cfg and constructs a new [Std] operations library instance.
func New(cfg Config) (*Std, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Std{
		sandbox: cfg.Sandbox,
		tracker: cfg.Tracker,
		logger:  cfg.Logger.ForComponent("std"),
		rg: ripgrep.New(func(ctx context.Context, name string, args []string, dir string) ([]byte, int, error) {
			res, err := cfg.Sandbox.Proc().Exec(ctx, sandbox.Command{
				Name: name,
				Args: args,
				Dir:  dir,
			})
			if err != nil {
				return nil, -1, err
			}
			return res.Stdout, res.ExitCode, nil
		}),
	}, nil
}

// Sandbox returns the underlying [sandbox.Sandbox] bound to s.
func (s *Std) Sandbox() sandbox.Sandbox {
	return s.sandbox
}

// Tracker returns the [FileTracker] bound to s.
func (s *Std) Tracker() FileTracker {
	return s.tracker
}

// normalizePath delegates path normalization to s.sandbox.FS().ResolvePath
// if the path is within WorkDir. If the path escapes WorkDir, it returns
// a clean slash-separated absolute host path.
func (s *Std) normalizePath(rawPath string) string {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return ""
	}
	expanded := trimmed
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") {
		var home string
		if s.sandbox != nil {
			if env := s.sandbox.Env(); env != nil {
				home = env["HOME"]
			}
		}
		if home == "" {
			if uHome, err := os.UserHomeDir(); err == nil {
				home = uHome
			}
		}
		if home != "" {
			expanded = fsutils.ExpandAndCleanPath(trimmed, "", home)
		}
	}

	if rel, err := s.sandbox.FS().ResolvePath(expanded); err == nil {
		return rel
	}
	if filepath.IsAbs(expanded) {
		return filepath.ToSlash(filepath.Clean(expanded))
	}
	return filepath.ToSlash(filepath.Clean(filepath.Join(s.sandbox.Platform().WorkDir, expanded)))
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func isBinaryContent(data []byte) bool {
	sniff := data
	if len(sniff) > binarySniffLen {
		sniff = sniff[:binarySniffLen]
	}
	return bytes.IndexByte(sniff, 0) >= 0
}

func splitTextLines(content string) []string {
	if content == "" {
		return nil
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
