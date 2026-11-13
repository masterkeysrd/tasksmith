package sandboxcfg

import (
	"fmt"
	"slices"
	"strings"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/guard"
	"github.com/masterkeysrd/tasksmith/internal/daemon/sandbox"
)

// Effective is the fully resolved sandbox posture for one workspace: the
// merged user settings plus the canonical kernel path lists a session sandbox
// must apply.
type Effective struct {
	// Settings is the merged user-facing configuration (what clients display).
	Settings api.SandboxSettings

	// Mode is the configured default backend ("native"/"local"), before the
	// host availability probe. Use ResolveMode for the enforceable backend.
	Mode api.SandboxMode
	// FailIfUnavailable requests hard failure instead of degrading to local
	// isolation when kernel sandboxing is unavailable.
	FailIfUnavailable bool
	// AllowUnsandboxed gates per-command unsandboxed execution. Forced false
	// in restricted workspaces and locked false by a global "false".
	AllowUnsandboxed bool
	// AutoAllowBash auto-approves safe sandboxed bash plans (consumed by the
	// permission engine).
	AutoAllowBash bool
	// ExcludedCommands holds command patterns exempted from the kernel
	// sandbox (consumed by the bash tool policy).
	ExcludedCommands []string

	// DeniedReadPaths is the credential read-masking floor merged with user
	// denyRead entries, minus any narrower user allowRead re-opens.
	DeniedReadPaths []string
	// AllowReadPaths re-opens subtrees inside a DeniedReadPaths mask.
	AllowReadPaths []string
	// ExtraWritePaths are writable directories outside the workspace root.
	ExtraWritePaths []string
	// DeniedWritePaths are protected paths that stay read-only even inside a
	// writable root; the built-in floor can never be removed by user config.
	DeniedWritePaths []string

	// Network carries the merged subprocess network posture.
	Network api.SandboxNetworkSettings

	// WorkDir is the workspace root the path lists were resolved against.
	WorkDir string
}

// Clone returns a deep copy of the effective posture.
func (e Effective) Clone() Effective {
	e.Settings = e.Settings.Clone()
	e.DeniedReadPaths = slices.Clone(e.DeniedReadPaths)
	e.AllowReadPaths = slices.Clone(e.AllowReadPaths)
	e.ExtraWritePaths = slices.Clone(e.ExtraWritePaths)
	e.DeniedWritePaths = slices.Clone(e.DeniedWritePaths)
	e.ExcludedCommands = slices.Clone(e.ExcludedCommands)
	e.Network = e.Network.Clone()
	return e
}

// FilesystemPolicy renders the effective posture as the kernel path policy a
// [sandbox.Sandbox] enforces.
func (e Effective) FilesystemPolicy() sandbox.FilesystemPolicy {
	return sandbox.FilesystemPolicy{
		DeniedReadPaths:  slices.Clone(e.DeniedReadPaths),
		AllowReadPaths:   slices.Clone(e.AllowReadPaths),
		ExtraWritePaths:  slices.Clone(e.ExtraWritePaths),
		DeniedWritePaths: slices.Clone(e.DeniedWritePaths),
	}
}

// Overlay layers the user-configured posture on top of a session's built-in
// floor policy (credential masks, protected writes, linked-worktree extras).
// All four lists are unioned so nothing a floor protects can be dropped by a
// configuration mistake; a user allowRead nested inside a floor mask survives
// as an allow entry and is re-opened by the kernel's breadth-ordered rule
// merge, while TaskSmith's own secret stores were already stripped from the
// user's allow list by the merge engine.
func (e Effective) Overlay(base sandbox.FilesystemPolicy) sandbox.FilesystemPolicy {
	p := base.Clone()
	p.DeniedReadPaths = deduplicate(append(p.DeniedReadPaths, e.DeniedReadPaths...))
	p.AllowReadPaths = deduplicate(append(p.AllowReadPaths, e.AllowReadPaths...))
	p.ExtraWritePaths = deduplicate(append(p.ExtraWritePaths, e.ExtraWritePaths...))
	p.DeniedWritePaths = deduplicate(append(p.DeniedWritePaths, e.DeniedWritePaths...))
	return p
}

// ResolvedPaths renders the effective posture for client inspection.
func (e Effective) ResolvedPaths() api.SandboxResolvedPaths {
	return api.SandboxResolvedPaths{
		WorkDir:    e.WorkDir,
		AllowWrite: slices.Clone(e.ExtraWritePaths),
		DenyWrite:  slices.Clone(e.DeniedWritePaths),
		DenyRead:   slices.Clone(e.DeniedReadPaths),
		AllowRead:  slices.Clone(e.AllowReadPaths),
	}
}

// ResolveMode maps a session's requested backend onto the backend this host
// can enforce, honoring the effective failIfUnavailable lock: with the lock
// active, requesting (or defaulting to) native on a host without kernel
// sandboxing returns [sandbox.ErrNativeUnavailable] instead of degrading.
func (e Effective) ResolveMode(requested api.SandboxMode) (api.SandboxMode, error) {
	req := api.SandboxMode(strings.ToLower(strings.TrimSpace(string(requested))))
	effective := e.Mode
	if req != "" {
		effective = req
	}
	if effective == "" {
		if e.FailIfUnavailable {
			// The strict posture means "I require kernel isolation": an
			// unconfigured backend defaults to native, and the availability
			// probe below turns native-unavailable into a hard error instead
			// of the usual silent local degradation.
			effective = api.SandboxNative
		} else {
			effective = sandbox.DefaultMode()
		}
	}
	if effective == api.SandboxNative && sandbox.NativeAvailable() != nil {
		if e.FailIfUnavailable {
			return "", fmt.Errorf("%w: failIfUnavailable is enabled and kernel sandboxing is unavailable on this host", sandbox.ErrNativeUnavailable)
		}
		return api.SandboxLocal, nil
	}
	if effective == api.SandboxLocal {
		return api.SandboxLocal, nil
	}
	return api.SandboxNative, nil
}

// boolOverride resolves an optional project knob over an optional global knob
// with a final default.
func boolOverride(project, global *bool, fallback bool) bool {
	if project != nil {
		return *project
	}
	if global != nil {
		return *global
	}
	return fallback
}

// monotonicOr resolves a security knob whose "true" side is the strict side
// (failIfUnavailable): any scope that explicitly enables it locks it on, so an
// omitted value never relaxes an explicit true in the other scope.
func monotonicOr(project, global *bool, fallback bool) bool {
	if (global != nil && *global) || (project != nil && *project) {
		return true
	}
	if global != nil {
		return *global
	}
	if project != nil {
		return *project
	}
	return fallback
}

// monotonicAnd resolves a security knob whose "false" side is the strict side
// (allowUnsandboxed): any scope that explicitly disables it locks it off, so a
// project can narrow but never widen a global "false".
func monotonicAnd(project, global *bool, fallback bool) bool {
	if (global != nil && !*global) || (project != nil && !*project) {
		return false
	}
	if global != nil {
		return *global
	}
	if project != nil {
		return *project
	}
	return fallback
}

// Effective merges the global and project sandbox.json scopes over the
// built-in security floor following the workspace precedence rules.
func (m *manager) Effective() (Effective, error) {
	return m.EffectiveFor("")
}

// EffectiveFor merges the scopes exactly like Effective but resolves
// project-relative paths and the built-in security floor against the given
// work directory instead of the manager's workspace root. Subagents running
// in an isolated worktree pass their own root so rules like denyRead:
// ["./secrets"] mask the worktree copy, not only the main checkout. An empty
// workDir falls back to the manager's workspace root.
func (m *manager) EffectiveFor(workDir string) (Effective, error) {
	m.mu.RLock()
	restricted := m.restricted
	root := m.workDir
	m.mu.RUnlock()
	if wd := strings.TrimSpace(workDir); wd != "" {
		root = wd
	}

	global, err := m.Get(api.ScopeGlobal)
	if err != nil {
		return Effective{}, err
	}
	project, err := m.Get(api.ScopeProject)
	if err != nil {
		return Effective{}, err
	}
	return m.effective(global, project, restricted, root)
}

func (m *manager) effective(global, project api.SandboxSettings, restricted bool, workDir string) (Effective, error) {
	mode := global.Mode
	if project.Mode != "" {
		mode = project.Mode
	}

	// failIfUnavailable is a monotonic security lock: enabling it in either
	// scope (a global "true" especially) can never be relaxed by the other.
	failIfUnavailable := monotonicOr(project.FailIfUnavailable, global.FailIfUnavailable, false)

	// allowUnsandboxed defaults to true in trusted workspaces, is forced false
	// whenever the workspace is restricted, and a global false locks it.
	allowUnsandboxed := monotonicAnd(project.AllowUnsandboxed, global.AllowUnsandboxed, true)
	if restricted {
		allowUnsandboxed = false
	}

	autoAllowBash := boolOverride(project.AutoAllowBash, global.AutoAllowBash, false)

	excluded := unionDedup(global.ExcludedCommands, project.ExcludedCommands)
	if restricted {
		// A freshly cloned repository can never exempt commands from the
		// sandbox; excludedCommands only apply once the workspace is trusted.
		excluded = nil
	}

	allowWrite, err := m.expandUnion(api.ScopeGlobal, api.ScopeProject, global.Filesystem.AllowWrite, project.Filesystem.AllowWrite, workDir)
	if err != nil {
		return Effective{}, err
	}
	userDenyWrite, err := m.expandUnion(api.ScopeGlobal, api.ScopeProject, global.Filesystem.DenyWrite, project.Filesystem.DenyWrite, workDir)
	if err != nil {
		return Effective{}, err
	}
	userDenyRead, err := m.expandUnion(api.ScopeGlobal, api.ScopeProject, global.Filesystem.DenyRead, project.Filesystem.DenyRead, workDir)
	if err != nil {
		return Effective{}, err
	}
	userAllowRead, err := m.expandUnion(api.ScopeGlobal, api.ScopeProject, global.Filesystem.AllowRead, project.Filesystem.AllowRead, workDir)
	if err != nil {
		return Effective{}, err
	}

	// Built-in floors are always present and non-overridable.
	credentialDenies := guard.HostCredentialPaths(workDir)
	protectedWrites := guard.ProtectedWorkspaceWritePaths(workDir)

	// Writes: built-in protected paths > user denyWrite > allowWrite.
	deniedWrites := deduplicate(append(slices.Clone(protectedWrites), userDenyWrite...))
	allowWrite = removeCoveredWrites(allowWrite, deniedWrites)

	// Reads: the credential floor is always masked; user denies add to it; a
	// narrower user allowRead re-opens a broader mask; TaskSmith's own secret
	// stores can never be re-opened.
	deniedReads := deduplicate(append(slices.Clone(credentialDenies), userDenyRead...))
	allowRead := removeCoveredAllows(removeSecretReopens(userAllowRead), deniedReads)

	network := global.Network
	if project.Network.Mode != "" {
		network.Mode = project.Network.Mode
	}
	network.AllowedDomains = unionDedup(global.Network.AllowedDomains, project.Network.AllowedDomains)
	network.DeniedDomains = unionDedup(global.Network.DeniedDomains, project.Network.DeniedDomains)

	settings := api.SandboxSettings{
		Schema:            1,
		Mode:              mode,
		FailIfUnavailable: &failIfUnavailable,
		AllowUnsandboxed:  &allowUnsandboxed,
		AutoAllowBash:     &autoAllowBash,
		ExcludedCommands:  excluded,
		Filesystem: api.SandboxFilesystemSettings{
			AllowWrite: allowWrite,
			DenyWrite:  deniedWrites,
			DenyRead:   deniedReads,
			AllowRead:  allowRead,
		},
		Network: network,
	}

	return Effective{
		Settings:          settings,
		Mode:              mode,
		FailIfUnavailable: failIfUnavailable,
		AllowUnsandboxed:  allowUnsandboxed,
		AutoAllowBash:     autoAllowBash,
		ExcludedCommands:  excluded,
		DeniedReadPaths:   deniedReads,
		AllowReadPaths:    allowRead,
		ExtraWritePaths:   allowWrite,
		DeniedWritePaths:  deniedWrites,
		Network:           network,
		WorkDir:           workDir,
	}, nil
}

// expandUnion resolves each global entry against global scope rules and each
// project entry against project scope rules (anchored on workDir), then unions
// the canonical paths. Global entries are absolute or ~/... so the anchor is
// irrelevant to them.
func (m *manager) expandUnion(globalScope, projectScope api.PermissionScope, globalPaths, projectPaths []string, workDir string) ([]string, error) {
	var out []string
	for _, p := range globalPaths {
		resolved, err := ResolveUserPath(globalScope, p, workDir)
		if err != nil {
			return nil, fmt.Errorf("global %w", err)
		}
		out = append(out, resolved)
	}
	for _, p := range projectPaths {
		resolved, err := ResolveUserPath(projectScope, p, workDir)
		if err != nil {
			return nil, fmt.Errorf("project %w", err)
		}
		out = append(out, resolved)
	}
	return deduplicate(out), nil
}

// removeCoveredWrites drops any allowWrite entry covered by a denyWrite entry
// at or above it: a deny always wins over an equal or broader allow.
func removeCoveredWrites(allow, deny []string) []string {
	if len(allow) == 0 || len(deny) == 0 {
		return allow
	}
	return slices.DeleteFunc(allow, func(a string) bool {
		for _, d := range deny {
			if fsutils.IsInsideDir(a, d) {
				return true
			}
		}
		return false
	})
}

// removeCoveredAllows drops allowRead entries that a deny at the same path or
// strictly narrower than it defeats: the narrower entry always wins and an
// exact tie keeps the deny. A strictly narrower allow nested inside a deny
// survives, because the kernel's breadth-ordered rule merge re-opens just
// that subtree. Denied entries are never dropped by an allow.
func removeCoveredAllows(allow, deny []string) []string {
	if len(allow) == 0 || len(deny) == 0 {
		return allow
	}
	return slices.DeleteFunc(allow, func(a string) bool {
		for _, d := range deny {
			if fsutils.IsInsideDir(d, a) {
				return true
			}
		}
		return false
	})
}

// removeSecretReopens drops allowRead entries that would re-open TaskSmith's
// own secret stores ($XDG_CONFIG_HOME/tasksmith/.env, $XDG_DATA_HOME/
// tasksmith/credentials, $XDG_DATA_HOME/tasksmith/mcp): neither an ancestor
// that blankets a store nor a descendant file inside one (e.g.
// credentials/oauth.json) is ever honored. Unlike host credential directories
// (~/.ssh stays readable for public files like known_hosts through a narrower
// allow), TaskSmith's stores hold the isolation and approval posture itself
// and can never be partially re-opened. Comparisons canonicalize both sides so
// symlinked XDG roots still anchor on the real store.
func removeSecretReopens(allow []string) []string {
	if len(allow) == 0 {
		return allow
	}
	secrets := make([]string, 0, len(guard.TasksmithSecretPaths()))
	for _, secret := range guard.TasksmithSecretPaths() {
		secrets = append(secrets, fsutils.ResolveDeepestExisting(secret))
	}
	return slices.DeleteFunc(allow, func(a string) bool {
		aResolved := fsutils.ResolveDeepestExisting(a)
		for _, secret := range secrets {
			if fsutils.IsInsideDir(aResolved, secret) || fsutils.IsInsideDir(secret, aResolved) {
				return true
			}
		}
		return false
	})
}
