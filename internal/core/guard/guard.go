package guard

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
)

// DefaultProtectedDirs is the default list of protected workspace directories
// where write, delete, or exec operations are classified as SafetyUnsafe.
var DefaultProtectedDirs = []string{".git", ".bare", ".agents"}

// DefaultSensitivePaths is the list of system directory prefixes that are always
// classified as SafetyUnsafe even for read-only operations.
var DefaultSensitivePaths = []string{
	"/etc",
	"/private/etc",
	"/System",
	"/proc",
	"/sys",
	"/dev",
	"/boot",
	"/root",
	"/var/root",
}

// DefaultSensitiveHomeDirs is the list of credential/key directory and file names
// that are always classified as SafetyUnsafe even for read-only operations.
var DefaultSensitiveHomeDirs = []string{
	".ssh",
	".aws",
	".gnupg",
	".kube",
	".docker",
	".netrc",
}

// IsSecretEnvFile reports whether name (a file's base name) is a dotenv secret file
// (such as ".env" or ".env.local") while allowing safe template suffixes
// (".example", ".sample", ".template").
func IsSecretEnvFile(name string) bool {
	if name == ".env" || strings.HasPrefix(name, ".env.") {
		if strings.HasSuffix(name, ".example") ||
			strings.HasSuffix(name, ".sample") ||
			strings.HasSuffix(name, ".template") {
			return false
		}
		return true
	}
	return false
}

// IsProtectedPath reports whether targetPath (either workspace-relative or
// absolute within workspaceDir) resides inside any of the specified protected
// directories. If protectedDirs is empty, DefaultProtectedDirs is used.
func IsProtectedPath(targetPath, workspaceDir string, protectedDirs ...string) bool {
	if len(protectedDirs) == 0 {
		protectedDirs = DefaultProtectedDirs
	}
	cleaned := filepath.Clean(strings.TrimSpace(targetPath))
	checkPath := cleaned
	if filepath.IsAbs(cleaned) && workspaceDir != "" {
		wsClean := filepath.Clean(workspaceDir)
		if rel, err := filepath.Rel(wsClean, cleaned); err == nil && !strings.HasPrefix(rel, "..") {
			checkPath = rel
		}
	}
	for part := range strings.SplitSeq(filepath.ToSlash(checkPath), "/") {
		if slices.Contains(protectedDirs, part) {
			return true
		}
	}
	return false
}

// tasksmithSecretPaths returns TaskSmith's own global secret locations under
// XDG config and data directories ($XDG_CONFIG_HOME/tasksmith/.env,
// $XDG_DATA_HOME/tasksmith/credentials, and $XDG_DATA_HOME/tasksmith/mcp).
func tasksmithSecretPaths() []string {
	var paths []string
	if configDir, err := xdg.Home(xdg.VarTypeConfig); err == nil && strings.TrimSpace(configDir) != "" {
		paths = append(paths, filepath.Join(configDir, ".env"))
	}
	if dataDir, err := xdg.Home(xdg.VarTypeData); err == nil && strings.TrimSpace(dataDir) != "" {
		paths = append(paths,
			filepath.Join(dataDir, "credentials"),
			filepath.Join(dataDir, "mcp"),
		)
	}
	return paths
}

// IsSensitivePath reports whether targetPath points to a sensitive system path
// (e.g. /etc, /proc, /dev), user credential path (e.g. ~/.ssh, ~/.aws), or
// TaskSmith's own global XDG secret stores.
func IsSensitivePath(targetPath, homeDir string) bool {
	cleaned := fsutils.ExpandAndCleanPath(targetPath, "", homeDir)
	for _, sp := range DefaultSensitivePaths {
		if fsutils.IsInsideDir(cleaned, sp) {
			return true
		}
	}
	if homeDir != "" {
		homeClean := filepath.Clean(homeDir)
		for _, dir := range DefaultSensitiveHomeDirs {
			if fsutils.IsInsideDir(cleaned, filepath.Join(homeClean, dir)) {
				return true
			}
		}
	}
	for part := range strings.SplitSeq(filepath.ToSlash(cleaned), "/") {
		if slices.Contains(DefaultSensitiveHomeDirs, part) {
			return true
		}
	}
	canonical := cleaned
	if filepath.IsAbs(cleaned) {
		canonical = fsutils.ResolveDeepestExisting(cleaned)
	}
	for _, sp := range tasksmithSecretPaths() {
		spClean := filepath.Clean(sp)
		if fsutils.IsInsideDir(cleaned, spClean) {
			return true
		}
		if filepath.IsAbs(spClean) && filepath.IsAbs(canonical) {
			if fsutils.IsInsideDir(canonical, fsutils.ResolveDeepestExisting(spClean)) {
				return true
			}
		}
	}
	return false
}

// HostCredentialPaths returns canonical absolute host paths holding user or
// TaskSmith secrets that native OS sandbox profiles must block child processes
// from reading: every entry in DefaultSensitiveHomeDirs under the user's home
// directory plus TaskSmith's XDG secret stores.
//
// Candidates are returned even when they do not exist yet so secrets created
// mid-session (e.g. an OAuth login or /setup writing the credential store) land
// behind an already-active kernel rule. Each candidate is canonicalized via
// fsutils.ResolveDeepestExisting before overlap detection so a dotfiles
// workspace whose ~/.ssh symlinks into workDir stays readable while unrelated
// workspaces mask the resolved target.
func HostCredentialPaths(workDir string) []string {
	var candidates []string
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		for _, name := range DefaultSensitiveHomeDirs {
			candidates = append(candidates, filepath.Join(home, name))
		}
	}
	candidates = append(candidates, tasksmithSecretPaths()...)

	root := fsutils.ResolveDeepestExisting(strings.TrimSpace(workDir))
	var paths []string
	seen := make(map[string]struct{}, len(candidates))
	for _, p := range candidates {
		if strings.TrimSpace(p) == "" {
			continue
		}
		p = fsutils.ResolveDeepestExisting(p)
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		if pathOverlapsWorkspace(p, root) {
			continue
		}
		paths = append(paths, p)
	}
	return paths
}

func pathOverlapsWorkspace(denied, workDir string) bool {
	return workDir == "." || workDir == string(filepath.Separator) ||
		fsutils.IsInsideDir(workDir, denied) || fsutils.IsInsideDir(denied, workDir)
}

// PathConfig encapsulates the path configuration used by EvaluatePathWithConfig.
type PathConfig struct {
	WorkspaceDir  string
	HomeDir       string
	ProtectedDirs []string
	ReadDirs      []string
	WriteDirs     []string
}

// EvaluatePathWithConfig resolves targetPath against cfg.WorkspaceDir (and expands ~ with cfg.HomeDir)
// and classifies its SafetyLevel for the given ActionType:
//   - Sensitive system or credential paths always return SafetyUnsafe.
//   - Paths inside cfg.WorkspaceDir return SafetyUnsafe if targeting a protected directory
//     with a non-read action, or SafetySafe otherwise.
//   - Paths inside cfg.WriteDirs return SafetySafe for ActionRead, ActionWrite, ActionDelete,
//     and SafetyUnsafe for other actions.
//   - Paths inside cfg.ReadDirs return SafetySafe for ActionRead, and SafetyUnsafe for other actions.
//   - Otherwise (outside cfg.WorkspaceDir, cfg.WriteDirs, cfg.ReadDirs):
//     returns SafetyOutside for ActionRead, or SafetyUnsafe for non-read actions.
func EvaluatePathWithConfig(targetPath string, action api.ActionType, cfg PathConfig) (string, api.SafetyLevel) {
	resolved := fsutils.ExpandAndCleanPath(targetPath, cfg.WorkspaceDir, cfg.HomeDir)
	if IsSensitivePath(resolved, cfg.HomeDir) {
		return resolved, api.SafetyUnsafe
	}
	for _, rd := range cfg.ReadDirs {
		if fsutils.IsInsideDir(resolved, rd) {
			if action == api.ActionRead {
				canonical, err := filepath.EvalSymlinks(resolved)
				base, baseErr := filepath.EvalSymlinks(rd)
				if err == nil && baseErr == nil && (!fsutils.IsInsideDir(canonical, base) || IsSensitivePath(canonical, cfg.HomeDir)) {
					return resolved, api.SafetyUnsafe
				}
				return resolved, api.SafetySafe
			}
			return resolved, api.SafetyUnsafe
		}
		if canonical, err := filepath.EvalSymlinks(resolved); err == nil {
			if base, err := filepath.EvalSymlinks(rd); err == nil && fsutils.IsInsideDir(canonical, base) && action != api.ActionRead {
				return resolved, api.SafetyUnsafe
			}
		}
	}
	if cfg.WorkspaceDir != "" && fsutils.IsInsideDir(resolved, cfg.WorkspaceDir) {
		if IsProtectedPath(resolved, cfg.WorkspaceDir, cfg.ProtectedDirs...) && action != api.ActionRead {
			return resolved, api.SafetyUnsafe
		}
		return resolved, api.SafetySafe
	}
	for _, wd := range cfg.WriteDirs {
		if fsutils.IsInsideDir(resolved, wd) {
			if action == api.ActionRead || action == api.ActionWrite || action == api.ActionDelete {
				return resolved, api.SafetySafe
			}
			return resolved, api.SafetyUnsafe
		}
	}
	if action == api.ActionRead {
		return resolved, api.SafetyOutside
	}
	return resolved, api.SafetyUnsafe
}

// EvaluatePath resolves targetPath against workspaceDir (and expands ~ with homeDir)
// and classifies its SafetyLevel for the given ActionType.
func EvaluatePath(targetPath, workspaceDir, homeDir string, action api.ActionType, protectedDirs ...string) (string, api.SafetyLevel) {
	return EvaluatePathWithConfig(targetPath, action, PathConfig{
		WorkspaceDir:  workspaceDir,
		HomeDir:       homeDir,
		ProtectedDirs: protectedDirs,
	})
}
