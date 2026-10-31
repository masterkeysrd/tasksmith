package permissions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/daemon/workspace"
)

// Config configures the root permission [Manager].
type Config struct {
	Workspace  *workspace.Workspace
	GlobalPath string // optional override for tests; defaults to filepath.Join(xdg.ConfigDir(), "permissions.json")
}

// Manager coordinates global and project-level permission persistence and
// attaches session-scoped permission managers.
type Manager interface {
	Get(scope Scope) (Permissions, error)
	Set(scope Scope, perms Permissions) error
	AddRule(scope Scope, effect Effect, rule string) error
	RemoveRule(scope Scope, effect Effect, rule string) error
	Attach(mode api.PermissionMode, initial Permissions, onUpdate func(Permissions) error) SessionManager
}

type manager struct {
	mu          sync.RWMutex
	ws          *workspace.Workspace
	globalPath  string
	projectPath string
}

var _ Manager = (*manager)(nil)

// New constructs a [Manager] configured for the given workspace.
func New(cfg Config) (Manager, error) {
	if cfg.Workspace == nil {
		return nil, fmt.Errorf("%w: workspace is required", ErrInvalidConfig)
	}

	globalPath := strings.TrimSpace(cfg.GlobalPath)
	if globalPath == "" {
		p, err := xdg.SubConfigDir("permissions.json")
		if err != nil {
			return nil, fmt.Errorf("permissions: resolve global config dir: %w", err)
		}
		globalPath = p
	}

	wsDataDir, err := xdg.WorkspaceDir(cfg.Workspace.RootDir())
	if err != nil {
		return nil, fmt.Errorf("permissions: resolve workspace dir: %w", err)
	}
	projectPath := filepath.Join(wsDataDir, "permissions.json")

	return &manager{
		ws:          cfg.Workspace,
		globalPath:  globalPath,
		projectPath: projectPath,
	}, nil
}

func (m *manager) filePath(scope Scope) (string, error) {
	switch scope {
	case ScopeGlobal:
		return m.globalPath, nil
	case ScopeProject:
		return m.projectPath, nil
	default:
		return "", fmt.Errorf("%w: unsupported scope %q", ErrInvalidScope, scope)
	}
}

func (m *manager) readFromFile(path string) (Permissions, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Permissions{}, nil
		}
		return Permissions{}, fmt.Errorf("permissions: read %q: %w", path, err)
	}

	var perms Permissions
	if err := json.Unmarshal(data, &perms); err != nil {
		return Permissions{}, fmt.Errorf("permissions: parse %q: %w", path, err)
	}
	return perms, nil
}

func deduplicateRules(rules []string) []string {
	if len(rules) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(rules))
	var result []string
	for _, r := range rules {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if _, ok := seen[r]; !ok {
			seen[r] = struct{}{}
			result = append(result, r)
		}
	}
	return result
}

func (m *manager) writeToFile(path string, perms Permissions) error {
	if err := perms.Validate(); err != nil {
		return err
	}
	perms.Allow = deduplicateRules(perms.Allow)
	perms.Deny = deduplicateRules(perms.Deny)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("permissions: create directory: %w", err)
	}

	data, err := json.MarshalIndent(perms, "", "  ")
	if err != nil {
		return fmt.Errorf("permissions: marshal permissions: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("permissions: write %q: %w", path, err)
	}
	return nil
}

// Get returns the stored [Permissions] for ScopeProject or ScopeGlobal.
func (m *manager) Get(scope Scope) (Permissions, error) {
	path, err := m.filePath(scope)
	if err != nil {
		return Permissions{}, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	p, err := m.readFromFile(path)
	if err != nil {
		return Permissions{}, err
	}
	return p.Clone(), nil
}

// Set overwrites the stored [Permissions] for ScopeProject or ScopeGlobal.
func (m *manager) Set(scope Scope, perms Permissions) error {
	path, err := m.filePath(scope)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	return m.writeToFile(path, perms)
}

// AddRule appends rule to the specified scope and effect if not already present.
func (m *manager) AddRule(scope Scope, effect Effect, rule string) error {
	if _, _, err := ParseRule(rule); err != nil {
		return err
	}

	path, err := m.filePath(scope)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	perms, err := m.readFromFile(path)
	if err != nil {
		return err
	}

	switch effect {
	case EffectAllow:
		if !slices.Contains(perms.Allow, rule) {
			perms.Allow = append(perms.Allow, rule)
		}
	case EffectDeny:
		if !slices.Contains(perms.Deny, rule) {
			perms.Deny = append(perms.Deny, rule)
		}
	default:
		return fmt.Errorf("permissions: invalid effect %q", effect)
	}

	return m.writeToFile(path, perms)
}

// RemoveRule deletes all occurrences of rule from the specified scope and effect.
func (m *manager) RemoveRule(scope Scope, effect Effect, rule string) error {
	path, err := m.filePath(scope)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	perms, err := m.readFromFile(path)
	if err != nil {
		return err
	}

	switch effect {
	case EffectAllow:
		perms.Allow = slices.DeleteFunc(perms.Allow, func(r string) bool { return r == rule })
	case EffectDeny:
		perms.Deny = slices.DeleteFunc(perms.Deny, func(r string) bool { return r == rule })
	default:
		return fmt.Errorf("permissions: invalid effect %q", effect)
	}

	return m.writeToFile(path, perms)
}

// Attach binds a new [SessionManager] initialized with the given permission mode,
// initial session permissions, and update callback.
func (m *manager) Attach(mode api.PermissionMode, initial Permissions, onUpdate func(Permissions) error) SessionManager {
	if mode == "" {
		mode = api.ModeDefault
	}
	return &sessionManager{
		manager:     m,
		ws:          m.ws,
		mode:        mode,
		permissions: initial.Clone(),
		onUpdate:    onUpdate,
	}
}
