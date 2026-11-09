package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/version"
	"golang.org/x/mod/semver"
)

// WorkspaceMetadataSchema defines the current format version of workspace.json.
const WorkspaceMetadataSchema = 1

// WorkspaceMetadataFile is the name of the per-workspace metadata file.
const WorkspaceMetadataFile = "workspace.json"

const legacySetupFile = "setup.json"

// Metadata represents the persisted workspace.json state.
type Metadata struct {
	Schema  int    `json:"schema"`
	Version string `json:"version"`
}

// MigrationAction defines an atomic version migration step for a workspace.
type MigrationAction struct {
	TargetVersion    string // Semver string, e.g. "v1.1.2"
	Description      string
	TouchesCommitted bool // If true, skipped on automatic open and requires review via /setup
	Apply            func(ctx context.Context, wsDir string) error
}

var (
	migrationsMu sync.RWMutex
	migrations   []MigrationAction
)

// RegisterMigration registers a version migration action.
func RegisterMigration(action MigrationAction) {
	migrationsMu.Lock()
	defer migrationsMu.Unlock()
	migrations = append(migrations, action)
}

// RegisteredMigrations returns a copy of all registered migration actions.
func RegisteredMigrations() []MigrationAction {
	migrationsMu.RLock()
	defer migrationsMu.RUnlock()
	out := make([]MigrationAction, len(migrations))
	copy(out, migrations)
	return out
}

// MetadataPath returns the absolute path to workspace.json for dir.
func MetadataPath(dir string) (string, error) {
	cleanDir, err := fsutils.CleanPath(dir)
	if err != nil {
		return "", err
	}
	wsDataDir, err := xdg.WorkspaceDir(cleanDir)
	if err != nil {
		return "", fmt.Errorf("workspace: resolve workspace data dir: %w", err)
	}
	return filepath.Join(wsDataDir, WorkspaceMetadataFile), nil
}

// ReadMetadata reads and parses the workspace.json file for dir.
// If workspace.json does not exist but legacy setup.json is found,
// it parses setup.json and returns a migrated Metadata.
func ReadMetadata(dir string) (*Metadata, error) {
	cleanDir, err := fsutils.CleanPath(dir)
	if err != nil {
		return nil, err
	}

	wsDataDir, err := xdg.WorkspaceDir(cleanDir)
	if err != nil {
		return nil, fmt.Errorf("workspace: resolve workspace data dir: %w", err)
	}

	metaPath := filepath.Join(wsDataDir, WorkspaceMetadataFile)
	data, err := os.ReadFile(metaPath)
	if err == nil {
		var meta Metadata
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, fmt.Errorf("workspace: parse %s: %w", metaPath, err)
		}
		return &meta, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	// Fallback to legacy setup.json if present
	legacyPath := filepath.Join(wsDataDir, legacySetupFile)
	legacyData, lerr := os.ReadFile(legacyPath)
	if lerr == nil {
		var legacy struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(legacyData, &legacy); err == nil && legacy.Version != "" {
			return &Metadata{
				Schema:  WorkspaceMetadataSchema,
				Version: version.Normalize(legacy.Version),
			}, nil
		}
		return &Metadata{
			Schema:  WorkspaceMetadataSchema,
			Version: "v1.0.0",
		}, nil
	}

	return nil, os.ErrNotExist
}

// WriteMetadata writes meta to workspace.json for dir atomically.
func WriteMetadata(dir string, meta Metadata) error {
	cleanDir, err := fsutils.CleanPath(dir)
	if err != nil {
		return err
	}

	wsDataDir, err := xdg.WorkspaceDir(cleanDir)
	if err != nil {
		return fmt.Errorf("workspace: resolve workspace data dir: %w", err)
	}

	if err := os.MkdirAll(wsDataDir, 0o755); err != nil {
		return fmt.Errorf("workspace: create workspace data dir: %w", err)
	}

	if meta.Schema == 0 {
		meta.Schema = WorkspaceMetadataSchema
	}
	meta.Version = version.Normalize(meta.Version)

	bytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: marshal %s: %w", WorkspaceMetadataFile, err)
	}
	bytes = append(bytes, '\n')

	metaPath := filepath.Join(wsDataDir, WorkspaceMetadataFile)
	tmpPath := metaPath + ".tmp"
	if err := os.WriteFile(tmpPath, bytes, 0o644); err != nil {
		return fmt.Errorf("workspace: write tmp %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, metaPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("workspace: commit %s: %w", metaPath, err)
	}

	return nil
}

// EnsureMetadata ensures that workspace.json exists for a configured workspace dir.
func EnsureMetadata(dir string, runningVersion string) (*Metadata, error) {
	meta, err := ReadMetadata(dir)
	if err == nil {
		return meta, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	// If workspace is configured, create default workspace.json
	if IsConfigured(dir) {
		v := version.Normalize(runningVersion)
		newMeta := Metadata{
			Schema:  WorkspaceMetadataSchema,
			Version: v,
		}
		if err := WriteMetadata(dir, newMeta); err != nil {
			return nil, err
		}
		return &newMeta, nil
	}

	return nil, os.ErrNotExist
}

// isDevVersion reports whether v is a development non-release version.
func isDevVersion(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || v == "(devel)" || strings.Contains(v, "dev") {
		return true
	}
	norm := version.Normalize(v)
	return !semver.IsValid(norm)
}

// RunMigrations checks the stored version of a workspace against runningVersion
// and executes applicable migrations in ascending semver order.
//
// Rules matching §2.2:
//  1. On workspace open, compare stored version with running version.
//  2. If older, run every registered migration action whose target version is in (stored, running],
//     in ascending semver order. Actions touching committed files are skipped for automatic execution.
//  3. After all succeed, write running version to workspace.json. If one fails, stop, keep
//     the last successfully applied version, and return the error.
//  4. If stored is newer than running (downgrade), run nothing, leave the file untouched, and warn.
//  5. Development builds skip migrations.
func RunMigrations(ctx context.Context, dir string, runningVersion string) error {
	cleanDir, err := fsutils.CleanPath(dir)
	if err != nil {
		return err
	}

	// Rule 5: Dev builds skip migrations
	if isDevVersion(runningVersion) {
		// Ensure metadata file exists if configured
		_, _ = EnsureMetadata(cleanDir, runningVersion)
		return nil
	}

	running := version.Normalize(runningVersion)
	meta, err := ReadMetadata(cleanDir)
	if err != nil {
		if os.IsNotExist(err) {
			if IsConfigured(cleanDir) {
				return WriteMetadata(cleanDir, Metadata{
					Schema:  WorkspaceMetadataSchema,
					Version: running,
				})
			}
			return nil
		}
		return err
	}

	stored := version.Normalize(meta.Version)
	if isDevVersion(stored) {
		// Stored version was a dev build, update to running version
		meta.Version = running
		return WriteMetadata(cleanDir, *meta)
	}

	cmp := semver.Compare(stored, running)
	if cmp == 0 {
		return nil
	}

	if cmp > 0 {
		// Rule 4: Stored is newer than running (downgrade)
		slog.Warn("workspace: stored version is newer than binary version (downgrade detected); skipping migrations",
			"workspace", cleanDir,
			"stored", stored,
			"running", running,
		)
		return nil
	}

	// cmp < 0: stored is older than running -> migrations needed
	migrationsMu.RLock()
	var applicable []MigrationAction
	for _, action := range migrations {
		target := version.Normalize(action.TargetVersion)
		if semver.Compare(target, stored) > 0 && semver.Compare(target, running) <= 0 {
			applicable = append(applicable, action)
		}
	}
	migrationsMu.RUnlock()

	// Sort ascending by semver
	sort.SliceStable(applicable, func(i, j int) bool {
		vI := version.Normalize(applicable[i].TargetVersion)
		vJ := version.Normalize(applicable[j].TargetVersion)
		return semver.Compare(vI, vJ) < 0
	})

	lastAppliedVersion := stored
	for _, action := range applicable {
		if action.TouchesCommitted {
			// Rule 6: Actions touching committed files are not applied silently.
			// They surface in /setup and go through Review.
			continue
		}

		if action.Apply != nil {
			if err := action.Apply(ctx, cleanDir); err != nil {
				// Stop and record last successfully applied version
				_ = WriteMetadata(cleanDir, Metadata{
					Schema:  WorkspaceMetadataSchema,
					Version: lastAppliedVersion,
				})
				return fmt.Errorf("workspace migration %s (%s) failed: %w", action.TargetVersion, action.Description, err)
			}
		}
		lastAppliedVersion = version.Normalize(action.TargetVersion)
	}

	// Update to running version
	return WriteMetadata(cleanDir, Metadata{
		Schema:  WorkspaceMetadataSchema,
		Version: running,
	})
}

// PendingUpgrades returns any registered migration actions that touch committed
// files and are applicable for the current workspace.
func PendingUpgrades(dir string, runningVersion string) ([]MigrationAction, error) {
	cleanDir, err := fsutils.CleanPath(dir)
	if err != nil {
		return nil, err
	}

	if isDevVersion(runningVersion) {
		return nil, nil
	}

	running := version.Normalize(runningVersion)
	meta, err := ReadMetadata(cleanDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	stored := version.Normalize(meta.Version)
	if isDevVersion(stored) || semver.Compare(stored, running) >= 0 {
		return nil, nil
	}

	migrationsMu.RLock()
	defer migrationsMu.RUnlock()

	var pending []MigrationAction
	for _, action := range migrations {
		if !action.TouchesCommitted {
			continue
		}
		target := version.Normalize(action.TargetVersion)
		if semver.Compare(target, stored) > 0 && semver.Compare(target, running) <= 0 {
			pending = append(pending, action)
		}
	}

	sort.SliceStable(pending, func(i, j int) bool {
		vI := version.Normalize(pending[i].TargetVersion)
		vJ := version.Normalize(pending[j].TargetVersion)
		return semver.Compare(vI, vJ) < 0
	})

	return pending, nil
}
