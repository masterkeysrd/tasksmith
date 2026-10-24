package workspace

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
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
	"github.com/masterkeysrd/warp"
)

//go:embed all:builtin/*.md
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
		if p.Metadata.Name == name || p.Spec.Type == name {
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
	agentOverride string
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
		agentOverride: o.agentOverride,
	}

	if err := w.Reload(ctx); err != nil {
		return nil, err
	}

	return w, nil
}

// Reload re-reads all 4 resource tiers and .env layers from disk into the Workspace.
func (w *Workspace) Reload(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// 1. Build system-tier providers (embedded builtin agents + WithBuiltinFS)
	var sysProviders []warp.ResourceProvider
	if subBuiltin, err := fs.Sub(builtinFS, "builtin"); err == nil {
		sysProviders = append(sysProviders, warp.NewFSResourceProvider(subBuiltin, ""))
	} else {
		sysProviders = append(sysProviders, warp.NewFSResourceProvider(builtinFS, ""))
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

// Warnings returns a copy of non-fatal warnings encountered during workspace loading.
func (w *Workspace) Warnings() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.registry == nil {
		return nil
	}
	return cloneSlice(w.registry.Warnings())
}

// Env returns a defensive copy of the workspace's isolated environment variables.
func (w *Workspace) Env() map[string]string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.env == nil {
		return map[string]string{}
	}
	return maps.Clone(w.env)
}

// SetEnv updates key=value in the workspace's in-memory environment map and
// atomically persists the change to <RootDir>/.env.
func (w *Workspace) SetEnv(key, value string) error {
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
func (w *Workspace) DefaultAgentName() string {
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
func (w *Workspace) SetAgentOverride(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.agentOverride = strings.TrimSpace(name)
}

// Presets returns all embedded model provider presets.
func (w *Workspace) Presets() []Provider {
	return Presets()
}

// ProviderPresets returns all embedded model provider presets.
func (w *Workspace) ProviderPresets() []Provider {
	return Presets()
}

func (w *Workspace) resolverLocked() warp.Resolver {
	if w.registry == nil {
		return nil
	}
	if proj, ok := w.registry.ProjectFromPath(w.cwd); ok {
		return w.registry.Project(proj.Name)
	}
	return w.registry
}

func (w *Workspace) currentWarpProjectLocked() *warp.Project {
	if w.registry == nil {
		return nil
	}
	if proj, ok := w.registry.ProjectFromPath(w.cwd); ok {
		return proj
	}
	return nil
}

func (w *Workspace) resolveByKindLocked(kind warp.Kind, name string) (warp.Resource, bool) {
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
func (w *Workspace) Providers() []Provider {
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

// GetProvider resolves an effective model provider by name or type.
func (w *Workspace) GetProvider(name string) (*Provider, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if res, ok := w.resolveByKindLocked(warp.KindModelProvider, name); ok {
		if mp, ok := res.(*warp.ModelProvider); ok {
			p := convertProvider(mp, w.registry)
			return &p, true
		}
	}
	// Also allow lookup by provider Spec.Type
	resolver := w.resolverLocked()
	if resolver != nil {
		for _, r := range resolver.ListResources(warp.QueryOptions{
			Kinds:     []warp.Kind{warp.KindModelProvider},
			Effective: true,
		}) {
			if mp, ok := r.(*warp.ModelProvider); ok && mp.Spec.Type == name {
				p := convertProvider(mp, w.registry)
				return &p, true
			}
		}
	}
	return nil, false
}

func (w *Workspace) resolveWarpAgentLocked(name string) (*warp.Agent, *warp.ResolvedAgent, bool) {
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
func (w *Workspace) Agents() []Agent {
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
			if ok {
				out = append(out, convertAgent(merged, resolved, w.registry))
			} else {
				out = append(out, convertAgent(ag, nil, w.registry))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// GetAgent resolves an effective agent by name, applying any inheritance chain.
func (w *Workspace) GetAgent(name string) (*Agent, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	ag, resolved, ok := w.resolveWarpAgentLocked(name)
	if !ok {
		return nil, false
	}
	domainAgent := convertAgent(ag, resolved, w.registry)
	return &domainAgent, true
}

// Decisions returns all effective decision resources in the workspace scope,
// sorted by name.
func (w *Workspace) Decisions() []Decision {
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
func (w *Workspace) GetDecision(name string) (*Decision, bool) {
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
func (w *Workspace) Skills() []Skill {
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
func (w *Workspace) GetSkill(name string) (*Skill, bool) {
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
func (w *Workspace) Commands() []Command {
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
func (w *Workspace) GetCommand(name string) (*Command, bool) {
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
func (w *Workspace) Tools() []Tool {
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
func (w *Workspace) GetTool(name string) (*Tool, bool) {
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
func (w *Workspace) Toolkits() []Toolkit {
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
func (w *Workspace) GetToolkit(name string) (*Toolkit, bool) {
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
func (w *Workspace) MCPs() []MCP {
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
func (w *Workspace) GetMCP(name string) (*MCP, bool) {
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
func (w *Workspace) Projects() []Project {
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
func (w *Workspace) Policies() *Policies {
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
//  3. Default Provider role default (spec.defaultModels.<role> or spec.defaultModel for completion).
//  4. Capability match across registered providers.
func (w *Workspace) ResolveModels(role ModelRole, explicit ...string) []string {
	w.mu.RLock()
	resolver := w.resolverLocked()
	var resolved []string
	if resolver != nil {
		resolved = resolver.ResolveModels(warp.ModelRole(role), explicit...)
	}
	w.mu.RUnlock()

	if len(resolved) > 0 {
		return resolved
	}

	// Fallback: inspect effective providers for role defaults or capability match
	providers := w.Providers()
	if len(providers) == 0 {
		return nil
	}

	for _, p := range providers {
		if m, ok := p.DefaultModels[role]; ok && m != "" {
			if strings.Contains(m, "/") {
				return []string{m}
			}
			return []string{p.Name + "/" + m}
		}
		if role == RoleCompletion && p.DefaultModel != "" {
			if strings.Contains(p.DefaultModel, "/") {
				return []string{p.DefaultModel}
			}
			return []string{p.Name + "/" + p.DefaultModel}
		}
	}

	for _, p := range providers {
		for _, m := range p.Models {
			switch role {
			case RoleCompletion:
				if m.Capabilities.SupportsCompletion() {
					return []string{p.Name + "/" + m.ID}
				}
			case RoleDecision:
				if m.Capabilities.SupportsDecision() {
					return []string{p.Name + "/" + m.ID}
				}
			case RoleEmbedding:
				if m.Capabilities.SupportsEmbedding() {
					return []string{p.Name + "/" + m.ID}
				}
			}
		}
	}

	return nil
}

// ─── Template Rendering Helpers ─────────────────────────────────────────────

// RenderAgent renders the named agent's instructions template with workspace,
// project, resolved agent metadata, args, and globals.
func (w *Workspace) RenderAgent(name string, args map[string]string, globals map[string]any) (string, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	ag, resolved, ok := w.resolveWarpAgentLocked(name)
	if !ok {
		return "", fmt.Errorf("workspace: agent %q not found", name)
	}

	mergedGlobals := mergeStringArgsIntoGlobals(args, globals)
	return ag.Render(&warp.AgentRenderOptions{
		Workspace: w.registry.WorkspaceSpec(),
		Project:   w.currentWarpProjectLocked(),
		Resolved:  resolved,
		Globals:   mergedGlobals,
	})
}

// RenderSkill renders the named skill's instructions template with workspace,
// project, args, and globals.
func (w *Workspace) RenderSkill(name string, args map[string]string, globals map[string]any) (string, error) {
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
	return sk.Render(&warp.SkillRenderOptions{
		Workspace: w.registry.WorkspaceSpec(),
		Project:   w.currentWarpProjectLocked(),
		Globals:   mergedGlobals,
	})
}

// RenderCommand renders the named command's instructions template with workspace,
// project, positional/hint arguments, and globals.
func (w *Workspace) RenderCommand(name string, args map[string]string, globals map[string]any) (string, error) {
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
	positionalArgs := buildPositionalArgsFromStringMap(cmd.Spec.Hints, args)

	return cmd.Render(&warp.CommandRenderOptions{
		Workspace: w.registry.WorkspaceSpec(),
		Project:   w.currentWarpProjectLocked(),
		Args:      positionalArgs,
		Globals:   mergedGlobals,
	})
}

// RenderDecision renders the named decision's instructions template with workspace,
// project, inputs, and globals.
func (w *Workspace) RenderDecision(name string, inputs map[string]any, globals map[string]any) (string, error) {
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

	positionalArgs := buildPositionalArgsFromAnyMap(dec.Spec.Hints, inputs)

	return dec.Render(&warp.DecisionRenderOptions{
		Workspace: w.registry.WorkspaceSpec(),
		Project:   w.currentWarpProjectLocked(),
		Args:      positionalArgs,
		Globals:   mergedGlobals,
	})
}

// RenderWorkspace renders the workspace-level instructions from WORKSPACE.md.
func (w *Workspace) RenderWorkspace(globals map[string]any) (string, error) {
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

func buildPositionalArgsFromStringMap(hints []string, args map[string]string) []string {
	if len(args) == 0 {
		return nil
	}
	var out []string
	if len(hints) > 0 {
		out = make([]string, len(hints))
		for i, h := range hints {
			if val, ok := args[h]; ok {
				out[i] = val
			} else if val, ok := args[fmt.Sprintf("%d", i+1)]; ok {
				out[i] = val
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

func buildPositionalArgsFromAnyMap(hints []string, inputs map[string]any) []string {
	if len(inputs) == 0 {
		return nil
	}
	var out []string
	if len(hints) > 0 {
		out = make([]string, len(hints))
		for i, h := range hints {
			if val, ok := inputs[h]; ok && val != nil {
				out[i] = fmt.Sprint(val)
			} else if val, ok := inputs[fmt.Sprintf("%d", i+1)]; ok && val != nil {
				out[i] = fmt.Sprint(val)
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
