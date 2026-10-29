package workspace

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/masterkeysrd/tasksmith/api"
	"github.com/masterkeysrd/tasksmith/internal/core/env"
	"github.com/masterkeysrd/tasksmith/internal/core/fsutils"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/warp"
)

//go:embed all:builtin
var builtinFS embed.FS

//go:embed all:preset/*.yaml
var presetFS embed.FS

// Option configures how a Workspace is opened.
type Option func(*openOptions)

type openOptions struct {
	builtinFS     []fs.FS
	agentOverride string
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

// WithAgentOverride sets an active default agent override on the opened Workspace.
func WithAgentOverride(name string) Option {
	return func(o *openOptions) {
		o.agentOverride = strings.TrimSpace(name)
	}
}

// Presets returns all embedded model provider presets as domain structs,
// sorted deterministically by provider name.
func Presets() []Provider {
	raw := loadWarpPresets()
	out := make([]Provider, 0, len(raw))
	for _, p := range raw {
		out = append(out, convertProvider(p, nil))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// ProviderPresets is an alias for Presets.
func ProviderPresets() []Provider {
	return Presets()
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

// workspace manages an active TaskSmith workspace, encapsulating its 4-tier
// resource registry and isolated environment variables.
type workspace struct {
	mu            sync.RWMutex
	cwd           string
	rootDir       string
	opts          openOptions
	registry      *warp.Registry
	env           map[string]string
	agentOverride string
}

var _ Workspace = (*workspace)(nil)

// Open discovers and loads the workspace for dir, applying the 4-tier resource
// hierarchy (system < user < workspace < local) and isolated .env layering.
func Open(ctx context.Context, dir string, opts ...Option) (Workspace, error) {
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

	w := &workspace{
		cwd:           absDir,
		rootDir:       absDir,
		opts:          o,
		env:           make(map[string]string),
		agentOverride: o.agentOverride,
	}

	if err := w.Reload(ctx); err != nil {
		return nil, err
	}

	return w, nil
}

// Reload re-reads all 4 resource tiers and .env layers from disk into the Workspace.
func (w *workspace) Reload(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// 1. Build system-tier providers (embedded builtin agents + WithBuiltinFS)
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

	// 2. Load workspace, local projects, and system providers via warp.Load
	reg, err := warp.Load(w.cwd, sysProviders...)
	if err != nil {
		return fmt.Errorf("workspace: load %q: %w", w.cwd, err)
	}

	rootDir := w.cwd
	if wsSpec := reg.WorkspaceSpec(); wsSpec != nil && wsSpec.RootPath != "" {
		rootDir = filepath.Clean(wsSpec.RootPath)
		if wsSpec.Def != nil && wsSpec.Def.Directory == "" {
			wsSpec.Def.Directory = rootDir
		}
	}

	// If w.cwd is a subdirectory inside rootDir that has its own .agents/ directory
	// (or AGENTS.md) and was not explicitly declared in WORKSPACE.md's projects list,
	// promote rootDir/.agents/ to NamespaceWorkspace and register w.cwd as a local project
	// so 4-tier resolution (system < user < workspace < local) works seamlessly.
	if w.cwd != rootDir {
		if err := ensureSubprojectLoaded(ctx, reg, rootDir, w.cwd); err != nil {
			return err
		}
	}

	// 3. Inject user-tier resources from XDG config ($XDG_CONFIG_HOME/<app>/agents)
	if userAgentsDir, err := xdg.SubConfigDir("agents"); err == nil {
		if info, statErr := os.Stat(userAgentsDir); statErr == nil && info.IsDir() {
			userProv := warp.NewNamespacedFSProvider(
				os.DirFS(userAgentsDir),
				userAgentsDir,
				warp.NamespaceUser,
				warp.NamespacePriority(warp.NamespaceUser),
			)
			userResources, loadErr := userProv.GetResources(ctx)
			if loadErr != nil {
				return fmt.Errorf("workspace: load user agents dir %q: %w", userAgentsDir, loadErr)
			}
			for _, r := range userResources {
				reg.Set(r.QualifiedName(), r)
			}
		}
	}

	// 4. Load isolated .env layers (never calling os.Setenv)
	loadedEnv, err := loadIsolatedEnv(w.cwd, rootDir)
	if err != nil {
		return err
	}

	w.registry = reg
	w.rootDir = rootDir
	w.env = loadedEnv
	return nil
}

// ensureSubprojectLoaded handles the case where w.cwd is a nested directory inside
// rootDir that contains its own .agents/ directory, even when WORKSPACE.md defaulted
// to projects: ["."].
func ensureSubprojectLoaded(ctx context.Context, reg *warp.Registry, rootDir, cwd string) error {
	proj, ok := reg.ProjectFromPath(cwd)
	if ok && filepath.Clean(proj.AbsPath()) == filepath.Clean(cwd) {
		return nil
	}

	cwdAgentsDir := filepath.Join(cwd, warp.DefaultAgentsDir)
	info, err := os.Stat(cwdAgentsDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	// Ensure rootDir/.agents/ resources are available in NamespaceWorkspace so
	// the subproject's ScopedRegistry sees them at the workspace tier.
	rootAgentsDir := filepath.Join(rootDir, warp.DefaultAgentsDir)
	if rootInfo, statErr := os.Stat(rootAgentsDir); statErr == nil && rootInfo.IsDir() {
		wsProv := warp.NewNamespacedFSProvider(
			os.DirFS(rootAgentsDir),
			rootAgentsDir,
			warp.NamespaceWorkspace,
			warp.NamespacePriority(warp.NamespaceWorkspace),
		)
		if wsResources, loadErr := wsProv.GetResources(ctx); loadErr == nil {
			for _, r := range wsResources {
				reg.Set(r.QualifiedName(), r)
			}
		}
	}

	rel, err := filepath.Rel(rootDir, cwd)
	if err != nil || rel == "." {
		return nil
	}
	slug := strings.ToLower(strings.ReplaceAll(filepath.ToSlash(rel), "/", "-"))

	reg.AddProject(&warp.Project{
		Name:     slug,
		Path:     rel,
		RootPath: rootDir,
	})

	localProv := warp.NewNamespacedFSProvider(
		os.DirFS(cwdAgentsDir),
		cwdAgentsDir,
		slug,
		warp.NamespacePriority(warp.NamespaceLocal),
	)
	localResources, err := localProv.GetResources(ctx)
	if err != nil {
		return fmt.Errorf("workspace: load local project agents dir %q: %w", cwdAgentsDir, err)
	}
	for _, r := range localResources {
		reg.Set(r.QualifiedName(), r)
	}
	return nil
}

func loadIsolatedEnv(cwd, rootDir string) (map[string]string, error) {
	var layers []map[string]string

	// Layer 0: Host process environment variables (os.Environ())
	hostEnv := make(map[string]string)
	for _, entry := range os.Environ() {
		if k, v, ok := strings.Cut(entry, "="); ok && k != "" {
			hostEnv[k] = v
		}
	}
	layers = append(layers, hostEnv)

	// Layer 1: Global user .env ($XDG_CONFIG_HOME/<app>/.env)
	if userEnvPath, err := xdg.SubConfigDir(".env"); err == nil {
		m, loadErr := env.LoadFile(userEnvPath)
		if loadErr != nil {
			return nil, fmt.Errorf("workspace: load user .env: %w", loadErr)
		}
		layers = append(layers, m)
	}

	// Layer 2: Climb from cwd up to workspace sentinel or filesystem root to collect
	// any ancestor .env files (e.g. a bare-repo hub .env above a worktree), ordered
	// from outermost ancestor to innermost directory.
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
		// Stop climbing once we reach rootDir or a directory with WORKSPACE.md / .bare
		if curr == rootDir {
			// Check one level above rootDir in case rootDir is a worktree inside a bare hub
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

	// Reverse climbPaths so outer ancestors are merged before inner directories
	for _, climbPath := range slices.Backward(climbPaths) {
		m, err := env.LoadFile(climbPath)
		if err != nil {
			return nil, fmt.Errorf("workspace: load ancestor .env %q: %w", climbPath, err)
		}
		layers = append(layers, m)
	}

	// Layer 3: Workspace root .env (<rootDir>/.env)
	rootEnvPath := filepath.Clean(filepath.Join(rootDir, ".env"))
	if !seenPaths[rootEnvPath] {
		seenPaths[rootEnvPath] = true
		m, err := env.LoadFile(rootEnvPath)
		if err != nil {
			return nil, fmt.Errorf("workspace: load root .env: %w", err)
		}
		layers = append(layers, m)
	}

	// Layer 4: Working directory .env (<cwd>/.env) if distinct from rootDir
	cwdEnvPath := filepath.Clean(filepath.Join(cwd, ".env"))
	if !seenPaths[cwdEnvPath] {
		seenPaths[cwdEnvPath] = true
		m, err := env.LoadFile(cwdEnvPath)
		if err != nil {
			return nil, fmt.Errorf("workspace: load cwd .env: %w", err)
		}
		layers = append(layers, m)
	}

	return env.Merge(layers...), nil
}

// RootDir returns the absolute path to the discovered workspace root directory.
func (w *workspace) RootDir() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.rootDir
}

// CWD returns the absolute working directory path this Workspace was opened with.
func (w *workspace) CWD() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cwd
}

// Name returns the workspace name declared in WORKSPACE.md, or the base directory
// name of RootDir when unconfigured/synthetic.
func (w *workspace) Name() string {
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
func (w *workspace) IsSynthetic() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.registry == nil {
		return true
	}
	ws := w.registry.WorkspaceSpec()
	return ws == nil || ws.Synthetic
}

// Warnings returns a copy of non-fatal warnings encountered during workspace loading.
func (w *workspace) Warnings() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.registry == nil {
		return nil
	}
	return cloneSlice(w.registry.Warnings())
}

// Env returns a defensive copy of the workspace's isolated environment variables.
func (w *workspace) Env() map[string]string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.env == nil {
		return map[string]string{}
	}
	return maps.Clone(w.env)
}

// SetEnv updates key=value in the workspace's in-memory environment map and
// atomically persists the change to <RootDir>/.env.
func (w *workspace) SetEnv(key, value string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	envPath := filepath.Join(w.rootDir, ".env")
	if err := env.SetInFile(envPath, key, value); err != nil {
		return err
	}
	if w.env == nil {
		w.env = make(map[string]string)
	}
	w.env[strings.TrimSpace(key)] = value
	return nil
}

// DefaultAgentName returns the active default agent name, honoring any runtime
// override, then WORKSPACE.md's spec.defaultAgent, and finally falling back to "main".
func (w *workspace) DefaultAgentName() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.agentOverride != "" {
		return w.agentOverride
	}
	if w.registry != nil {
		if ws := w.registry.WorkspaceSpec(); ws != nil && ws.Def != nil && ws.Def.Spec.DefaultAgent != "" {
			return ws.Def.Spec.DefaultAgent
		}
	}
	return AgentNameMain
}

// SetAgentOverride sets or clears the active default agent override.
func (w *workspace) SetAgentOverride(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.agentOverride = strings.TrimSpace(name)
}

// Presets returns all embedded model provider presets.
func (w *workspace) Presets() []Provider {
	return Presets()
}

// ProviderPresets returns all embedded model provider presets.
func (w *workspace) ProviderPresets() []Provider {
	return Presets()
}

func (w *workspace) resolverLocked() warp.Resolver {
	if w.registry == nil {
		return nil
	}
	if proj, ok := w.registry.ProjectFromPath(w.cwd); ok {
		return w.registry.Project(proj.Name)
	}
	return w.registry
}

func (w *workspace) currentWarpProjectLocked() *warp.Project {
	if w.registry == nil {
		return nil
	}
	if proj, ok := w.registry.ProjectFromPath(w.cwd); ok {
		return proj
	}
	return nil
}

func (w *workspace) resolveByKindLocked(kind warp.Kind, name string) (warp.Resource, bool) {
	resolver := w.resolverLocked()
	if resolver == nil || strings.TrimSpace(name) == "" {
		return nil, false
	}

	// If caller passed a qualified name ("ns/Kind/name"), try direct resolution first.
	if strings.Contains(name, "/") {
		if res, ok := resolver.ResolveResource(name); ok && res.GetKind() == kind {
			return res, true
		}
	}

	// Filter strictly by kind with Effective: true so 4-tier shadowing applies
	// without cross-kind name collisions.
	resources := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{kind},
		Effective: true,
	})
	for _, r := range resources {
		if r.GetName() == name || r.QualifiedName() == name {
			return r, true
		}
	}
	return nil, false
}

// Providers returns all effective model providers in the workspace scope,
// sorted by name.
func (w *workspace) Providers() []Provider {
	w.mu.RLock()
	defer w.mu.RUnlock()
	resolver := w.resolverLocked()
	if resolver == nil {
		return nil
	}
	raw := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindModelProvider},
		Effective: true,
	})
	out := make([]Provider, 0, len(raw))
	for _, r := range raw {
		if mp, ok := r.(*warp.ModelProvider); ok {
			out = append(out, convertProvider(mp, w.registry))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// GetProvider resolves an effective model provider by name.
func (w *workspace) GetProvider(name string) (*Provider, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if res, ok := w.resolveByKindLocked(warp.KindModelProvider, name); ok {
		if mp, ok := res.(*warp.ModelProvider); ok {
			p := convertProvider(mp, w.registry)
			return &p, true
		}
	}
	return nil, false
}

func (w *workspace) resolveWarpAgentLocked(name string) (*warp.Agent, *warp.ResolvedAgent, bool) {
	res, ok := w.resolveByKindLocked(warp.KindAgent, name)
	if !ok {
		return nil, nil, false
	}
	baseAgent, ok := res.(*warp.Agent)
	if !ok {
		return nil, nil, false
	}

	resolver := w.resolverLocked()
	var resolved *warp.ResolvedAgent
	switch r := resolver.(type) {
	case *warp.ScopedRegistry:
		resolved, _ = r.ResolveAgent(baseAgent.QualifiedName())
	case *warp.Registry:
		resolved, _ = r.ResolveAgent(baseAgent.QualifiedName())
	}
	if resolved != nil && resolved.Agent != nil {
		return resolved.Agent, resolved, true
	}
	return baseAgent, nil, true
}

// Agents returns all effective agents in the workspace scope, sorted by name.
func (w *workspace) Agents() []Agent {
	w.mu.RLock()
	defer w.mu.RUnlock()
	resolver := w.resolverLocked()
	if resolver == nil {
		return nil
	}
	raw := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindAgent},
		Effective: true,
	})
	out := make([]Agent, 0, len(raw))
	for _, r := range raw {
		if ag, ok := r.(*warp.Agent); ok {
			merged, resolved, ok := w.resolveWarpAgentLocked(ag.GetName())
			var da Agent
			if ok {
				da = convertAgent(merged, resolved, w.registry)
			} else {
				da = convertAgent(ag, nil, w.registry)
			}
			da.Context = w.agentContextLocked(da.Name)
			out = append(out, da)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// GetAgent resolves an effective agent by name, applying any inheritance chain.
func (w *workspace) GetAgent(name string) (*Agent, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	ag, resolved, ok := w.resolveWarpAgentLocked(name)
	if !ok {
		return nil, false
	}
	domainAgent := convertAgent(ag, resolved, w.registry)
	domainAgent.Context = w.agentContextLocked(name)
	return &domainAgent, true
}

// ResolveAgent resolves an effective agent by name, applying inheritance,
// policies, and returning the fully hydrated WARP ResolvedAgent.
func (w *workspace) ResolveAgent(name string) (*warp.ResolvedAgent, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	target := strings.TrimSpace(name)
	if target == "" {
		target = w.defaultAgentNameLocked()
	}

	_, resolved, ok := w.resolveWarpAgentLocked(target)
	if !ok || resolved == nil {
		return nil, fmt.Errorf("workspace: agent %q not found", target)
	}
	return resolved, nil
}

// Decisions returns all effective decision resources in the workspace scope,
// sorted by name.
func (w *workspace) Decisions() []Decision {
	w.mu.RLock()
	defer w.mu.RUnlock()
	resolver := w.resolverLocked()
	if resolver == nil {
		return nil
	}
	raw := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindDecision},
		Effective: true,
	})
	out := make([]Decision, 0, len(raw))
	for _, r := range raw {
		if dec, ok := r.(*warp.Decision); ok {
			out = append(out, convertDecision(dec, w.registry))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// GetDecision resolves an effective decision resource by name.
func (w *workspace) GetDecision(name string) (*Decision, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	res, ok := w.resolveByKindLocked(warp.KindDecision, name)
	if !ok {
		return nil, false
	}
	dec, ok := res.(*warp.Decision)
	if !ok {
		return nil, false
	}
	d := convertDecision(dec, w.registry)
	return &d, true
}

// Skills returns all effective skill resources in the workspace scope, sorted by name.
func (w *workspace) Skills() []Skill {
	w.mu.RLock()
	defer w.mu.RUnlock()
	resolver := w.resolverLocked()
	if resolver == nil {
		return nil
	}
	raw := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindSkill},
		Effective: true,
	})
	out := make([]Skill, 0, len(raw))
	for _, r := range raw {
		if sk, ok := r.(*warp.Skill); ok {
			out = append(out, convertSkill(sk, w.registry))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// GetSkill resolves an effective skill resource by name.
func (w *workspace) GetSkill(name string) (*Skill, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	res, ok := w.resolveByKindLocked(warp.KindSkill, name)
	if !ok {
		return nil, false
	}
	sk, ok := res.(*warp.Skill)
	if !ok {
		return nil, false
	}
	s := convertSkill(sk, w.registry)
	return &s, true
}

// Commands returns all effective command resources in the workspace scope, sorted by name.
func (w *workspace) Commands() []Command {
	w.mu.RLock()
	defer w.mu.RUnlock()
	resolver := w.resolverLocked()
	if resolver == nil {
		return nil
	}
	raw := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindCommand},
		Effective: true,
	})
	out := make([]Command, 0, len(raw))
	for _, r := range raw {
		if cmd, ok := r.(*warp.Command); ok {
			out = append(out, convertCommand(cmd, w.registry))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// GetCommand resolves an effective command resource by name.
func (w *workspace) GetCommand(name string) (*Command, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	res, ok := w.resolveByKindLocked(warp.KindCommand, name)
	if !ok {
		return nil, false
	}
	cmd, ok := res.(*warp.Command)
	if !ok {
		return nil, false
	}
	c := convertCommand(cmd, w.registry)
	return &c, true
}

// Tools returns all effective tool resources in the workspace scope, sorted by name.
func (w *workspace) Tools() []Tool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	resolver := w.resolverLocked()
	if resolver == nil {
		return nil
	}
	raw := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindTool},
		Effective: true,
	})
	out := make([]Tool, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, r := range raw {
		if t, ok := r.(*warp.Tool); ok {
			dt := convertTool(t, w.registry)
			out = append(out, dt)
			seen[dt.Name] = true
		}
	}

	// Also include inline tools from effective toolkits if not shadowed by standalone tools
	toolkits := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindToolkit},
		Effective: true,
	})
	for _, r := range toolkits {
		tk, ok := r.(*warp.Toolkit)
		if !ok {
			continue
		}
		dtk := convertToolkit(tk, w.registry)
		for _, item := range dtk.Tools {
			if item.Ref == "" && item.Name != "" && !seen[item.Name] {
				seen[item.Name] = true
				out = append(out, item.Tool)
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// GetTool resolves an effective tool resource by name.
func (w *workspace) GetTool(name string) (*Tool, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if res, ok := w.resolveByKindLocked(warp.KindTool, name); ok {
		if t, ok := res.(*warp.Tool); ok {
			dt := convertTool(t, w.registry)
			return &dt, true
		}
	}
	// Check inline tools in effective toolkits
	resolver := w.resolverLocked()
	if resolver != nil {
		toolkits := resolver.ListResources(warp.QueryOptions{
			Kinds:     []warp.Kind{warp.KindToolkit},
			Effective: true,
		})
		for _, r := range toolkits {
			tk, ok := r.(*warp.Toolkit)
			if !ok {
				continue
			}
			dtk := convertToolkit(tk, w.registry)
			for _, item := range dtk.Tools {
				if item.Ref == "" && item.Name == name {
					t := item.Tool
					return &t, true
				}
			}
		}
	}
	return nil, false
}

// Toolkits returns all effective toolkit resources in the workspace scope, sorted by name.
func (w *workspace) Toolkits() []Toolkit {
	w.mu.RLock()
	defer w.mu.RUnlock()
	resolver := w.resolverLocked()
	if resolver == nil {
		return nil
	}
	raw := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindToolkit},
		Effective: true,
	})
	out := make([]Toolkit, 0, len(raw))
	for _, r := range raw {
		if tk, ok := r.(*warp.Toolkit); ok {
			out = append(out, convertToolkit(tk, w.registry))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// GetToolkit resolves an effective toolkit resource by name.
func (w *workspace) GetToolkit(name string) (*Toolkit, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	res, ok := w.resolveByKindLocked(warp.KindToolkit, name)
	if !ok {
		return nil, false
	}
	tk, ok := res.(*warp.Toolkit)
	if !ok {
		return nil, false
	}
	dtk := convertToolkit(tk, w.registry)
	return &dtk, true
}

// MCPs returns all effective MCP server configurations in the workspace scope,
// sorted by name.
func (w *workspace) MCPs() []MCP {
	w.mu.RLock()
	defer w.mu.RUnlock()
	resolver := w.resolverLocked()
	if resolver == nil {
		return nil
	}
	raw := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindMCP},
		Effective: true,
	})
	out := make([]MCP, 0, len(raw))
	for _, r := range raw {
		if m, ok := r.(*warp.MCP); ok {
			out = append(out, convertMCP(m, w.registry))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// GetMCP resolves an effective MCP server configuration by name.
func (w *workspace) GetMCP(name string) (*MCP, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	res, ok := w.resolveByKindLocked(warp.KindMCP, name)
	if !ok {
		return nil, false
	}
	m, ok := res.(*warp.MCP)
	if !ok {
		return nil, false
	}
	dm := convertMCP(m, w.registry)
	return &dm, true
}

// Projects returns all projects registered in the workspace, sorted by name.
func (w *workspace) Projects() []Project {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.registry == nil {
		return nil
	}
	raw := w.registry.ListProjects()
	out := make([]Project, 0, len(raw))
	for _, p := range raw {
		out = append(out, convertProject(p, w.registry))
	}
	return out
}

// Policies returns the workspace-level security and tool policies declared in
// WORKSPACE.md, or nil when none are configured.
func (w *workspace) Policies() *Policies {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.registry == nil {
		return nil
	}
	ws := w.registry.WorkspaceSpec()
	if ws == nil || ws.Def == nil {
		return nil
	}
	return convertPolicies(ws.Def.Spec.Policies)
}

// ResolveModels returns the prioritized list of qualified model identifiers for
// the given role following the hierarchy:
//  1. Explicit model overrides.
//  2. Workspace role default (spec.defaultModels.<role> or spec.defaultModel for completion).
//  3. Provider role default (spec.defaultModels.<role> or spec.defaultModel for completion).
//  4. Capability match across registered providers.
func (w *workspace) ResolveModels(role ModelRole, explicit ...string) []string {
	w.mu.RLock()
	resolver := w.resolverLocked()
	var resolved []warp.ResolvedModel
	if resolver != nil {
		resolved = resolver.ResolveModels(warp.ModelRole(role), explicit...)
	}
	w.mu.RUnlock()

	if len(resolved) == 0 {
		return nil
	}
	out := make([]string, len(resolved))
	for i, rm := range resolved {
		out[i] = rm.QualifiedID
	}
	return out
}

// ResolveModel resolves the highest-priority model reference for the given role
// and explicit overrides, returning its provider name, canonical qualified ID,
// and catalog metadata when available.
func (w *workspace) ResolveModel(role ModelRole, explicit ...string) (*ResolvedModel, bool) {
	w.mu.RLock()
	resolver := w.resolverLocked()
	var rm *warp.ResolvedModel
	var ok bool
	if resolver != nil {
		rm, ok = resolver.ResolveModel(warp.ModelRole(role), explicit...)
	}
	w.mu.RUnlock()

	if !ok || rm == nil {
		return nil, false
	}
	var pm *ProviderModel
	if rm.Model != nil {
		converted := convertProviderModel(*rm.Model)
		pm = &converted
	}
	var provName string
	if rm.Provider != nil {
		provName = rm.Provider.Metadata.Name
	}
	return &ResolvedModel{
		ProviderName: provName,
		ModelID:      rm.ModelID,
		QualifiedID:  rm.QualifiedID,
		Model:        pm,
	}, true
}

// ─── Template Rendering Helpers ─────────────────────────────────────────────

// RenderAgent renders the named agent's instructions template with workspace,
// project, resolved agent metadata, args, and globals.
func (w *workspace) RenderAgent(name string, args map[string]string, globals map[string]any) (string, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	_, resolved, ok := w.resolveWarpAgentLocked(name)
	if !ok || resolved == nil || resolved.Agent == nil {
		return "", fmt.Errorf("workspace: agent %q not found", name)
	}

	mergedGlobals := mergeStringArgsIntoGlobals(args, globals)
	return resolved.Agent.Render(&warp.AgentRenderOptions{
		Workspace: w.registry.WorkspaceSpec(),
		Project:   w.currentWarpProjectLocked(),
		Resolved:  resolved,
		Globals:   mergedGlobals,
	})
}

// RenderSkill renders the named skill's instructions template with workspace,
// project, args, and globals.
func (w *workspace) RenderSkill(name string, args map[string]string, globals map[string]any) (string, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	res, ok := w.resolveByKindLocked(warp.KindSkill, name)
	if !ok {
		return "", fmt.Errorf("workspace: skill %q not found", name)
	}
	sk, ok := res.(*warp.Skill)
	if !ok {
		return "", fmt.Errorf("workspace: resource %q is not a skill", name)
	}

	mergedGlobals := mergeStringArgsIntoGlobals(args, globals)
	positionalArgs := buildPositionalArgsFromArguments(sk.Spec.Arguments, args)
	return sk.Render(&warp.SkillRenderOptions{
		Workspace: w.registry.WorkspaceSpec(),
		Project:   w.currentWarpProjectLocked(),
		Args:      positionalArgs,
		Globals:   mergedGlobals,
	})
}

// RenderCommand renders the named command's instructions template with workspace,
// project, positional/hint arguments, and globals.
func (w *workspace) RenderCommand(name string, args map[string]string, globals map[string]any) (string, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	res, ok := w.resolveByKindLocked(warp.KindCommand, name)
	if !ok {
		return "", fmt.Errorf("workspace: command %q not found", name)
	}
	cmd, ok := res.(*warp.Command)
	if !ok {
		return "", fmt.Errorf("workspace: resource %q is not a command", name)
	}

	mergedGlobals := mergeStringArgsIntoGlobals(args, globals)
	positionalArgs := buildPositionalArgsFromArguments(cmd.Spec.Arguments, args)

	return cmd.Render(&warp.CommandRenderOptions{
		Workspace: w.registry.WorkspaceSpec(),
		Project:   w.currentWarpProjectLocked(),
		Args:      positionalArgs,
		Globals:   mergedGlobals,
	})
}

// RenderDecision renders the named decision's instructions template with workspace,
// project, inputs, and globals.
func (w *workspace) RenderDecision(name string, inputs map[string]any, globals map[string]any) (string, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	res, ok := w.resolveByKindLocked(warp.KindDecision, name)
	if !ok {
		return "", fmt.Errorf("workspace: decision %q not found", name)
	}
	dec, ok := res.(*warp.Decision)
	if !ok {
		return "", fmt.Errorf("workspace: resource %q is not a decision", name)
	}

	mergedGlobals := make(map[string]any)
	maps.Copy(mergedGlobals, globals)
	maps.Copy(mergedGlobals, inputs)
	if _, hasInputs := mergedGlobals["Inputs"]; !hasInputs && inputs != nil {
		mergedGlobals["Inputs"] = inputs
	}

	positionalArgs := buildPositionalArgsFromAnyMap(dec.Spec.Arguments, inputs)

	return dec.Render(&warp.DecisionRenderOptions{
		Workspace: w.registry.WorkspaceSpec(),
		Project:   w.currentWarpProjectLocked(),
		Args:      positionalArgs,
		Globals:   mergedGlobals,
	})
}

// RenderWorkspace renders the workspace-level instructions from WORKSPACE.md.
func (w *workspace) RenderWorkspace(globals map[string]any) (string, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.registry == nil {
		return "", nil
	}
	ws := w.registry.WorkspaceSpec()
	if ws == nil {
		return "", nil
	}
	return ws.Render(&warp.WorkspaceRenderOptions{
		Globals: globals,
	})
}

// PortablePath converts targetPath into a portable representation relative to the
// workspace root, or prefixed with "~" if within the user's home directory.
func (w *workspace) PortablePath(targetPath string) string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var home string
	if w.env != nil {
		home = w.env["HOME"]
	}
	return fsutils.PortablePath(targetPath, w.rootDir, home)
}

func mergeStringArgsIntoGlobals(args map[string]string, globals map[string]any) map[string]any {
	merged := make(map[string]any)
	maps.Copy(merged, globals)
	for k, v := range args {
		merged[k] = v
	}
	if _, hasArgs := merged["Args"]; !hasArgs && args != nil {
		merged["Args"] = args
	}
	return merged
}

func buildPositionalArgsFromArguments(argsDef []warp.Argument, args map[string]string) []string {
	if len(args) == 0 {
		return nil
	}
	var out []string
	if len(argsDef) > 0 {
		out = make([]string, len(argsDef))
		for i, a := range argsDef {
			if val, ok := args[a.Name]; ok {
				out[i] = val
			} else if val, ok := args[fmt.Sprintf("%d", i+1)]; ok {
				out[i] = val
			} else if a.Default != "" {
				out[i] = a.Default
			}
		}
		return out
	}
	// Check 1-indexed numeric keys ("1", "2", ...)
	for i := 1; i <= len(args); i++ {
		val, ok := args[fmt.Sprintf("%d", i)]
		if !ok {
			break
		}
		out = append(out, val)
	}
	return out
}

func buildPositionalArgsFromAnyMap(argsDef []warp.Argument, inputs map[string]any) []string {
	if len(inputs) == 0 {
		return nil
	}
	var out []string
	if len(argsDef) > 0 {
		out = make([]string, len(argsDef))
		for i, a := range argsDef {
			if val, ok := inputs[a.Name]; ok && val != nil {
				out[i] = fmt.Sprint(val)
			} else if val, ok := inputs[fmt.Sprintf("%d", i+1)]; ok && val != nil {
				out[i] = fmt.Sprint(val)
			} else if a.Default != "" {
				out[i] = a.Default
			}
		}
		return out
	}
	for i := 1; i <= len(inputs); i++ {
		val, ok := inputs[fmt.Sprintf("%d", i)]
		if !ok || val == nil {
			break
		}
		out = append(out, fmt.Sprint(val))
	}
	return out
}

var (
	reProjectContext = regexp.MustCompile(`(?s)<project_context\b[^>]*>.*?</project_context>`)
	reSkillsSection  = regexp.MustCompile(`(?s)<skills_and_specialized_knowledge\b[^>]*>.*?</skills_and_specialized_knowledge>`)
	reSubagentsSec   = regexp.MustCompile(`(?s)<subagents_and_delegation\b[^>]*>.*?</subagents_and_delegation>`)
)

func estimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return (len(s) + 3) / 4
}

func (w *workspace) AgentContext(name string) api.ContextBreakdown {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.agentContextLocked(name)
}

func (w *workspace) agentContextLocked(name string) api.ContextBreakdown {
	if name == "" {
		name = w.defaultAgentNameLocked()
	}

	var wsPrompt string
	if w.registry != nil {
		if wsSpec := w.registry.WorkspaceSpec(); wsSpec != nil {
			wsPrompt, _ = wsSpec.Render(&warp.WorkspaceRenderOptions{})
		}
	}
	contextFiles := estimateTokens(wsPrompt)

	ag, resolved, ok := w.resolveWarpAgentLocked(name)
	if !ok {
		return api.ContextBreakdown{ContextFiles: contextFiles}
	}

	globals := map[string]any{
		"HasTool": func(toolName string) bool {
			return true
		},
	}
	agentPrompt, _ := ag.Render(&warp.AgentRenderOptions{
		Workspace: w.registry.WorkspaceSpec(),
		Project:   w.currentWarpProjectLocked(),
		Resolved:  resolved,
		Globals:   globals,
	})

	var skills, subagents int

	// Extract <project_context> sections
	projMatches := reProjectContext.FindAllString(agentPrompt, -1)
	for _, m := range projMatches {
		contextFiles += estimateTokens(m)
	}
	agentPrompt = reProjectContext.ReplaceAllString(agentPrompt, "")

	// Extract <skills_and_specialized_knowledge> sections
	skillMatches := reSkillsSection.FindAllString(agentPrompt, -1)
	for _, m := range skillMatches {
		skills += estimateTokens(m)
	}
	agentPrompt = reSkillsSection.ReplaceAllString(agentPrompt, "")

	// Extract <subagents_and_delegation> sections
	subagentMatches := reSubagentsSec.FindAllString(agentPrompt, -1)
	for _, m := range subagentMatches {
		subagents += estimateTokens(m)
	}
	agentPrompt = reSubagentsSec.ReplaceAllString(agentPrompt, "")

	systemPrompt := estimateTokens(strings.TrimSpace(agentPrompt))

	// Tool definition schemas
	var systemTools, customTools, mcpTools int
	toolsList := w.toolsLocked()
	for _, t := range toolsList {
		if resolved != nil && len(resolved.Tools) > 0 {
			var found bool
			for _, rt := range resolved.Tools {
				if rt.Metadata.Name == t.Name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		toolTokens := estimateTokens(t.Name) + estimateTokens(t.Description) + estimateTokens(t.Instructions)
		if len(t.InputSchema) > 0 {
			if data, err := json.Marshal(t.InputSchema); err == nil {
				toolTokens += estimateTokens(string(data))
			}
		}

		ns := strings.ToLower(t.Namespace)
		if ns == NamespaceSystem || ns == "system" || ns == "" {
			systemTools += toolTokens
		} else if ns == "mcp" || strings.HasPrefix(t.Name, "mcp__") {
			mcpTools += toolTokens
		} else {
			customTools += toolTokens
		}
	}

	return api.ContextBreakdown{
		SystemPrompt: systemPrompt,
		ContextFiles: contextFiles,
		SystemTools:  systemTools,
		CustomTools:  customTools,
		MCPTools:     mcpTools,
		Skills:       skills,
		Subagents:    subagents,
	}
}

func (w *workspace) defaultAgentNameLocked() string {
	if w.agentOverride != "" {
		return w.agentOverride
	}
	if w.registry != nil {
		if ws := w.registry.WorkspaceSpec(); ws != nil && ws.Def != nil && ws.Def.Spec.DefaultAgent != "" {
			return ws.Def.Spec.DefaultAgent
		}
	}
	return AgentNameMain
}

func (w *workspace) toolsLocked() []Tool {
	resolver := w.resolverLocked()
	if resolver == nil {
		return nil
	}
	raw := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindTool},
		Effective: true,
	})
	out := make([]Tool, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, r := range raw {
		if t, ok := r.(*warp.Tool); ok {
			dt := convertTool(t, w.registry)
			out = append(out, dt)
			seen[dt.Name] = true
		}
	}

	toolkits := resolver.ListResources(warp.QueryOptions{
		Kinds:     []warp.Kind{warp.KindToolkit},
		Effective: true,
	})
	for _, r := range toolkits {
		tk, ok := r.(*warp.Toolkit)
		if !ok {
			continue
		}
		dtk := convertToolkit(tk, w.registry)
		for _, item := range dtk.Tools {
			if item.Ref == "" && item.Name != "" && !seen[item.Name] {
				seen[item.Name] = true
				out = append(out, item.Tool)
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func (w *workspace) ModelLimits(rawID string) ModelLimits {
	if rm, ok := w.ResolveModel(RoleCompletion, strings.TrimSpace(rawID)); ok && rm.Model != nil {
		return rm.Model.Limits
	}
	return ModelLimits{
		Context: 128000,
		Output:  16384,
	}
}
