package workspace

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/masterkeysrd/tasksmith/internal/core/env"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/tasksmith/internal/version"
	"github.com/masterkeysrd/warp"
)

//go:embed all:builtin
var builtinFS embed.FS

//go:embed all:preset/*.yaml
var presetFS embed.FS

// Namespace constants identifying the 4-tier resource resolution hierarchy.
const (
	NamespaceSystem    = warp.NamespaceSystem
	NamespaceUser      = warp.NamespaceUser
	NamespaceWorkspace = warp.NamespaceWorkspace
	NamespaceLocal     = warp.NamespaceLocal
)

// Builtin agent identifiers shipped in the system namespace.
const (
	AgentNameMain           = "main"
	AgentNameTitleGenerator = "title-generator"
)

// ModelAliasInherit is the TaskSmith-reserved model alias "@inherit". It never
// resolves through a provider manifest: the session layer resolves it to the
// delegating parent session's active model when a subagent is invoked.
const ModelAliasInherit = warp.ModelAliasInherit

// TierAliases lists the standardized completion-model tier aliases TaskSmith
// recommends providers declare (fast, balanced, smart, flagship). Built-in
// provider presets assign each tier alias to a model; workspaces driven by
// custom providers may leave a tier unresolvable, which the catalog tolerates
// for built-in agents.
var TierAliases = []string{"@fast", "@balanced", "@smart", "@flagship"}

// Option configures how a Workspace is opened.
type Option func(*openOptions)

type openOptions struct {
	builtinFS  []fs.FS
	restricted bool
}

// WithRestrictedMode opens the workspace in restricted mode:
// local project and workspace .env files are ignored, and workspace/project-scoped
// custom tools and MCP servers are stripped from the registry so only system/user
// tools remain available.
func WithRestrictedMode(restricted bool) Option {
	return func(o *openOptions) {
		o.restricted = restricted
	}
}

// WithBuiltinFS registers additional embedded or virtual filesystems (such as
// builtin tool manifests from internal/daemon/tools) into the "system" namespace.
func WithBuiltinFS(fsys ...fs.FS) Option {
	return func(o *openOptions) {
		for _, f := range fsys {
			if f != nil {
				o.builtinFS = append(o.builtinFS, f)
			}
		}
	}
}

// Presets returns all embedded model provider presets sorted by metadata name.
func Presets() []*warp.ModelProvider {
	return loadWarpPresets()
}

func loadWarpPresets() []*warp.ModelProvider {
	var providers []*warp.ModelProvider
	_ = fs.WalkDir(presetFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}
		data, err := presetFS.ReadFile(path)
		if err != nil {
			return nil
		}
		res, err := warp.Parse(path, string(data))
		if err != nil {
			return nil
		}
		if mp, ok := res.Resource.(*warp.ModelProvider); ok {
			mp.Namespace = NamespaceSystem
			providers = append(providers, mp)
		}
		return nil
	})
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].Metadata.Name < providers[j].Metadata.Name
	})
	return providers
}

func findWarpPreset(name string) *warp.ModelProvider {
	if name == "" {
		return nil
	}
	for _, p := range loadWarpPresets() {
		if p.Metadata.Name == name {
			return p
		}
	}
	return nil
}

// Workspace manages an active TaskSmith workspace, encapsulating its 4-tier
// resource registry and isolated environment variables.
type Workspace struct {
	mu            sync.RWMutex
	cwd           string
	rootDir       string
	opts          openOptions
	registry      *warp.Registry
	env           map[string]string
	subprocessEnv map[string]string
	catalog       *Catalog
	mcp           MCPService
}

// Open discovers and loads the workspace for dir, applying the 4-tier resource
// hierarchy (system < user < workspace < local) and isolated .env layering.
func Open(ctx context.Context, dir string, opts ...Option) (*Workspace, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	absDir, err := fsutils.CleanPath(dir)
	if err != nil {
		if errors.Is(err, fsutils.ErrEmptyPath) {
			return nil, errors.New("workspace: directory path must not be empty")
		}
		return nil, fmt.Errorf("workspace: resolve directory %q: %w", dir, err)
	}

	var o openOptions
	for _, fn := range opts {
		if fn != nil {
			fn(&o)
		}
	}

	w := &Workspace{
		cwd:           absDir,
		rootDir:       absDir,
		opts:          o,
		env:           make(map[string]string),
		subprocessEnv: make(map[string]string),
	}

	if err := w.Reload(ctx); err != nil {
		return nil, err
	}

	if IsConfigured(absDir) {
		if err := RunMigrations(ctx, absDir, version.Get()); err != nil {
			slog.Warn("workspace: migrations encountered an error", "dir", absDir, "error", err)
		}
	}

	return w, nil
}

// PrepareReload loads a fresh snapshot of the workspace without mutating the
// current instance until the returned commit function is invoked.
func (w *Workspace) PrepareReload(ctx context.Context) (*Workspace, func(), error) {
	w.mu.RLock()
	next := &Workspace{cwd: w.cwd, rootDir: w.rootDir, opts: w.opts, mcp: w.mcp}
	w.mu.RUnlock()
	if err := next.Reload(ctx); err != nil {
		return nil, nil, err
	}
	commit := func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.registry = next.registry
		w.rootDir = next.rootDir
		w.env = next.env
		w.subprocessEnv = next.subprocessEnv
		w.catalog = next.catalog
		w.mcp = next.mcp
	}
	return next, commit, nil
}

// Reload re-reads all 4 resource tiers and .env layers from disk into the Workspace.
func (w *Workspace) Reload(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	var sysProviders []warp.ResourceProvider
	if builtinDir, err := EnsureBuiltinDir(); err == nil {
		sysProviders = append(sysProviders, warp.NewFSResourceProvider(os.DirFS(builtinDir), builtinDir))
	} else {
		slog.Warn("workspace: unable to materialize built-in resources; using embedded resources", "error", err)
		subBuiltin, subErr := fs.Sub(builtinFS, "builtin")
		if subErr != nil {
			return fmt.Errorf("workspace: load embedded resources: %w", subErr)
		}
		sysProviders = append(sysProviders, warp.NewFSResourceProvider(subBuiltin, ""))
	}
	for _, fsys := range w.opts.builtinFS {
		sysProviders = append(sysProviders, warp.NewFSResourceProvider(fsys, ""))
	}

	var userProviders []warp.ResourceProvider
	if userAgentsDir, err := xdg.SubConfigDir("agents"); err == nil {
		if info, statErr := os.Stat(userAgentsDir); statErr == nil && info.IsDir() {
			userProviders = append(userProviders, warp.NewFSResourceProvider(os.DirFS(userAgentsDir), userAgentsDir))
		}
	}

	reg, err := warp.LoadWithOptions(ctx, w.cwd, warp.LoadOptions{
		SystemProviders:             sysProviders,
		UserProviders:               userProviders,
		AutoDiscoverStartSubproject: true,
	})
	if err != nil {
		return fmt.Errorf("workspace: load %q: %w", w.cwd, err)
	}

	rootDir := w.cwd
	if wsSpec := reg.WorkspaceSpec(); wsSpec != nil && wsSpec.RootPath != "" {
		rootDir = filepath.Clean(wsSpec.RootPath)
	}

	if w.opts.restricted {
		// Strip workspace and project-scoped executable resources (tools, mcps)
		for _, res := range reg.Resources() {
			if res.GetKind() == warp.KindTool || res.GetKind() == warp.KindMCP {
				ns := strings.ToLower(res.GetNamespace())
				if ns == warp.NamespaceWorkspace || ns == "workspace" || (ns != warp.NamespaceSystem && ns != warp.NamespaceUser && ns != "system" && ns != "user") {
					reg.Delete(res.QualifiedName())
				}
			}
		}
	}

	loadedEnv, subEnv, err := loadIsolatedEnv(w.cwd, rootDir, w.opts.restricted)
	if err != nil {
		return err
	}

	w.registry = reg
	w.rootDir = rootDir
	w.env = loadedEnv
	w.subprocessEnv = subEnv
	w.catalog = NewCatalog(reg, w.cwd, rootDir)
	// The catalog is freshly built, so it carries no resource probe; re-install it
	// against the runtime this workspace already has attached. The caller holds the
	// workspace write lock, so w.mcp is read directly rather than through MCP().
	w.setMCPResourceProbe(w.catalog, w.mcp)
	return nil
}

func loadIsolatedEnv(cwd, rootDir string, restricted bool) (daemonEnv, subprocessEnv map[string]string, err error) {
	hostEnv := make(map[string]string)
	for _, entry := range os.Environ() {
		if k, v, ok := strings.Cut(entry, "="); ok && k != "" {
			hostEnv[k] = v
		}
	}

	daemonLayers := []map[string]string{hostEnv}
	subLayers := []map[string]string{hostEnv}

	if userEnvPath, err := xdg.SubConfigDir(".env"); err == nil {
		m, loadErr := env.LoadFile(userEnvPath)
		if loadErr != nil {
			return nil, nil, fmt.Errorf("workspace: load user .env: %w", loadErr)
		}
		daemonLayers = append(daemonLayers, m)
	}

	if restricted {
		return env.Merge(daemonLayers...), env.Merge(subLayers...), nil
	}

	var climbPaths []string
	seenPaths := make(map[string]bool)
	addEnvPath := func(dir string) {
		p := filepath.Clean(filepath.Join(dir, ".env"))
		if !seenPaths[p] {
			seenPaths[p] = true
			climbPaths = append(climbPaths, p)
		}
	}

	curr := cwd
	for {
		if curr != rootDir && curr != cwd {
			if info, err := os.Stat(filepath.Join(curr, ".env")); err == nil && !info.IsDir() {
				addEnvPath(curr)
			}
		}
		if curr == rootDir {
			parent := filepath.Dir(curr)
			if parent != curr {
				if info, err := os.Stat(filepath.Join(parent, ".bare")); err == nil && info.IsDir() {
					if envInfo, envErr := os.Stat(filepath.Join(parent, ".env")); envErr == nil && !envInfo.IsDir() {
						addEnvPath(parent)
					}
				}
			}
			break
		}
		if _, err := os.Stat(filepath.Join(curr, warp.WorkspaceFileName)); err == nil {
			break
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}

	var projectLayers []map[string]string
	for _, climbPath := range slices.Backward(climbPaths) {
		m, err := env.LoadFile(climbPath)
		if err != nil {
			return nil, nil, fmt.Errorf("workspace: load ancestor .env %q: %w", climbPath, err)
		}
		projectLayers = append(projectLayers, m)
	}

	rootEnvPath := filepath.Clean(filepath.Join(rootDir, ".env"))
	if !seenPaths[rootEnvPath] {
		seenPaths[rootEnvPath] = true
		m, err := env.LoadFile(rootEnvPath)
		if err != nil {
			return nil, nil, fmt.Errorf("workspace: load root .env: %w", err)
		}
		projectLayers = append(projectLayers, m)
	}

	cwdEnvPath := filepath.Clean(filepath.Join(cwd, ".env"))
	if !seenPaths[cwdEnvPath] {
		seenPaths[cwdEnvPath] = true
		m, err := env.LoadFile(cwdEnvPath)
		if err != nil {
			return nil, nil, fmt.Errorf("workspace: load cwd .env: %w", err)
		}
		projectLayers = append(projectLayers, m)
	}

	daemonLayers = append(daemonLayers, projectLayers...)
	subLayers = append(subLayers, projectLayers...)
	return env.Merge(daemonLayers...), env.Merge(subLayers...), nil
}

// IsRestricted reports whether the workspace was opened in restricted mode.
func (w *Workspace) IsRestricted() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.opts.restricted
}
func (w *Workspace) RootDir() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.rootDir
}

// CWD returns the absolute working directory path this Workspace was opened with.
func (w *Workspace) CWD() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cwd
}

// Name returns the workspace name declared in WORKSPACE.md, or the base directory
// name of RootDir when unconfigured/synthetic.
func (w *Workspace) Name() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.registry != nil {
		if ws := w.registry.WorkspaceSpec(); ws != nil && ws.Def != nil && ws.Def.Metadata.Name != "" {
			return ws.Def.Metadata.Name
		}
	}
	return filepath.Base(w.rootDir)
}

// IsSynthetic reports whether the workspace was inferred without an on-disk WORKSPACE.md.
func (w *Workspace) IsSynthetic() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.registry == nil {
		return true
	}
	ws := w.registry.WorkspaceSpec()
	return ws == nil || ws.Synthetic
}

// Env returns a defensive copy of the workspace's isolated environment variables.
// It is the daemon-side view used for provider and MCP credential resolution;
// agent-spawned subprocesses must use SubprocessEnv instead.
func (w *Workspace) Env() map[string]string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.env == nil {
		return map[string]string{}
	}
	return maps.Clone(w.env)
}

// SubprocessEnv returns the environment passed to agent-spawned child processes
// (bash commands and custom tools). It preserves the host shell environment and,
// in trusted workspaces, project .env layers, while excluding TaskSmith's
// private user .env (~/.config/tasksmith/.env), configured ModelProvider
// auth env-expression credentials, and the TASKSMITH_* internal namespace.
func (w *Workspace) SubprocessEnv() map[string]string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if len(w.subprocessEnv) == 0 {
		return map[string]string{}
	}
	deny := w.providerAuthEnvLocked()
	out := make(map[string]string, len(w.subprocessEnv))
	for k, v := range w.subprocessEnv {
		trimmed := strings.TrimSpace(k)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(strings.ToUpper(trimmed), "TASKSMITH_") {
			continue
		}
		if _, blocked := deny[trimmed]; blocked {
			continue
		}
		out[k] = v
	}
	return out
}

// providerAuthEnvLocked collects environment variable names referenced via
// ${env:...} expressions on configured ModelProvider spec.auth fields. The
// caller must hold at least the read lock.
func (w *Workspace) providerAuthEnvLocked() map[string]struct{} {
	if w.catalog == nil {
		return nil
	}
	var out map[string]struct{}
	for _, p := range w.catalog.Providers() {
		if p == nil || p.Spec.Auth == nil {
			continue
		}
		for _, name := range ProviderAuthEnvNames(p.Spec.Auth) {
			if out == nil {
				out = make(map[string]struct{})
			}
			out[name] = struct{}{}
		}
	}
	return out
}

// ProviderAuthEnvNames extracts all environment variable names referenced by
// ${env:<VAR>} expressions across auth's fields (including nested OAuth fields).
func ProviderAuthEnvNames(auth *warp.ProviderAuth) []string {
	if auth == nil {
		return nil
	}
	fields := []string{auth.Token, auth.Header, auth.Username, auth.Password}
	if auth.OAuth != nil {
		fields = append(fields,
			auth.OAuth.IDPIssuer,
			auth.OAuth.IDPClientID,
			auth.OAuth.IDPClientSecret,
			auth.OAuth.AudienceIssuer,
			auth.OAuth.ResourceURI,
			auth.OAuth.ClientID,
			auth.OAuth.ClientSecret,
		)
	}
	var names []string
	seen := make(map[string]struct{})
	for _, field := range fields {
		exprs, err := warp.ExtractExpressions(field)
		if err != nil {
			continue
		}
		for _, expr := range exprs {
			if expr.Scheme != "env" {
				continue
			}
			name := strings.TrimSpace(expr.Target)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	return names
}

// ProviderAuthPrimaryEnv returns the primary environment variable name
// referenced by auth (checking Token first, then any other ${env:...} expression).
func ProviderAuthPrimaryEnv(auth *warp.ProviderAuth) string {
	if auth == nil {
		return ""
	}
	if expr, ok := warp.ParseExpression(strings.TrimSpace(auth.Token)); ok && expr.Scheme == "env" {
		return strings.TrimSpace(expr.Target)
	}
	if names := ProviderAuthEnvNames(auth); len(names) > 0 {
		return names[0]
	}
	return ""
}

// PortablePath converts targetPath into a portable representation relative to the
// workspace root, or prefixed with "~" if within the user's home directory.
func (w *Workspace) PortablePath(targetPath string) string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var home string
	if w.env != nil {
		home = w.env["HOME"]
	}
	return fsutils.PortablePath(targetPath, w.rootDir, home)
}

// Catalog returns the query and resolution catalog for this workspace.
func (w *Workspace) Catalog() *Catalog {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.catalog
}
