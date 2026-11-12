package guard

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
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

// IsSensitivePath reports whether targetPath points to a sensitive system path
// (e.g. /etc, /proc, /dev) or user credential path (e.g. ~/.ssh, ~/.aws).
func IsSensitivePath(targetPath, homeDir string) bool {
	cleaned := fsutils.ExpandAndCleanPath(targetPath, "", homeDir)
	for _, sp := range DefaultSensitivePaths {
		spClean := filepath.Clean(sp)
		if cleaned == spClean || strings.HasPrefix(cleaned, spClean+string(filepath.Separator)) {
			return true
		}
	}
	if homeDir != "" {
		homeClean := filepath.Clean(homeDir)
		for _, dir := range DefaultSensitiveHomeDirs {
			full := filepath.Join(homeClean, dir)
			if cleaned == full || strings.HasPrefix(cleaned, full+string(filepath.Separator)) {
				return true
			}
		}
	}
	for part := range strings.SplitSeq(filepath.ToSlash(cleaned), "/") {
		if slices.Contains(DefaultSensitiveHomeDirs, part) {
			return true
		}
	}
	return false
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
