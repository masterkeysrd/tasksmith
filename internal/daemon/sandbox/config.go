package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// FilesystemPolicy describes the canonical path lists a native OS sandbox
// profile enforces beyond the sandbox work directory itself. All entries must
// be absolute host paths; relative or blank entries are dropped during
// normalization.
type FilesystemPolicy struct {
	// DeniedReadPaths lists host paths (typically credential directories)
	// that child processes must not read even though the profile allows
	// broad file reads.
	DeniedReadPaths []string `json:"denied_read_paths,omitempty"`

	// AllowReadPaths lists subpaths inside a DeniedReadPaths entry that
	// should remain readable. A narrower AllowReadPaths entry re-opens a
	// subtree masked by a broader DeniedReadPaths entry.
	AllowReadPaths []string `json:"allow_read_paths,omitempty"`

	// ExtraWritePaths lists additional writable directories outside WorkDir
	// (such as a linked git worktree's shared .git directory) that child
	// processes may write to.
	ExtraWritePaths []string `json:"extra_write_paths,omitempty"`

	// DeniedWritePaths lists paths inside WorkDir or ExtraWritePaths that
	// must stay read-only at the kernel level (such as .agents,
	// WORKSPACE.md, and .git/hooks), overriding the broad write allow.
	DeniedWritePaths []string `json:"denied_write_paths,omitempty"`
}

// IsZero reports whether the policy carries no path restrictions.
func (p *FilesystemPolicy) IsZero() bool {
	return len(p.DeniedReadPaths) == 0 &&
		len(p.AllowReadPaths) == 0 &&
		len(p.ExtraWritePaths) == 0 &&
		len(p.DeniedWritePaths) == 0
}

// Clone returns a deep copy of the policy so callers can extend a shared
// baseline (e.g. layering user sandbox.json overrides onto the built-in
// floor) without mutating the original lists.
func (p FilesystemPolicy) Clone() FilesystemPolicy {
	return FilesystemPolicy{
		DeniedReadPaths:  slices.Clone(p.DeniedReadPaths),
		AllowReadPaths:   slices.Clone(p.AllowReadPaths),
		ExtraWritePaths:  slices.Clone(p.ExtraWritePaths),
		DeniedWritePaths: slices.Clone(p.DeniedWritePaths),
	}
}

// Config defines the configuration for instantiating a [Sandbox].
type Config struct {
	// ID is an optional identifier for the sandbox. If empty, a deterministic
	// prefix of the configuration fingerprint is used.
	ID string `json:"id,omitempty"`

	// Mode selects the isolation backend. Defaults to ModeLocal if empty.
	Mode Mode `json:"mode,omitempty"`

	// WorkDir is the required existing absolute directory on the host that serves
	// as the root of the sandbox filesystem and default process working directory.
	WorkDir string `json:"work_dir"`

	// ReadOnly blocks all mutating filesystem operations and configures native
	// OS sandboxes to disallow file writes inside WorkDir.
	ReadOnly bool `json:"read_only,omitempty"`

	// Env specifies the explicit base environment variables exposed to child
	// processes. The host daemon's os.Environ() is never inherited.
	Env map[string]string `json:"env,omitempty"`

	// Network configures outbound network restrictions.
	Network NetworkPolicy `json:"network,omitzero"`

	// Filesystem carries the structured read/write path lists native OS
	// sandbox profiles enforce in addition to the WorkDir confinement. It has
	// no effect in ModeLocal, where reads and writes are already confined to
	// WorkDir by the os.Root handle.
	Filesystem FilesystemPolicy `json:"filesystem,omitzero"`

	// DefaultTimeout sets the fallback execution timeout for Proc.Exec when
	// Command.Timeout is zero.
	DefaultTimeout time.Duration `json:"default_timeout,omitempty"`
}

// Validate checks that the configuration is structurally sound, that Mode is
// supported, and that WorkDir is an existing absolute directory.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("%w: config cannot be nil", ErrInvalidConfig)
	}

	mode := c.Mode
	if mode == "" {
		mode = ModeLocal
	}
	if !mode.IsValid() {
		return fmt.Errorf("%w: unsupported mode %q", ErrInvalidConfig, mode)
	}

	workDir := strings.TrimSpace(c.WorkDir)
	if workDir == "" {
		return fmt.Errorf("%w: work_dir is required", ErrInvalidConfig)
	}
	if !filepath.IsAbs(workDir) {
		return fmt.Errorf("%w: work_dir %q must be an absolute path", ErrInvalidConfig, workDir)
	}

	info, err := os.Stat(workDir)
	if err != nil {
		return fmt.Errorf("%w: work_dir %q is not accessible: %v", ErrInvalidConfig, workDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: work_dir %q is not a directory", ErrInvalidConfig, workDir)
	}

	if c.DefaultTimeout < 0 {
		return fmt.Errorf("%w: default_timeout cannot be negative", ErrInvalidConfig)
	}

	return nil
}

// Fingerprint computes a canonical SHA-256 hex digest of the effective sandbox
// configuration, normalizing slices and maps for deterministic comparison.
func (c *Config) Fingerprint() string {
	if c == nil {
		return "nil"
	}

	mode := c.Mode
	if mode == "" {
		mode = ModeLocal
	}

	workDir := filepath.Clean(strings.TrimSpace(c.WorkDir))

	allowed := make([]string, len(c.Network.AllowedHosts))
	for i, h := range c.Network.AllowedHosts {
		allowed[i] = strings.ToLower(strings.TrimSpace(h))
	}
	slices.Sort(allowed)

	blocked := make([]string, len(c.Network.BlockedHosts))
	for i, h := range c.Network.BlockedHosts {
		blocked[i] = strings.ToLower(strings.TrimSpace(h))
	}
	slices.Sort(blocked)

	envCopy := make(map[string]string, len(c.Env))
	maps.Copy(envCopy, c.Env)

	canonicalPaths := func(paths []string) []string {
		out := make([]string, len(paths))
		for i, p := range paths {
			out[i] = filepath.Clean(strings.TrimSpace(p))
		}
		slices.Sort(out)
		return out
	}

	canonical := struct {
		Mode           Mode              `json:"mode"`
		WorkDir        string            `json:"work_dir"`
		ReadOnly       bool              `json:"read_only"`
		Env            map[string]string `json:"env,omitempty"`
		Network        NetworkPolicy     `json:"network"`
		Filesystem     FilesystemPolicy  `json:"filesystem"`
		DefaultTimeout time.Duration     `json:"default_timeout,omitempty"`
	}{
		Mode:     mode,
		WorkDir:  workDir,
		ReadOnly: c.ReadOnly,
		Env:      envCopy,
		Filesystem: FilesystemPolicy{
			DeniedReadPaths:  canonicalPaths(c.Filesystem.DeniedReadPaths),
			AllowReadPaths:   canonicalPaths(c.Filesystem.AllowReadPaths),
			ExtraWritePaths:  canonicalPaths(c.Filesystem.ExtraWritePaths),
			DeniedWritePaths: canonicalPaths(c.Filesystem.DeniedWritePaths),
		},
		Network: NetworkPolicy{
			Disabled:      c.Network.Disabled,
			AllowedHosts:  allowed,
			BlockedHosts:  blocked,
			AllowLoopback: c.Network.AllowLoopback,
		},
		DefaultTimeout: c.DefaultTimeout,
	}

	data, err := json.Marshal(canonical)
	if err != nil {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%+v", canonical)))
		return hex.EncodeToString(sum[:])
	}

	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
