package permissions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutil"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

// FSManager implements PermissionManager storing configuration in JSON files.
type FSManager struct {
	globalPath             string
	workspacePath          string
	sessionPath            string
	defaultMode            PermissionMode
	isWorkspaceInitialized func() bool
	mu                     sync.RWMutex
}

// SetWorkspaceInitializedFn configures a callback to check if the workspace is initialized.
func (m *FSManager) SetWorkspaceInitializedFn(fn func() bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isWorkspaceInitialized = fn
}

// Ensure FSManager implements PermissionManager.
var _ PermissionManager = (*FSManager)(nil)

type permissionFileContent struct {
	Mode        PermissionMode `json:"mode,omitempty"`
	Permissions []Permission   `json:"permissions"`
}

// NewFSManager creates a new FSManager using XDG data directory paths based on
// the workspace path and session ID.
func NewFSManager(workspacePath string, sessionID string) (*FSManager, error) {
	globalPath, err := xdg.SubDataDir("permissions.json")
	if err != nil {
		return nil, fmt.Errorf("failed to get global permissions path: %w", err)
	}

	var wsPath string
	if workspacePath != "" {
		wsDir, err := xdg.WorkspaceDir(workspacePath)
		if err != nil {
			return nil, fmt.Errorf("failed to get workspace dir: %w", err)
		}
		wsPath = filepath.Join(wsDir, "permissions.json")
	}

	var sessPath string
	if sessionID != "" {
		sessPath, err = xdg.SubDataDir("sessions", sessionID, "permissions.json")
		if err != nil {
			return nil, fmt.Errorf("failed to get session permissions path: %w", err)
		}
	}

	return &FSManager{
		globalPath:    globalPath,
		workspacePath: wsPath,
		sessionPath:   sessPath,
		defaultMode:   ModeDefault,
	}, nil
}

// NewFSManagerWithPaths creates a new FSManager with explicit file paths.
// This is particularly useful for testing purposes.
func NewFSManagerWithPaths(globalPath, workspacePath, sessionPath string) *FSManager {
	return &FSManager{
		globalPath:    globalPath,
		workspacePath: workspacePath,
		sessionPath:   sessionPath,
		defaultMode:   ModeDefault,
	}
}

// GetGrants retrieves all saved permissions for a specific tool across all active scopes.
// It searches in order of specificity: Session, Workspace, then Global.
func (m *FSManager) GetGrants(ctx context.Context, toolName string) []Permission {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var allGrants []Permission

	paths := []string{m.sessionPath, m.workspacePath, m.globalPath}
	for _, path := range paths {
		if path == "" {
			continue
		}
		content, err := m.readFile(path)
		if err != nil {
			continue
		}
		for _, perm := range content.Permissions {
			if perm.Group == toolName {
				allGrants = append(allGrants, perm)
			}
		}
	}

	return allGrants
}

// GetMode returns the current operating mode of the permission system.
// It searches in order of specificity: Session, Workspace, then Global.
// If a workspace is present but has no configured mode, it defaults to Strict mode
// as a safety fallback, overriding any global mode configuration.
func (m *FSManager) GetMode(ctx context.Context) PermissionMode {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 1. Session scope check (TUI runtime overrides)
	if m.sessionPath != "" {
		content, err := m.readFile(m.sessionPath)
		if err == nil && content.Mode != "" {
			return content.Mode
		}
	}

	// 2. Workspace scope check
	if m.workspacePath != "" {
		content, err := m.readFile(m.workspacePath)
		if err == nil && content.Mode != "" {
			return content.Mode
		}
		// Graceful Degradation: workspace exists but has no mode configured in permissions.json.
		// If workspace is not initialized, default to Strict mode to guarantee user safety.
		// Otherwise, fall through to Global/Default.
		initialized := false
		if m.isWorkspaceInitialized != nil {
			initialized = m.isWorkspaceInitialized()
		}
		if !initialized {
			return ModeStrict
		}
	}

	// 3. Global scope check (only if outside a workspace context)
	if m.globalPath != "" {
		content, err := m.readFile(m.globalPath)
		if err == nil && content.Mode != "" {
			return content.Mode
		}
	}

	return m.defaultMode
}

// SavePermission persists a new grant/deny to the specified scope.
func (m *FSManager) SavePermission(ctx context.Context, scope PermissionScope, perm Permission) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var path string
	switch scope {
	case ScopeSession:
		path = m.sessionPath
	case ScopeWorkspace:
		path = m.workspacePath
	case ScopeGlobal:
		path = m.globalPath
	case ScopeOnce:
		// ScopeOnce is a special one-time approval and is not persisted.
		return nil
	default:
		return fmt.Errorf("invalid permission scope: %q", scope)
	}

	if path == "" {
		return fmt.Errorf("path for scope %q is not configured", scope)
	}

	content, err := m.readFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read permissions file %q: %w", path, err)
	}

	// Check if this exact permission already exists to avoid duplicates
	exists := false
	for _, p := range content.Permissions {
		if p.Group == perm.Group &&
			p.Target == perm.Target &&
			p.MatchMethod == perm.MatchMethod &&
			p.Action == perm.Action &&
			p.AllowedDirectory == perm.AllowedDirectory {
			exists = true
			break
		}
	}

	if !exists {
		content.Permissions = append(content.Permissions, perm)
		if err := m.writeFile(path, content); err != nil {
			return fmt.Errorf("failed to write permissions file %q: %w", path, err)
		}
	}

	return nil
}

// SaveMode persists the permission mode to the specified scope.
func (m *FSManager) SaveMode(ctx context.Context, scope PermissionScope, mode PermissionMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var path string
	switch scope {
	case ScopeSession:
		path = m.sessionPath
	case ScopeWorkspace:
		path = m.workspacePath
	case ScopeGlobal:
		path = m.globalPath
	case ScopeOnce:
		return nil
	default:
		return fmt.Errorf("invalid permission scope: %q", scope)
	}

	if path == "" {
		return fmt.Errorf("path for scope %q is not configured", scope)
	}

	content, err := m.readFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read permissions file %q: %w", path, err)
	}

	content.Mode = mode
	if err := m.writeFile(path, content); err != nil {
		return fmt.Errorf("failed to write permissions file %q: %w", path, err)
	}

	return nil
}

func (m *FSManager) readFile(path string) (permissionFileContent, error) {
	var content permissionFileContent
	data, err := os.ReadFile(path)
	if err != nil {
		return content, err
	}
	if len(data) == 0 {
		return content, nil
	}
	if err := json.Unmarshal(data, &content); err != nil {
		return content, err
	}
	return content, nil
}

func (m *FSManager) writeFile(path string, content permissionFileContent) error {
	dir := filepath.Dir(path)
	if err := fsutil.EnsureDir(dir); err != nil {
		return err
	}

	data, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
