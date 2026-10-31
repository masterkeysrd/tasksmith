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
	AgentNameMain               = "main"
	AgentNameAgentCreator       = "agent-creator"
	AgentNameProjectInitializer = "project-initializer"
	AgentNameProviderManager    = "provider-manager"
	AgentNameSkillCreator       = "skill-creator"
	AgentNameTitleGenerator     = "title-generator"
	AgentNameToolCreator        = "tool-creator"
)

// Option configures how a Workspace is opened.
type Option func(*openOptions)

type openOptions struct {
	builtinFS []fs.FS
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
	mu       sync.RWMutex
	cwd      string
	rootDir  string
	opts     openOptions
	registry *warp.Registry
	env      map[string]string
	catalog  *Catalog
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
		cwd:     absDir,
		rootDir: absDir,
		opts:    o,
		env:     make(map[string]string),
	}

	if err := w.Reload(ctx); err != nil {
		return nil, err
	}

	return w, nil
}

// PrepareReload loads a fresh snapshot of the workspace without mutating the
// current instance until the returned commit function is invoked.
func (w *Workspace) PrepareReload(ctx context.Context) (*Workspace, func(), error) {
	w.mu.RLock()
	next := &Workspace{cwd: w.cwd, rootDir: w.rootDir, opts: w.opts}
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
		w.catalog = next.catalog
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

	if w.cwd != rootDir {
		if err := ensureSubprojectLoaded(ctx, reg, rootDir, w.cwd); err != nil {
			return err
		}
	}

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

	loadedEnv, err := loadIsolatedEnv(w.cwd, rootDir)
	if err != nil {
		return err
	}

	w.registry = reg
	w.rootDir = rootDir
	w.env = loadedEnv
	w.catalog = NewCatalog(reg, w.cwd, rootDir)
	return nil
}

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

	hostEnv := make(map[string]string)
	for _, entry := range os.Environ() {
		if k, v, ok := strings.Cut(entry, "="); ok && k != "" {
			hostEnv[k] = v
		}
	}
	layers = append(layers, hostEnv)

	if userEnvPath, err := xdg.SubConfigDir(".env"); err == nil {
		m, loadErr := env.LoadFile(userEnvPath)
		if loadErr != nil {
			return nil, fmt.Errorf("workspace: load user .env: %w", loadErr)
		}
		layers = append(layers, m)
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

	for _, climbPath := range slices.Backward(climbPaths) {
		m, err := env.LoadFile(climbPath)
		if err != nil {
			return nil, fmt.Errorf("workspace: load ancestor .env %q: %w", climbPath, err)
		}
		layers = append(layers, m)
	}

	rootEnvPath := filepath.Clean(filepath.Join(rootDir, ".env"))
	if !seenPaths[rootEnvPath] {
		seenPaths[rootEnvPath] = true
		m, err := env.LoadFile(rootEnvPath)
		if err != nil {
			return nil, fmt.Errorf("workspace: load root .env: %w", err)
		}
		layers = append(layers, m)
	}

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

// Env returns a defensive copy of the workspace's isolated environment variables.
func (w *Workspace) Env() map[string]string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.env == nil {
		return map[string]string{}
	}
	return maps.Clone(w.env)
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
