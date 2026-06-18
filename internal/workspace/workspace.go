package workspace

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/masterkeysrd/tasksmith/internal/agent/tools"
	"github.com/masterkeysrd/tasksmith/internal/core/log"
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
	return &Workspace{
		cwd:    cwd,
		logger: log.ForComponent("workspace"),
	}
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
		Kinds: []string{string(warp.KindAgent)},
	})
	agents := make([]*warp.Agent, 0, len(resources))
	for _, r := range resources {
		if agent, ok := r.(*warp.Agent); ok {
			agents = append(agents, agent)
		}
	}
	return agents
}

func (w *Workspace) Providers() []*warp.ModelProvider {
	resolver := w.resolver()
	if resolver == nil {
		return nil
	}

	resources := resolver.ListResources(warp.QueryOptions{
		Kinds: []string{string(warp.KindModelProvider)},
	})
	providers := make([]*warp.ModelProvider, 0, len(resources))
	for _, r := range resources {
		if provider, ok := r.(*warp.ModelProvider); ok {
			providers = append(providers, provider)
		}
	}
	return providers
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
}

func (w *Workspace) GetWorkspaceConfig(ctx context.Context) (WorkspaceConfig, error) {
	if w.registry == nil {
		return WorkspaceConfig{}, nil
	}
	wsSpec := w.registry.WorkspaceSpec()
	if wsSpec == nil || wsSpec.Def == nil || wsSpec.Synthetic {
		return WorkspaceConfig{}, nil
	}

	cfg := WorkspaceConfig{
		Name:            wsSpec.Def.Metadata.Name,
		DefaultProvider: wsSpec.Def.Spec.DefaultProvider,
		IsConfigured:    true,
		AuthorizedTools: make(map[string]bool),
	}

	if wsSpec.Def.Spec.Policies != nil && wsSpec.Def.Spec.Policies.Tools != nil {
		for _, tool := range wsSpec.Def.Spec.Policies.Tools.Include {
			cfg.AuthorizedTools[tool] = true
		}
	}

	return cfg, nil
}
