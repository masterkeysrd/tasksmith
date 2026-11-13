package sandboxcfg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
)

// ResolveUserPath canonicalizes one user-authored path from sandbox.json for
// the given scope: "~" and "~/..." expand to the user's home directory, and
// project scope additionally resolves relative paths against the workspace
// root. Global scope rejects relative paths other than "~/..." because a
// user-wide configuration file has no meaningful anchor directory to resolve
// them against.
func ResolveUserPath(scope api.PermissionScope, raw, workDir string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", fmt.Errorf("%w: path must not be empty", ErrInvalidValue)
	}
	if strings.HasPrefix(p, "~") && p != "~" && !strings.HasPrefix(p, "~/") {
		return "", fmt.Errorf("%w: path %q must be \"~\" or start with \"~/\"", ErrInvalidValue, raw)
	}

	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if strings.HasPrefix(p, "~") {
			return "", fmt.Errorf("%w: user home directory is unavailable to expand %q", ErrInvalidValue, raw)
		}
		home = ""
	}
	expanded := fsutils.ExpandAndCleanPath(p, "", home)
	if !filepath.IsAbs(expanded) {
		if scope == api.ScopeGlobal {
			return "", fmt.Errorf("%w: global scope rejects relative path %q (use an absolute path or \"~/...\")", ErrInvalidValue, raw)
		}
		if strings.TrimSpace(workDir) == "" {
			return "", fmt.Errorf("%w: relative path %q requires a workspace root", ErrInvalidValue, raw)
		}
		expanded = filepath.Join(workDir, expanded)
	}
	// Anchor on the kernel's canonical vnode path so overlap detection against
	// the built-in floors (which are canonicalized the same way) is exact even
	// behind symlinked parents (e.g. /var -> /private/var on macOS).
	return fsutils.ResolveDeepestExisting(expanded), nil
}

// Validate checks a scope's settings before persistence: modes must be known
// values, filesystem paths must be resolvable for the scope, and network
// domain patterns must be exact hosts or "*." wildcard suffixes.
func Validate(scope api.PermissionScope, workDir string, settings api.SandboxSettings) error {
	if mode := settings.Mode; mode != "" && !mode.IsValid() {
		return fmt.Errorf("%w: unsupported sandbox mode %q", ErrInvalidValue, mode)
	}
	if netMode := settings.Network.Mode; netMode != "" && !netMode.IsValid() {
		return fmt.Errorf("%w: unsupported network mode %q", ErrInvalidValue, netMode)
	}

	fsSets := map[string][]string{
		"filesystem.allowWrite": settings.Filesystem.AllowWrite,
		"filesystem.denyWrite":  settings.Filesystem.DenyWrite,
		"filesystem.denyRead":   settings.Filesystem.DenyRead,
		"filesystem.allowRead":  settings.Filesystem.AllowRead,
	}
	for field, paths := range fsSets {
		for _, p := range paths {
			if _, err := ResolveUserPath(scope, p, workDir); err != nil {
				return fmt.Errorf("%s: %w", field, err)
			}
		}
	}

	domains := map[string][]string{
		"network.allowedDomains": settings.Network.AllowedDomains,
		"network.deniedDomains":  settings.Network.DeniedDomains,
	}
	for field, list := range domains {
		for _, d := range list {
			if err := validateDomainPattern(d); err != nil {
				return fmt.Errorf("%s: %w", field, err)
			}
		}
	}

	for _, pattern := range settings.ExcludedCommands {
		if strings.TrimSpace(pattern) == "" {
			return fmt.Errorf("%w: excludedCommands entries must not be empty", ErrInvalidValue)
		}
		if strings.ContainsAny(pattern, "\n\r") {
			return fmt.Errorf("%w: excludedCommands pattern %q must be a single line", ErrInvalidValue, pattern)
		}
	}
	return nil
}

// validateDomainPattern accepts an exact hostname or a "*." wildcard suffix
// (e.g. "api.example.com", "*.example.com").
func validateDomainPattern(raw string) error {
	d := strings.TrimSpace(raw)
	if d == "" {
		return fmt.Errorf("%w: domain must not be empty", ErrInvalidValue)
	}
	if strings.ContainsAny(d, " \t/@\\") {
		return fmt.Errorf("%w: domain %q must be a hostname or \"*.domain\" pattern", ErrInvalidValue, raw)
	}
	if strings.HasPrefix(d, "*.") {
		d = d[2:]
		if d == "" || d == "." {
			return fmt.Errorf("%w: wildcard domain %q needs a suffix", ErrInvalidValue, raw)
		}
	}
	for label := range strings.SplitSeq(d, ".") {
		if label == "" {
			return fmt.Errorf("%w: domain %q contains an empty label", ErrInvalidValue, raw)
		}
		for _, r := range label {
			ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
			if !ok {
				return fmt.Errorf("%w: domain %q contains invalid character %q", ErrInvalidValue, raw, r)
			}
		}
	}
	return nil
}
