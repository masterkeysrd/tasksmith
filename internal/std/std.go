package std

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/sandbox"
)

const (
	// DefaultViewLimit is the default maximum number of lines returned by View.
	DefaultViewLimit = 2000

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

// normalizePath delegates path normalization to s.sandbox.FS().ResolvePath.
// If the path attempts to escape the sandbox root, it is returned unstripped so
// that the subsequent sandbox.FS operation rejects it with sandbox.ErrPathEscape.
func (s *Std) normalizePath(rawPath string) string {
	rel, err := s.sandbox.FS().ResolvePath(rawPath)
	if err != nil {
		return strings.TrimSpace(rawPath)
	}
	return rel
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
