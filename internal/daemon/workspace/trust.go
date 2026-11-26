package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/warp"
)

// TrustedFolder records a trusted path on disk and its fingerprint metadata.
type TrustedFolder struct {
	Path        string         `json:"path"`
	Scope       api.TrustScope `json:"scope"`
	TrustedAt   time.Time      `json:"trustedAt"`
	Fingerprint string         `json:"fingerprint,omitempty"`
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
	Decisions   []string `json:"decisions,omitempty"`
	HasEnv      bool     `json:"has_env,omitempty"`
}

// HasExecutables reports whether any executable items or local .env are present.
func (s ExecutableConfigSummary) HasExecutables() bool {
	return len(s.MCPServers) > 0 || len(s.CustomTools) > 0 || len(s.Decisions) > 0 || s.HasEnv
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

	return fsutils.WriteJSONAtomic(p, store, 0o600)
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

	trustMu.RLock()
	defer trustMu.RUnlock()

	store, err := loadTrustStoreLocked()
	if err != nil {
		return false, false, err
	}

	var folderEntry, parentEntry *TrustedFolder
	var parentClean string
	for i := range store.Folders {
		entry := &store.Folders[i]
		entryClean, err := fsutils.CleanPath(entry.Path)
		if err != nil {
			continue
		}

		if entry.Scope == api.TrustScopeParent {
			if parentEntry == nil && cleanDir != entryClean && fsutils.IsInsideDir(cleanDir, entryClean) {
				parentEntry = entry
				parentClean = entryClean
			}
			continue
		}

		if cleanDir == entryClean {
			folderEntry = entry
			break
		}
	}

	if folderEntry != nil {
		currentFP, err := ComputeExecutableFingerprint(cleanDir)
		if err != nil {
			return false, true, err
		}
		return true, currentFP != folderEntry.Fingerprint, nil
	}

	if parentEntry != nil {
		if _, err := ComputeExecutableFingerprint(cleanDir); err != nil {
			return false, true, err
		}
		parentFP, err := ComputeExecutableFingerprint(parentClean)
		if err != nil {
			return false, true, err
		}
		return true, parentFP != parentEntry.Fingerprint, nil
	}

	return false, false, nil
}

// TrustFolder marks a directory as trusted with the given scope.
func TrustFolder(dir string, scope api.TrustScope) error {
	cleanDir, err := fsutils.CleanPath(dir)
	if err != nil {
		return err
	}
	if scope == "" {
		scope = api.TrustScopeFolder
	}
	if !scope.Valid() {
		return fmt.Errorf("trust: invalid scope %q", scope)
	}

	fp, err := ComputeExecutableFingerprint(cleanDir)
	if err != nil {
		return err
	}

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
	return fsutils.WriteJSONAtomic(p, store, 0o600)
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
	return fsutils.WriteJSONAtomic(p, store, 0o600)
}

// LookupTrustEntry returns any exact trust entry stored for dir.
func LookupTrustEntry(dir string) (TrustedFolder, bool, error) {
	cleanDir, err := fsutils.CleanPath(dir)
	if err != nil {
		return TrustedFolder{}, false, err
	}

	trustMu.RLock()
	defer trustMu.RUnlock()

	store, err := loadTrustStoreLocked()
	if err != nil {
		return TrustedFolder{}, false, err
	}

	for _, entry := range store.Folders {
		entryClean, err := fsutils.CleanPath(entry.Path)
		if err == nil && entryClean == cleanDir {
			return entry, true, nil
		}
	}
	return TrustedFolder{}, false, nil
}

// RestoreTrustEntry restores dir's trust entry to a prior snapshot, or removes
// it when existed is false.
func RestoreTrustEntry(dir string, prev TrustedFolder, existed bool) error {
	if !existed {
		return RevokeTrust(dir)
	}

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

	updated := false
	for i, entry := range store.Folders {
		entryClean, err := fsutils.CleanPath(entry.Path)
		if err == nil && entryClean == cleanDir {
			store.Folders[i] = prev
			updated = true
			break
		}
	}
	if !updated {
		store.Folders = append(store.Folders, prev)
	}

	p, err := TrustFilePath()
	if err != nil {
		return fmt.Errorf("trust: resolve path: %w", err)
	}
	return fsutils.WriteJSONAtomic(p, store, 0o600)
}

// ScanExecutableConfig checks what MCP servers, custom tools, and .env files exist in dir.
func ScanExecutableConfig(dir string) (ExecutableConfigSummary, error) {
	summary, _, err := inspectExecutables(dir)
	return summary, err
}

func ComputeExecutableFingerprint(dir string) (string, error) {
	_, digest, err := inspectExecutables(dir)
	return digest, err
}

func trustRoots(cwd string) (string, []string, error) {
	root, wsDef, err := findEnclosingWorkspace(cwd)
	if err != nil {
		return "", nil, err
	}
	if wsDef == nil {
		return cwd, []string{cwd}, nil
	}

	roots, err := expandTrustProjectRoots(root, cwd, wsDef.Spec.Projects)
	if err != nil {
		return "", nil, err
	}
	return root, roots, nil
}

func findEnclosingWorkspace(cwd string) (string, *warp.WorkspaceDef, error) {
	for dir := cwd; ; dir = filepath.Dir(dir) {
		data, err := fsutils.ReadRegularFile(filepath.Join(dir, warp.WorkspaceFileName))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", nil, err
		}
		if err == nil {
			if res, parseErr := warp.Parse(warp.WorkspaceFileName, string(data)); parseErr == nil && res.Kind == warp.KindWorkspace {
				return dir, res.Resource.(*warp.WorkspaceDef), nil
			}
		}
		if dir == filepath.Dir(dir) {
			return "", nil, nil
		}
	}
}

func expandTrustProjectRoots(root, cwd string, projects []string) ([]string, error) {
	roots := map[string]bool{cwd: true, root: true}

	if len(projects) == 1 && projects[0] == "*" {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				roots[filepath.Join(root, e.Name())] = true
			}
		}
		return slices.Sorted(maps.Keys(roots)), nil
	}

	for _, project := range projects {
		path := filepath.Join(root, project)
		if !fsutils.IsConfinedInsideDir(path, root) {
			return nil, fmt.Errorf("project outside workspace: %s", project)
		}
		roots[path] = true
	}
	return slices.Sorted(maps.Keys(roots)), nil
}

func inspectExecutables(dir string) (ExecutableConfigSummary, string, error) {
	var summary ExecutableConfigSummary
	cwd, err := fsutils.CleanPath(dir)
	if err != nil {
		return summary, "", err
	}
	root, projects, err := trustRoots(cwd)
	if err != nil {
		return summary, "", err
	}
	h := sha256.New()
	hashFile := func(rel string, data []byte) {
		_, _ = fmt.Fprintf(h, "%d:%s%x", len(rel), rel, sha256.Sum256(data))
	}
	for _, project := range projects {
		agents := filepath.Join(project, warp.DefaultAgentsDir)
		if _, err := os.Lstat(agents); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return summary, "", err
		}
		err := filepath.WalkDir(agents, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if d.Type()&fs.ModeSymlink != 0 {
				info, err := os.Stat(path)
				if err != nil {
					return err
				}
				if info.IsDir() {
					return fmt.Errorf("cannot inspect linked directory: %s", path)
				}
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			local, err := filepath.Rel(agents, path)
			if err != nil {
				return err
			}
			sub, _, _ := strings.Cut(local, string(filepath.Separator))
			executableDir := sub == "tools" || sub == "decisions" || sub == "mcps"
			ext := filepath.Ext(path)
			if !executableDir && ext != ".md" && ext != ".yaml" && ext != ".yml" {
				return nil
			}
			data, err := fsutils.ReadRegularFile(path)
			if err != nil {
				return err
			}
			stem := strings.TrimSuffix(d.Name(), ext)
			var kind warp.Kind
			if parsed, err := warp.Parse(local, string(data)); err == nil {
				kind = parsed.Kind
			}
			switch {
			case kind == warp.KindTool || strings.HasSuffix(stem, ".tool"):
				summary.CustomTools = append(summary.CustomTools, rel)
			case kind == warp.KindMCP || strings.HasSuffix(stem, ".mcp"):
				summary.MCPServers = append(summary.MCPServers, rel)
			case kind == warp.KindDecision || strings.HasSuffix(stem, ".decision"):
				summary.Decisions = append(summary.Decisions, rel)
			default:
				if !executableDir {
					return nil
				}
			}
			hashFile(rel, data)
			return nil
		})
		if err != nil {
			return summary, "", err
		}
	}
	envDirs := []string{}
	for curr := cwd; ; curr = filepath.Dir(curr) {
		envDirs = append(envDirs, curr)
		if curr == root {
			break
		}
	}
	parent := filepath.Dir(root)
	if info, err := os.Stat(filepath.Join(parent, ".bare")); err == nil && info.IsDir() && parent != root {
		envDirs = append(envDirs, parent)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return summary, "", err
	}
	for _, dir := range envDirs {
		path := filepath.Join(dir, ".env")
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return summary, "", err
		}
		data, err := fsutils.ReadRegularFile(path)
		if err != nil {
			return summary, "", err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return summary, "", err
		}
		hashFile(rel, data)
		summary.HasEnv = true
	}
	return summary, hex.EncodeToString(h.Sum(nil)), nil
}
