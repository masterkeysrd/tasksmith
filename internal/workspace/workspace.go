package workspace

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/warp"
)

//go:embed all:preset/*.yaml
var presetFS embed.FS

type Workspace struct {
	cwd      string
	logger   log.Interface
	registry *warp.Registry
}

func New(cwd string) *Workspace {
	abs, err := filepath.Abs(cwd)
	if err == nil {
		cwd = abs
	}
	return &Workspace{
		cwd:    cwd,
		logger: log.ForComponent("workspace"),
	}
}

func (w *Workspace) CWD() string {
	return w.cwd
}

func (w *Workspace) Load(ctx context.Context) error {
	w.logger.Info("Loading workspace", log.String("cwd", w.cwd))
	provider := &systemProvider{}
	reg, err := warp.Load(w.cwd, provider)
	if err != nil {
		return fmt.Errorf("load workspace: %w", err)
	}
	w.registry = reg
	return nil
}

func (w *Workspace) Resources() []warp.Resource {
	resolver := w.resolver()
	if resolver == nil {
		return nil
	}

	resources := resolver.ListResources(warp.QueryOptions{})
	return resources
}

func (w *Workspace) Projects() []*warp.Project {
	if w.registry == nil {
		return nil
	}

	projects := w.registry.ListProjects()
	return projects
}

func (w *Workspace) Agents() []*warp.Agent {
	resolver := w.resolver()
	if resolver == nil {
		return nil
	}

	resources := resolver.ListResources(warp.QueryOptions{
		Kinds: []warp.Kind{warp.KindAgent},
	})
	agents := make([]*warp.Agent, 0, len(resources))
	for _, r := range resources {
		if agent, ok := r.(*warp.Agent); ok {
			agents = append(agents, agent)
		}
	}
	return agents
}

func (w *Workspace) Contexts() []*warp.Context {
	resolver := w.resolver()
	if resolver == nil {
		return nil
	}

	resources := resolver.ListResources(warp.QueryOptions{
		Kinds: []warp.Kind{warp.KindContext},
	})
	contexts := make([]*warp.Context, 0, len(resources))
	for _, r := range resources {
		if ctx, ok := r.(*warp.Context); ok {
			contexts = append(contexts, ctx)
		}
	}
	return contexts
}

func (w *Workspace) Providers() []*warp.ModelProvider {
	resolver := w.resolver()
	if resolver == nil {
		return nil
	}

	resources := resolver.ListResources(warp.QueryOptions{
		Kinds: []warp.Kind{warp.KindModelProvider},
	})
	providers := make([]*warp.ModelProvider, 0, len(resources))
	for _, r := range resources {
		if provider, ok := r.(*warp.ModelProvider); ok {
			providers = append(providers, provider)
		}
	}
	return providers
}

func (w *Workspace) MCPs() []*warp.MCP {
	resolver := w.resolver()
	if resolver == nil {
		return nil
	}

	resources := resolver.ListResources(warp.QueryOptions{
		Kinds: []warp.Kind{warp.KindMCP},
	})
	mcps := make([]*warp.MCP, 0, len(resources))
	for _, r := range resources {
		if mcp, ok := r.(*warp.MCP); ok {
			mcps = append(mcps, mcp)
		}
	}
	return mcps
}

func (w *Workspace) ProvidersPresets() []*warp.ModelProvider {
	var providers []*warp.ModelProvider
	err := fs.WalkDir(presetFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		data, err := presetFS.ReadFile(path)
		if err != nil {
			return err
		}
		result, err := warp.Parse(path, string(data))
		if err != nil {
			return err
		}
		if p, ok := result.Resource.(*warp.ModelProvider); ok {
			providers = append(providers, p)
		}
		return nil
	})
	if err != nil {
		w.logger.Error("Failed to load provider presets", log.Err(err))
	}
	return providers
}

func (w *Workspace) ToolsPresets() []*warp.Tool {
	toolsList, err := tools.Resources()
	if err != nil {
		w.logger.Error("Failed to load tool presets", log.Err(err))
		return nil
	}

	return toolsList
}

func (w *Workspace) resolver() warp.Resolver {
	if w.registry == nil {
		return nil
	}

	project, ok := w.registry.ProjectFromPath(w.cwd)
	if ok {
		return w.registry.Project(project.Name)
	}
	return w.registry
}

type WorkspaceConfig struct {
	Name            string
	DefaultProvider string
	AuthorizedTools map[string]bool
	IsConfigured    bool
	IsTrusted       bool
	HasManifest     bool
	CWD             string
}

func (w *Workspace) GetWorkspaceConfig(ctx context.Context) (WorkspaceConfig, error) {
	if w.registry == nil {
		return WorkspaceConfig{CWD: w.cwd}, nil
	}

	var hasManifest bool
	var projectName string
	var defaultProvider string
	var authorizedTools map[string]bool

	wsSpec := w.registry.WorkspaceSpec()
	if wsSpec != nil && wsSpec.Def != nil && !wsSpec.Synthetic {
		hasManifest = true
		projectName = wsSpec.Def.Metadata.Name
		defaultProvider = wsSpec.Def.Spec.DefaultProvider
		if wsSpec.Def.Spec.Policies != nil && wsSpec.Def.Spec.Policies.Tools != nil {
			authorizedTools = make(map[string]bool)
			for _, tool := range wsSpec.Def.Spec.Policies.Tools.Include {
				authorizedTools[tool] = true
			}
		}
	}

	// Default project name to CWD folder base name if not defined
	if projectName == "" {
		projectName = filepath.Base(w.cwd)
	}

	// Always resolve local trust regardless of whether WORKSPACE.md is present
	var isTrusted bool
	wsDir, err := xdg.WorkspaceDir(w.cwd)
	if err == nil {
		sentinelPath := filepath.Join(wsDir, "setup.json")
		if _, err := os.Stat(sentinelPath); err == nil {
			isTrusted = true
		}
	}

	return WorkspaceConfig{
		Name:            projectName,
		DefaultProvider: defaultProvider,
		AuthorizedTools: authorizedTools,
		IsConfigured:    isTrusted && hasManifest,
		IsTrusted:       isTrusted,
		HasManifest:     hasManifest,
		CWD:             w.cwd,
	}, nil
}

// ResolveAgent resolves an agent by ref within this workspace scope, applying
// recursive inheritance merging.
func (w *Workspace) ResolveAgent(ctx context.Context, ref string) (*warp.ResolvedAgent, error) {
	resolver := w.resolver()
	if resolver == nil {
		return nil, fmt.Errorf("workspace resolver not available")
	}
	switch r := resolver.(type) {
	case *warp.ScopedRegistry:
		return r.ResolveAgent(ref)
	case *warp.Registry:
		return r.ResolveAgent(ref)
	default:
		return nil, fmt.Errorf("unsupported registry type %T", resolver)
	}
}

// WorkspaceSpec returns the underlying warp Workspace spec.
func (w *Workspace) WorkspaceSpec() *warp.Workspace {
	if w.registry == nil {
		return nil
	}
	return w.registry.WorkspaceSpec()
}

// Project returns the current project metadata if the workspace is scoped to a project path.
func (w *Workspace) Project() *warp.Project {
	if w.registry == nil {
		return nil
	}
	p, ok := w.registry.ProjectFromPath(w.cwd)
	if !ok {
		return nil
	}
	return p
}

func (w *Workspace) ResolveDefaults(ctx context.Context) (agentName, providerName, modelName string, err error) {
	// Default values in case nothing is configured
	agentName = "main"
	providerName = "ollama"
	modelName = "qwen3.6:35b-a3b-coding-nvfp4"

	cfg, err := w.GetWorkspaceConfig(ctx)
	if err != nil {
		return agentName, providerName, modelName, err
	}

	// 1. Find the "main" agent (or first agent if main doesn't exist)
	var mainAgent *warp.Agent
	agents := w.Agents()
	for _, a := range agents {
		if a.GetName() == "main" {
			mainAgent = a
			break
		}
	}
	if mainAgent == nil && len(agents) > 0 {
		mainAgent = agents[0]
	}

	if mainAgent != nil {
		agentName = mainAgent.GetName()
		// Try to resolve provider and model from agent's models list
		if len(mainAgent.Spec.Models) > 0 {
			providers := w.Providers()
			for _, modelID := range mainAgent.Spec.Models {
				for _, p := range providers {
					for _, mInfo := range p.Spec.Models {
						if mInfo.ID == modelID {
							return agentName, p.GetName(), modelID, nil
						}
					}
				}
			}
		}
	}

	// 2. Fallback to default provider in workspace config
	if cfg.DefaultProvider != "" {
		providers := w.Providers()
		for _, p := range providers {
			if p.GetName() == cfg.DefaultProvider {
				pName := p.GetName()
				mName := p.Spec.DefaultModel
				if mName == "" && len(p.Spec.Models) > 0 {
					mName = p.Spec.Models[0].ID
				}
				if mName != "" {
					return agentName, pName, mName, nil
				}
			}
		}
	}

	return agentName, providerName, modelName, nil
}
