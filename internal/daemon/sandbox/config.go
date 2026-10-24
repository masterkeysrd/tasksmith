package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

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
	switch mode {
	case ModeLocal, ModeNative:
	default:
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
	sort.Strings(allowed)

	blocked := make([]string, len(c.Network.BlockedHosts))
	for i, h := range c.Network.BlockedHosts {
		blocked[i] = strings.ToLower(strings.TrimSpace(h))
	}
	sort.Strings(blocked)

	envCopy := make(map[string]string, len(c.Env))
	maps.Copy(envCopy, c.Env)

	canonical := struct {
		Mode           Mode              `json:"mode"`
		WorkDir        string            `json:"work_dir"`
		ReadOnly       bool              `json:"read_only"`
		Env            map[string]string `json:"env,omitempty"`
		Network        NetworkPolicy     `json:"network"`
		DefaultTimeout time.Duration     `json:"default_timeout,omitempty"`
	}{
		Mode:     mode,
		WorkDir:  workDir,
		ReadOnly: c.ReadOnly,
		Env:      envCopy,
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
