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
	"github.com/masterkeysrd/tasksmith/internal/core/env"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
	"github.com/masterkeysrd/tasksmith/internal/core/xdg"
	"github.com/masterkeysrd/warp"
)

//go:embed all:preset/*.yaml
var presetFS embed.FS

const (
	AgentNameMain               = "main"
	AgentNameAgentCreator       = "agent-creator"
	AgentNameSkillCreator       = "skill-creator"
	AgentNameToolCreator        = "tool-creator"
	AgentNameProjectInitializer = "project-initializer"
	AgentNameProviderManager    = "provider-manager"
)

type Workspace struct {
	cwd           string
	logger        log.Interface
	registry      *warp.Registry
	agentOverride string
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

func findEnvPath(startDir string) string {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		dir = startDir
	}
	for {
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
		// Stop climbing if we hit workspace sentinel files
		if _, err := os.Stat(filepath.Join(dir, "WORKSPACE.md")); err == nil {
			return envPath
		}
		if _, err := os.Stat(filepath.Join(dir, ".warp")); err == nil {
			return envPath
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func (w *Workspace) Load(ctx context.Context) error {
	w.logger.Info("Loading workspace", log.String("cwd", w.cwd))

	// Load local environment secrets by walking up to find .env or workspace root
	envPath := findEnvPath(w.cwd)
	if envPath != "" {
		if err := env.Load(envPath); err != nil {
			w.logger.Warn("Failed to load local .env file", log.String("path", envPath), log.Err(err))
		} else {
			w.logger.Info("Loaded environment secrets", log.String("path", envPath))
		}
	}

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

func (w *Workspace) ListResources(opts warp.QueryOptions) []warp.Resource {
	resolver := w.resolver()
	if resolver == nil {
		return nil
	}
	return resolver.ListResources(opts)
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
	DefaultAgent    string
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
	var defaultAgent string
	var authorizedTools map[string]bool

	wsSpec := w.registry.WorkspaceSpec()
	if wsSpec != nil && wsSpec.Def != nil && !wsSpec.Synthetic {
		hasManifest = true
		projectName = wsSpec.Def.Metadata.Name
		defaultProvider = wsSpec.Def.Spec.DefaultProvider
		defaultAgent = wsSpec.Def.Spec.DefaultAgent
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
		DefaultAgent:    defaultAgent,
		AuthorizedTools: authorizedTools,
		IsConfigured:    isTrusted && hasManifest,
		IsTrusted:       isTrusted,
		HasManifest:     hasManifest,
		CWD:             w.cwd,
	}, nil
}

func (w *Workspace) SetAgentOverride(agentName string) {
	w.agentOverride = agentName
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
	if w.registry == nil {
		_ = w.Load(ctx)
	}

	agentName = "main"
	if w.agentOverride != "" {
		agentName = w.agentOverride
	} else {
		cfg, _ := w.GetWorkspaceConfig(ctx)
		if cfg.DefaultAgent != "" {
			agentName = cfg.DefaultAgent
		}
	}
	providerName = "ollama"
	modelName = "qwen3.6:35b-a3b-coding-nvfp4"

	// 1. Find the target default agent
	var mainAgent *warp.Agent
	agents := w.Agents()
	for _, a := range agents {
		if a.GetName() == agentName {
			mainAgent = a
			break
		}
	}

	if mainAgent == nil && w.agentOverride == "" {
		// Only fallback if no override was explicitly specified
		for _, a := range agents {
			if a.GetName() == "main" {
				mainAgent = a
				break
			}
		}
		if mainAgent == nil && len(agents) > 0 {
			mainAgent = agents[0]
		}
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
	cfg, _ := w.GetWorkspaceConfig(ctx)
	providers := w.Providers()
	if cfg.DefaultProvider != "" {
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

	// 3. Fallback to ollama provider preset or first available provider
	var fallbackP *warp.ModelProvider
	for _, p := range providers {
		if p.GetName() == "ollama" {
			fallbackP = p
			break
		}
	}
	if fallbackP == nil && len(providers) > 0 {
		fallbackP = providers[0]
	}

	if fallbackP != nil {
		pName := fallbackP.GetName()
		mName := fallbackP.Spec.DefaultModel
		if mName == "" && len(fallbackP.Spec.Models) > 0 {
			mName = fallbackP.Spec.Models[0].ID
		}
		if mName != "" {
			return agentName, pName, mName, nil
		}
	}

	return agentName, "ollama", "qwen3.6:35b-a3b-coding-nvfp4", nil
}

// AuthorizeTools updates WORKSPACE.md to include the specified tools in the authorized list, then reloads the workspace.
func (w *Workspace) AuthorizeTools(ctx context.Context, toolsToAuthorize []string) error {
	wsSpec := w.WorkspaceSpec()
	var wsDef *warp.WorkspaceDef

	if wsSpec == nil || wsSpec.Def == nil || wsSpec.Synthetic {
		// Initialize a new WorkspaceDef
		projectName := filepath.Base(w.cwd)
		var allowedTools []string
		for _, tool := range toolsToAuthorize {
			allowedTools = append(allowedTools, tool)
		}
		wsDef = &warp.WorkspaceDef{
			BaseResource: warp.BaseResource{
				APIVersion: warp.APIVersion,
				Kind:       warp.KindWorkspace,
				Metadata: warp.Metadata{
					Name: projectName,
				},
			},
			Spec: warp.WorkspaceDefSpec{
				Projects:        []string{"."},
				DefaultProvider: "ollama",
				DefaultAgent:    "",
				Plugins:         []warp.WorkspacePlugin{},
				Policies: &warp.Policies{
					Tools: &warp.ToolPolicies{
						Include: allowedTools,
					},
				},
			},
		}
	} else {
		wsDef = wsSpec.Def
		if wsDef.Spec.Policies == nil {
			wsDef.Spec.Policies = &warp.Policies{}
		}
		if wsDef.Spec.Policies.Tools == nil {
			wsDef.Spec.Policies.Tools = &warp.ToolPolicies{}
		}

		for _, tool := range toolsToAuthorize {
			found := false
			for _, existing := range wsDef.Spec.Policies.Tools.Include {
				if existing == tool {
					found = true
					break
				}
			}
			if !found {
				wsDef.Spec.Policies.Tools.Include = append(wsDef.Spec.Policies.Tools.Include, tool)
			}
		}
	}

	data, err := warp.Format(wsDef)
	if err != nil {
		return fmt.Errorf("format workspace definition: %w", err)
	}

	wsPath := filepath.Join(w.cwd, "WORKSPACE.md")
	if err := os.WriteFile(wsPath, data, 0644); err != nil {
		return fmt.Errorf("write WORKSPACE.md: %w", err)
	}

	return w.Load(ctx)
}
