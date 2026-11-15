// Package sandboxcfg persists and merges the user-facing sandbox.json
// configuration layer that decides *what* the kernel boundary allows once a
// process runs (mode, availability locks, unsandboxed escape hatches, and
// filesystem/network overrides).
//
// It mirrors the two-scope layout of the permissions package
// (permissions.json decides *whether* a tool call requires approval;
// sandbox.json decides *what* the OS isolation permits), keeping
// authorization and isolation independent:
//
//   - global:  $XDG_CONFIG_HOME/tasksmith/sandbox.json
//   - project: $XDG_DATA_HOME/tasksmith/workspaces/<hash>/sandbox.json
//
// Effective settings follow the workspace trust boundary: repository-committed
// files can never widen these scopes, boolean knobs inherit across scopes via
// pointer optionality, and the global scope holds monotonic locks (a global
// "failIfUnavailable": true or "allowUnsandboxed": false can never be relaxed
// by the project scope).
package sandboxcfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

// Sentinel errors returned by the sandboxcfg package.
var (
	ErrInvalidConfig = errors.New("sandboxcfg: invalid configuration")
	ErrInvalidScope  = errors.New("sandboxcfg: unsupported scope")
	ErrInvalidValue  = errors.New("sandboxcfg: invalid sandbox setting")
)

// Config configures the per-workspace sandbox [Manager].
type Config struct {
	// WorkDir is the workspace root used to resolve relative project paths and
	// to anchor the built-in security floor (credential masks, protected
	// workspace write paths).
	WorkDir string

	// Restricted reports whether the workspace is currently untrusted. It
	// forces allowUnsandboxed false in Effective() and can be updated later
	// via Manager.SetRestricted.
	Restricted bool

	// GlobalPath optionally overrides $XDG_CONFIG_HOME/tasksmith/sandbox.json
	// (tests).
	GlobalPath string

	// ProjectPath optionally overrides
	// $XDG_DATA_HOME/tasksmith/workspaces/<hash>/sandbox.json (tests).
	ProjectPath string
}

// Manager coordinates global and project sandbox.json persistence and
// computes the effective per-workspace settings.
type Manager interface {
	// WorkDir returns the canonical workspace root the manager is bound to.
	WorkDir() string

	// SetRestricted updates the trust posture reflected by Effective().
	SetRestricted(restricted bool)

	// Restricted reports whether the workspace is currently untrusted.
	Restricted() bool

	// Get returns the stored [api.SandboxSettings] for ScopeProject or
	// ScopeGlobal. A missing file yields zero settings.
	Get(scope api.PermissionScope) (api.SandboxSettings, error)

	// Set validates and atomically overwrites the stored settings for
	// ScopeProject or ScopeGlobal.
	Set(scope api.PermissionScope, settings api.SandboxSettings) error

	// Effective merges global and project scopes over the built-in security
	// floor following the workspace precedence rules.
	Effective() (Effective, error)

	// EffectiveFor merges the scopes like Effective but resolves project-
	// relative paths and the built-in security floor against the given work
	// directory (e.g. a subagent's isolated worktree root). An empty workDir
	// falls back to the manager's workspace root.
	EffectiveFor(workDir string) (Effective, error)
}

type manager struct {
	mu          sync.RWMutex
	workDir     string
	restricted  bool
	globalPath  string
	projectPath string
}

var _ Manager = (*manager)(nil)

// New constructs a [Manager] bound to the given workspace root.
func New(cfg Config) (Manager, error) {
	workDir := strings.TrimSpace(cfg.WorkDir)
	if workDir == "" {
		return nil, fmt.Errorf("%w: workspace root is required", ErrInvalidConfig)
	}

	globalPath := strings.TrimSpace(cfg.GlobalPath)
	if globalPath == "" {
		p, err := xdg.SubConfigDir("sandbox.json")
		if err != nil {
			return nil, fmt.Errorf("sandboxcfg: resolve global config dir: %w", err)
		}
		globalPath = p
	}

	projectPath := strings.TrimSpace(cfg.ProjectPath)
	if projectPath == "" {
		wsDataDir, err := xdg.WorkspaceDir(workDir)
		if err != nil {
			return nil, fmt.Errorf("sandboxcfg: resolve workspace dir: %w", err)
		}
		projectPath = filepath.Join(wsDataDir, "sandbox.json")
	}

	return &manager{
		workDir:     workDir,
		restricted:  cfg.Restricted,
		globalPath:  globalPath,
		projectPath: projectPath,
	}, nil
}

func (m *manager) WorkDir() string { return m.workDir }

func (m *manager) SetRestricted(restricted bool) {
	m.mu.Lock()
	m.restricted = restricted
	m.mu.Unlock()
}

func (m *manager) Restricted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.restricted
}

func (m *manager) filePath(scope api.PermissionScope) (string, error) {
	switch scope {
	case api.ScopeGlobal:
		return m.globalPath, nil
	case api.ScopeProject:
		return m.projectPath, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidScope, scope)
	}
}

func (m *manager) readFromFile(path string) (api.SandboxSettings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return api.SandboxSettings{}, nil
		}
		return api.SandboxSettings{}, fmt.Errorf("sandboxcfg: read %q: %w", path, err)
	}
	var settings api.SandboxSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return api.SandboxSettings{}, fmt.Errorf("sandboxcfg: parse %q: %w", path, err)
	}
	return settings, nil
}

// writeToFile validates and atomically replaces the settings file with 0o600
// permissions: sandbox.json can widen isolation, so it must never be readable
// or writable by other users on shared hosts.
func (m *manager) writeToFile(path string, scope api.PermissionScope, settings api.SandboxSettings) error {
	if err := Validate(scope, m.workDir, settings); err != nil {
		return err
	}
	settings.Schema = 1
	settings.ExcludedCommands = deduplicate(settings.ExcludedCommands)
	settings.Filesystem.AllowWrite = deduplicate(settings.Filesystem.AllowWrite)
	settings.Filesystem.DenyWrite = deduplicate(settings.Filesystem.DenyWrite)
	settings.Filesystem.DenyRead = deduplicate(settings.Filesystem.DenyRead)
	settings.Filesystem.AllowRead = deduplicate(settings.Filesystem.AllowRead)
	settings.Network.AllowedDomains = deduplicateLower(settings.Network.AllowedDomains)
	settings.Network.DeniedDomains = deduplicateLower(settings.Network.DeniedDomains)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("sandboxcfg: create directory: %w", err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("sandboxcfg: marshal settings: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".sandbox-*.tmp")
	if err != nil {
		return fmt.Errorf("sandboxcfg: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("sandboxcfg: chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("sandboxcfg: write %q: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("sandboxcfg: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("sandboxcfg: replace %q: %w", path, err)
	}
	return nil
}

// Get returns the stored [api.SandboxSettings] for the given scope.
func (m *manager) Get(scope api.PermissionScope) (api.SandboxSettings, error) {
	path, err := m.filePath(scope)
	if err != nil {
		return api.SandboxSettings{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	settings, err := m.readFromFile(path)
	if err != nil {
		return api.SandboxSettings{}, err
	}
	return settings.Clone(), nil
}

// Set validates and atomically overwrites the stored settings for the scope.
func (m *manager) Set(scope api.PermissionScope, settings api.SandboxSettings) error {
	path, err := m.filePath(scope)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeToFile(path, scope, settings.Clone())
}

func deduplicate(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	var out []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

func deduplicateLower(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	var out []string
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

func unionDedup(groups ...[]string) []string {
	var all []string
	for _, g := range groups {
		all = append(all, g...)
	}
	return deduplicate(all)
}
