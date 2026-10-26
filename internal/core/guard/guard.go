package guard

import (
	"path/filepath"
	"slices"
	"strings"

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

// ActionType classifies the fundamental category of a tool or shell operation.
type ActionType string

const (
	ActionRead    ActionType = "read"
	ActionUnknown ActionType = "unknown"
	ActionNet     ActionType = "net"
	ActionWrite   ActionType = "write"
	ActionExec    ActionType = "exec"
	ActionDelete  ActionType = "delete"
)

// String returns the canonical lowercase name of the action type.
func (a ActionType) String() string {
	switch a {
	case ActionRead, ActionUnknown, ActionNet, ActionWrite, ActionExec, ActionDelete:
		return string(a)
	default:
		return string(ActionUnknown)
	}
}

// Severity returns the numeric severity rank of the action type, ordered from
// lowest (ActionRead = 0) to highest (ActionDelete = 5).
func (a ActionType) Severity() int {
	switch a {
	case ActionRead:
		return 0
	case ActionUnknown:
		return 1
	case ActionNet:
		return 2
	case ActionWrite:
		return 3
	case ActionExec:
		return 4
	case ActionDelete:
		return 5
	default:
		return 1
	}
}

// SafetyLevel classifies the risk profile of an operation.
type SafetyLevel string

const (
	SafetySafe    SafetyLevel = "safe"
	SafetyOutside SafetyLevel = "outside"
	SafetyUnknown SafetyLevel = "unknown"
	SafetyUnsafe  SafetyLevel = "unsafe"
)

// String returns the canonical lowercase name of the safety level.
func (s SafetyLevel) String() string {
	switch s {
	case SafetySafe, SafetyOutside, SafetyUnknown, SafetyUnsafe:
		return string(s)
	default:
		return string(SafetyUnknown)
	}
}

// Severity returns the numeric risk rank of the safety level, ordered from
// safest (SafetySafe = 0) to most unsafe (SafetyUnsafe = 3).
func (s SafetyLevel) Severity() int {
	switch s {
	case SafetySafe:
		return 0
	case SafetyOutside:
		return 1
	case SafetyUnknown:
		return 2
	case SafetyUnsafe:
		return 3
	default:
		return 2
	}
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
func EvaluatePathWithConfig(targetPath string, action ActionType, cfg PathConfig) (string, SafetyLevel) {
	resolved := fsutils.ExpandAndCleanPath(targetPath, cfg.WorkspaceDir, cfg.HomeDir)
	if IsSensitivePath(resolved, cfg.HomeDir) {
		return resolved, SafetyUnsafe
	}
	if cfg.WorkspaceDir != "" && fsutils.IsInsideDir(resolved, cfg.WorkspaceDir) {
		if IsProtectedPath(resolved, cfg.WorkspaceDir, cfg.ProtectedDirs...) && action != ActionRead {
			return resolved, SafetyUnsafe
		}
		return resolved, SafetySafe
	}
	for _, wd := range cfg.WriteDirs {
		if fsutils.IsInsideDir(resolved, wd) {
			if action == ActionRead || action == ActionWrite || action == ActionDelete {
				return resolved, SafetySafe
			}
			return resolved, SafetyUnsafe
		}
	}
	for _, rd := range cfg.ReadDirs {
		if fsutils.IsInsideDir(resolved, rd) {
			if action == ActionRead {
				return resolved, SafetySafe
			}
			return resolved, SafetyUnsafe
		}
	}
	if action == ActionRead {
		return resolved, SafetyOutside
	}
	return resolved, SafetyUnsafe
}

// EvaluatePath resolves targetPath against workspaceDir (and expands ~ with homeDir)
// and classifies its SafetyLevel for the given ActionType.
func EvaluatePath(targetPath, workspaceDir, homeDir string, action ActionType, protectedDirs ...string) (string, SafetyLevel) {
	return EvaluatePathWithConfig(targetPath, action, PathConfig{
		WorkspaceDir:  workspaceDir,
		HomeDir:       homeDir,
		ProtectedDirs: protectedDirs,
	})
}
