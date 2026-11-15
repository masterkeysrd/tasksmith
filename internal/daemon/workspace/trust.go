package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/warp"
)

// TrustScope determines the boundary of a trust decision.
type TrustScope string

const (
	// TrustScopeFolder trusts only the specific folder path.
	TrustScopeFolder TrustScope = "folder"
	// TrustScopeParent trusts every subfolder under the parent path.
	TrustScopeParent TrustScope = "parent"
)

// TrustedFolder records a trusted path on disk and its fingerprint metadata.
type TrustedFolder struct {
	Path        string     `json:"path"`
	Scope       TrustScope `json:"scope"`
	TrustedAt   time.Time  `json:"trustedAt"`
	Fingerprint string     `json:"fingerprint,omitempty"`
}

// TrustStore represents the persisted trust.json schema in XDG_DATA_HOME.
type TrustStore struct {
	Schema  int             `json:"schema"`
	Folders []TrustedFolder `json:"folders"`
}

// ExecutableConfigSummary summarizes the executable configurations found in a folder.
type ExecutableConfigSummary struct {
	MCPServers  []string `json:"mcp_servers,omitempty"`
	CustomTools []string `json:"custom_tools,omitempty"`
	HasEnv      bool     `json:"has_env,omitempty"`
}

// HasExecutables reports whether any executable items or local .env are present.
func (s ExecutableConfigSummary) HasExecutables() bool {
	return len(s.MCPServers) > 0 || len(s.CustomTools) > 0 || s.HasEnv
}

var trustMu sync.RWMutex

// TrustFilePath returns the absolute path to $XDG_DATA_HOME/tasksmith/trust.json.
func TrustFilePath() (string, error) {
	return xdg.SubDataDir("trust.json")
}

// LoadTrustStore reads and unmarshals trust.json from XDG_DATA_HOME.
// If the file does not exist, an empty store with Schema=1 is returned.
func LoadTrustStore() (*TrustStore, error) {
	trustMu.RLock()
	defer trustMu.RUnlock()
	return loadTrustStoreLocked()
}

func loadTrustStoreLocked() (*TrustStore, error) {
	p, err := TrustFilePath()
	if err != nil {
		return nil, fmt.Errorf("trust: resolve path: %w", err)
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &TrustStore{Schema: 1, Folders: []TrustedFolder{}}, nil
		}
		return nil, fmt.Errorf("trust: read file: %w", err)
	}

	var store TrustStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("trust: unmarshal: %w", err)
	}
	if store.Schema == 0 {
		store.Schema = 1
	}
	return &store, nil
}

// SaveTrustStore atomically writes the trust store to XDG_DATA_HOME/trust.json.
func SaveTrustStore(store *TrustStore) error {
	trustMu.Lock()
	defer trustMu.Unlock()

	if store == nil {
		return errors.New("trust: store must not be nil")
	}
	if store.Schema == 0 {
		store.Schema = 1
	}

	p, err := TrustFilePath()
	if err != nil {
		return fmt.Errorf("trust: resolve path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("trust: create dir: %w", err)
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("trust: marshal: %w", err)
	}
	data = append(data, '\n')

	tmpPath := fmt.Sprintf("%s.tmp.%d", p, time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("trust: write temp: %w", err)
	}
	if err := os.Rename(tmpPath, p); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("trust: rename: %w", err)
	}
	return nil
}

// IsTrusted reports whether dir is trusted by any entry in the trust store.
// If an entry is found, it also checks if the directory's executable configuration
// fingerprint has changed since it was trusted (re-prompt condition).
// Returns (isTrusted, fingerprintChanged, error).
func IsTrusted(dir string) (bool, bool, error) {
	cleanDir, err := fsutils.CleanPath(dir)
	if err != nil {
		return false, false, err
	}

	store, err := LoadTrustStore()
	if err != nil {
		return false, false, err
	}

	trustMu.RLock()
	defer trustMu.RUnlock()

	for _, entry := range store.Folders {
		entryClean, err := fsutils.CleanPath(entry.Path)
		if err != nil {
			continue
		}

		matched := false
		if entry.Scope == TrustScopeParent {
			rel, relErr := filepath.Rel(entryClean, cleanDir)
			if relErr == nil && !strings.HasPrefix(rel, "..") && rel != "." {
				matched = true
			}
		} else {
			if cleanDir == entryClean {
				matched = true
			}
		}

		if matched {
			// Check fingerprint if recorded
			if entry.Fingerprint != "" {
				currentFP, _ := ComputeExecutableFingerprint(cleanDir)
				if currentFP != "" && currentFP != entry.Fingerprint {
					return true, true, nil
				}
			}
			return true, false, nil
		}
	}

	return false, false, nil
}

// TrustFolder marks a directory as trusted with the given scope.
func TrustFolder(dir string, scope TrustScope) error {
	cleanDir, err := fsutils.CleanPath(dir)
	if err != nil {
		return err
	}
	if scope == "" {
		scope = TrustScopeFolder
	}

	fp, _ := ComputeExecutableFingerprint(cleanDir)

	trustMu.Lock()
	defer trustMu.Unlock()

	store, err := loadTrustStoreLocked()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	updated := false
	for i, entry := range store.Folders {
		entryClean, err := fsutils.CleanPath(entry.Path)
		if err != nil {
			continue
		}
		if entryClean == cleanDir {
			store.Folders[i].Scope = scope
			store.Folders[i].TrustedAt = now
			store.Folders[i].Fingerprint = fp
			updated = true
			break
		}
	}

	if !updated {
		store.Folders = append(store.Folders, TrustedFolder{
			Path:        cleanDir,
			Scope:       scope,
			TrustedAt:   now,
			Fingerprint: fp,
		})
	}

	p, err := TrustFilePath()
	if err != nil {
		return fmt.Errorf("trust: resolve path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("trust: create dir: %w", err)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("trust: marshal: %w", err)
	}
	return os.WriteFile(p, append(data, '\n'), 0o600)
}

// RevokeTrust removes dir from the trust store.
func RevokeTrust(dir string) error {
	cleanDir, err := fsutils.CleanPath(dir)
	if err != nil {
		return err
	}

	trustMu.Lock()
	defer trustMu.Unlock()

	store, err := loadTrustStoreLocked()
	if err != nil {
		return err
	}

	var remaining []TrustedFolder
	for _, entry := range store.Folders {
		entryClean, err := fsutils.CleanPath(entry.Path)
		if err == nil && entryClean == cleanDir {
			continue
		}
		remaining = append(remaining, entry)
	}
	store.Folders = remaining

	p, err := TrustFilePath()
	if err != nil {
		return fmt.Errorf("trust: resolve path: %w", err)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("trust: marshal: %w", err)
	}
	return os.WriteFile(p, append(data, '\n'), 0o600)
}

// ScanExecutableConfig checks what MCP servers, custom tools, and .env files exist in dir.
func ScanExecutableConfig(dir string) (ExecutableConfigSummary, error) {
	cleanDir, err := fsutils.CleanPath(dir)
	if err != nil {
		return ExecutableConfigSummary{}, err
	}

	var summary ExecutableConfigSummary

	// 1. Check for .env file
	envPath := filepath.Join(cleanDir, ".env")
	if info, err := os.Stat(envPath); err == nil && !info.IsDir() {
		summary.HasEnv = true
	}

	// 2. Check for .agents/mcps
	mcpsDir := filepath.Join(cleanDir, warp.DefaultAgentsDir, "mcps")
	if entries, err := os.ReadDir(mcpsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && (strings.HasSuffix(e.Name(), ".mcp.yaml") || strings.HasSuffix(e.Name(), ".mcp.yml")) {
				relPath := filepath.Join(warp.DefaultAgentsDir, "mcps", e.Name())
				summary.MCPServers = append(summary.MCPServers, relPath)
			}
		}
	}

	// 3. Check for .agents/tools
	toolsDir := filepath.Join(cleanDir, warp.DefaultAgentsDir, "tools")
	if entries, err := os.ReadDir(toolsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && (strings.HasSuffix(e.Name(), ".tool.md") || strings.HasSuffix(e.Name(), ".tool.yaml")) {
				relPath := filepath.Join(warp.DefaultAgentsDir, "tools", e.Name())
				summary.CustomTools = append(summary.CustomTools, relPath)
			}
		}
	}

	return summary, nil
}

// ComputeExecutableFingerprint calculates a SHA-256 hash over the contents of
// .agents/mcps/** and .agents/tools/** to detect modifications after a pull.
func ComputeExecutableFingerprint(dir string) (string, error) {
	cleanDir, err := fsutils.CleanPath(dir)
	if err != nil {
		return "", err
	}

	h := sha256.New()
	hasItems := false

	agentsDir := filepath.Join(cleanDir, warp.DefaultAgentsDir)
	for _, sub := range []string{"mcps", "tools"} {
		targetDir := filepath.Join(agentsDir, sub)
		if _, err := os.Stat(targetDir); err != nil {
			continue
		}
		_ = filepath.WalkDir(targetDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			content, readErr := os.ReadFile(p)
			if readErr != nil {
				return nil
			}
			hasItems = true
			rel, _ := filepath.Rel(cleanDir, p)
			h.Write([]byte(rel))
			h.Write(content)
			return nil
		})
	}

	if !hasItems {
		return "", nil
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
